// Package depthchartsync imports canonical nflverse depth-chart history.
package depthchartsync

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
	DepthChartsDataset(context.Context, int) (nflverse.DepthChartsDataset, error)
}

type Request struct {
	Season int
}

type Result struct {
	Season                   int      `json:"season"`
	SourceRows               int      `json:"source_rows"`
	SourceObservations       int      `json:"source_observations"`
	ResolvedObservations     int      `json:"resolved_observations"`
	StoredObservations       int      `json:"stored_observations"`
	SourceVersion            string   `json:"source_version"`
	DatasetVersion           string   `json:"dataset_version"`
	Unchanged                bool     `json:"unchanged"`
	UnmatchedExternalPlayers int      `json:"unmatched_external_players"`
	UnmatchedExternalIDs     []string `json:"unmatched_external_ids,omitempty"`
}

type Service struct {
	Source     Source
	Identities identity.BulkResolver
	Charts     history.DepthChartWriter
	State      history.DepthChartDatasetStateStore
	Archive    history.SourceFileWriter
	Now        func() time.Time
}

func (s Service) Sync(ctx context.Context, request Request) (Result, error) {
	if s.Source == nil || s.Identities == nil || s.Charts == nil {
		return Result{}, fmt.Errorf("depth-chart source, identity resolver, and repository are required")
	}
	if request.Season < 2001 || request.Season > 2100 {
		return Result{}, fmt.Errorf("depth-chart season must be between 2001 and 2100")
	}
	dataset, err := s.Source.DepthChartsDataset(ctx, request.Season)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Season: request.Season, SourceRows: dataset.SourceRows,
		SourceObservations: len(dataset.Records), SourceVersion: dataset.SourceVersion,
	}
	externalByRecord := make([]player.ExternalID, len(dataset.Records))
	unique := make(map[player.ExternalID]struct{})
	for index, record := range dataset.Records {
		externalID := player.ExternalID{Provider: player.ProviderGSIS, Value: record.GSISPlayerID}
		if externalID.Value == "" {
			externalID = player.ExternalID{Provider: player.ProviderESPN, Value: record.ESPNPlayerID}
		}
		externalByRecord[index] = externalID
		if externalID.Value != "" {
			unique[externalID] = struct{}{}
		}
	}
	externalIDs := make([]player.ExternalID, 0, len(unique))
	for externalID := range unique {
		externalIDs = append(externalIDs, externalID)
	}
	sort.Slice(externalIDs, func(i, j int) bool {
		if externalIDs[i].Provider != externalIDs[j].Provider {
			return externalIDs[i].Provider < externalIDs[j].Provider
		}
		return externalIDs[i].Value < externalIDs[j].Value
	})
	resolved, err := s.Identities.ResolvePlayers(ctx, externalIDs)
	if err != nil {
		return Result{}, fmt.Errorf("resolve depth-chart player identities: %w", err)
	}
	for _, externalID := range externalIDs {
		if _, ok := resolved[externalID]; !ok {
			result.UnmatchedExternalIDs = append(result.UnmatchedExternalIDs, string(externalID.Provider)+"#"+externalID.Value)
		}
	}
	result.UnmatchedExternalPlayers = len(result.UnmatchedExternalIDs)
	now := s.Now
	if now == nil {
		now = time.Now
	}
	runAt := now().UTC()
	runID := runAt.Format("20060102T150405.000000000Z")
	facts := make([]history.DepthChartObservation, 0, len(dataset.Records))
	factIndexes := make(map[string]int, len(dataset.Records))
	for index, record := range dataset.Records {
		externalID := externalByRecord[index]
		profile, ok := resolved[externalID]
		if !ok {
			continue
		}
		fact := history.DepthChartObservation{
			PlayerID: profile.ID, SourcePlayerID: externalID.Value, SourceProvider: string(externalID.Provider),
			PlayerName: record.PlayerName, Season: record.Season, Week: record.Week, GameType: record.GameType,
			ObservedAt: record.ObservedAt, LastSeenAt: record.LastSeenAt, Team: record.Team,
			PositionGroupID: record.PositionGroupID, PositionGroup: record.PositionGroup,
			PositionID: record.PositionID, Position: record.Position, PositionName: record.PositionName,
			PositionSlot: record.PositionSlot, DepthRank: record.DepthRank,
			Source: "nflverse-depth-charts", IngestionRunID: runID,
		}
		if err := fact.Validate(); err != nil {
			return Result{}, fmt.Errorf("normalize %s depth-chart observation: %w", externalID.Value, err)
		}
		factKey := fmt.Sprintf("%s\x00%d\x00%d\x00%s\x00%s\x00%s\x00%d\x00%s\x00%d\x00%s\x00%s\x00%d",
			fact.PlayerID, fact.Season, fact.Week, fact.ObservedAt.UTC().Format(time.RFC3339Nano), fact.GameType,
			fact.Team, fact.PositionGroupID, fact.PositionGroup, fact.PositionID, fact.Position, fact.PositionName, fact.PositionSlot)
		if existing, ok := factIndexes[factKey]; ok {
			if facts[existing].SourceProvider != string(player.ProviderGSIS) && fact.SourceProvider == string(player.ProviderGSIS) {
				facts[existing] = fact
			}
			continue
		}
		factIndexes[factKey] = len(facts)
		facts = append(facts, fact)
	}
	result.ResolvedObservations = len(facts)
	if len(facts) == 0 {
		return Result{}, fmt.Errorf("no depth-chart observations resolved to canonical players; sync player identities first")
	}
	version, err := normalizedVersion(facts)
	if err != nil {
		return Result{}, err
	}
	result.DatasetVersion = version
	if s.State != nil {
		state, err := s.State.DepthChartDatasetState(ctx, request.Season)
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
			Dataset: "nflverse-depth-charts", Season: request.Season, Version: dataset.SourceVersion,
			SourceURL: dataset.SourceURL, ContentType: "text/csv", Payload: dataset.Payload,
		}); err != nil {
			return Result{}, err
		}
	}
	if err := s.Charts.PutDepthChartObservations(ctx, facts); err != nil {
		return Result{}, err
	}
	result.StoredObservations = len(facts)
	if s.State != nil {
		if err := s.State.PutDepthChartDatasetState(ctx, history.DepthChartDatasetState{
			Season: request.Season, SourceVersion: dataset.SourceVersion, Version: version,
			RecordCount: len(facts), ImportedAt: runAt,
		}); err != nil {
			return Result{}, err
		}
	}
	return result, nil
}

func normalizedVersion(facts []history.DepthChartObservation) (string, error) {
	normalized := append([]history.DepthChartObservation(nil), facts...)
	for index := range normalized {
		normalized[index].IngestionRunID = ""
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].PlayerID != normalized[j].PlayerID {
			return normalized[i].PlayerID < normalized[j].PlayerID
		}
		if normalized[i].Week != normalized[j].Week {
			return normalized[i].Week < normalized[j].Week
		}
		if !normalized[i].ObservedAt.Equal(normalized[j].ObservedAt) {
			return normalized[i].ObservedAt.Before(normalized[j].ObservedAt)
		}
		if normalized[i].Team != normalized[j].Team {
			return normalized[i].Team < normalized[j].Team
		}
		if normalized[i].PositionGroupID != normalized[j].PositionGroupID {
			return normalized[i].PositionGroupID < normalized[j].PositionGroupID
		}
		return normalized[i].PositionSlot < normalized[j].PositionSlot
	})
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("fingerprint normalized depth charts: %w", err)
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
