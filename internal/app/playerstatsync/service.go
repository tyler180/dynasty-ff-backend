// Package playerstatsync imports comprehensive weekly nflverse player stats.
package playerstatsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
	"github.com/tyler180/dynasty-ff-backend/internal/identity"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
	"github.com/tyler180/dynasty-ff-backend/internal/provider/nflverse"
)

type Source interface {
	PlayerStatsDataset(context.Context, int) (nflverse.PlayerStatsDataset, error)
}

type Request struct {
	Season int
}

type Result struct {
	Season                 int      `json:"season"`
	SourceRecords          int      `json:"source_records"`
	ResolvedRecords        int      `json:"resolved_records"`
	StoredRecords          int      `json:"stored_records"`
	SourceVersion          string   `json:"source_version"`
	DatasetVersion         string   `json:"dataset_version"`
	Unchanged              bool     `json:"unchanged"`
	UnmatchedGSISPlayers   int      `json:"unmatched_gsis_players"`
	UnmatchedGSISPlayerIDs []string `json:"unmatched_gsis_player_ids,omitempty"`
}

type Service struct {
	Source     Source
	Identities identity.BulkResolver
	Stats      history.PlayerStatsWriter
	State      history.PlayerStatsDatasetStateStore
	Archive    history.SourceFileWriter
	Now        func() time.Time
}

func (s Service) Sync(ctx context.Context, request Request) (Result, error) {
	if s.Source == nil || s.Identities == nil || s.Stats == nil {
		return Result{}, fmt.Errorf("player-stat source, identity resolver, and repository are required")
	}
	if request.Season < 1999 || request.Season > 2100 {
		return Result{}, fmt.Errorf("player-stat season must be between 1999 and 2100")
	}
	dataset, err := s.Source.PlayerStatsDataset(ctx, request.Season)
	if err != nil {
		return Result{}, err
	}
	result := Result{Season: request.Season, SourceRecords: len(dataset.Records), SourceVersion: dataset.SourceVersion}
	unique := make(map[string]struct{})
	for _, record := range dataset.Records {
		if record.Season == request.Season && record.GSISPlayerID != "" {
			unique[record.GSISPlayerID] = struct{}{}
		}
	}
	externalIDs := make([]player.ExternalID, 0, len(unique))
	for id := range unique {
		externalIDs = append(externalIDs, player.ExternalID{Provider: player.ProviderGSIS, Value: id})
	}
	sort.Slice(externalIDs, func(i, j int) bool { return externalIDs[i].Value < externalIDs[j].Value })
	resolved, err := s.Identities.ResolvePlayers(ctx, externalIDs)
	if err != nil {
		return Result{}, fmt.Errorf("resolve GSIS player-stat identities: %w", err)
	}
	for _, externalID := range externalIDs {
		if _, ok := resolved[externalID]; !ok {
			result.UnmatchedGSISPlayerIDs = append(result.UnmatchedGSISPlayerIDs, externalID.Value)
		}
	}
	result.UnmatchedGSISPlayers = len(result.UnmatchedGSISPlayerIDs)
	now := s.Now
	if now == nil {
		now = time.Now
	}
	runAt := now().UTC()
	runID := runAt.Format("20060102T150405.000000000Z")
	facts := make([]history.PlayerGameStats, 0, len(dataset.Records))
	for _, record := range dataset.Records {
		if record.Season != request.Season {
			continue
		}
		externalID := player.ExternalID{Provider: player.ProviderGSIS, Value: record.GSISPlayerID}
		profile, ok := resolved[externalID]
		if !ok {
			continue
		}
		fact := history.PlayerGameStats{
			PlayerID: profile.ID, SourcePlayerID: record.GSISPlayerID, PlayerName: record.PlayerName,
			DisplayName: record.DisplayName, Position: record.Position, PositionGroup: record.PositionGroup,
			Season: record.Season, Week: record.Week, GameType: record.GameType, GameID: record.GameID,
			Team: record.Team, Opponent: record.Opponent, Metrics: cloneNumbers(record.Metrics),
			Attributes: cloneStrings(record.Attributes), Source: "nflverse-player-stats", IngestionRunID: runID,
		}
		if err := fact.Validate(); err != nil {
			return Result{}, fmt.Errorf("normalize GSIS player %s game %s: %w", record.GSISPlayerID, record.GameID, err)
		}
		facts = append(facts, fact)
	}
	result.ResolvedRecords = len(facts)
	if len(facts) == 0 {
		return Result{}, fmt.Errorf("no player-stat records resolved to canonical players; sync player identities first")
	}
	version, err := normalizedVersion(facts)
	if err != nil {
		return Result{}, err
	}
	result.DatasetVersion = version
	if s.State != nil {
		state, err := s.State.PlayerStatsDatasetState(ctx, request.Season)
		if err != nil {
			return Result{}, err
		}
		if state.SourceVersion == dataset.SourceVersion && state.Version == version {
			result.Unchanged = true
			return result, nil
		}
	}
	if s.Archive != nil {
		if err := s.Archive.PutSourceFile(ctx, history.SourceFile{
			Dataset: "nflverse-player-stats", Season: request.Season, Version: dataset.SourceVersion,
			SourceURL: dataset.SourceURL, ContentType: "text/csv", Payload: dataset.Payload,
		}); err != nil {
			return Result{}, err
		}
	}
	if err := s.Stats.PutPlayerGameStats(ctx, facts); err != nil {
		return Result{}, err
	}
	result.StoredRecords = len(facts)
	if s.State != nil {
		if err := s.State.PutPlayerStatsDatasetState(ctx, history.PlayerStatsDatasetState{
			Season: request.Season, SourceVersion: dataset.SourceVersion, Version: version,
			RecordCount: len(facts), ImportedAt: runAt,
		}); err != nil {
			return Result{}, err
		}
	}
	return result, nil
}

func normalizedVersion(facts []history.PlayerGameStats) (string, error) {
	normalized := append([]history.PlayerGameStats(nil), facts...)
	for index := range normalized {
		normalized[index].IngestionRunID = ""
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Week != normalized[j].Week {
			return normalized[i].Week < normalized[j].Week
		}
		if normalized[i].GameID != normalized[j].GameID {
			return normalized[i].GameID < normalized[j].GameID
		}
		return normalized[i].PlayerID < normalized[j].PlayerID
	})
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("fingerprint normalized player stats: %w", err)
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func cloneNumbers(values map[string]float64) map[string]float64 {
	result := make(map[string]float64, len(values))
	for name, value := range values {
		result[name] = value
	}
	return result
}

func cloneStrings(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for name, value := range values {
		result[name] = value
	}
	return result
}
