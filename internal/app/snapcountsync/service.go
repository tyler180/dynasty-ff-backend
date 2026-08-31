// Package snapcountsync imports game-level NFL snap participation facts.
package snapcountsync

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
	"github.com/tyler180/dynasty-ff-backend/internal/identity"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
	"github.com/tyler180/dynasty-ff-backend/internal/provider/nflverse"
)

type Source interface {
	SnapCounts(context.Context, int) ([]nflverse.SnapCount, error)
}

type Request struct {
	Season int
}

type Result struct {
	Season                int      `json:"season"`
	SourceRecords         int      `json:"source_records"`
	DefensiveRecords      int      `json:"defensive_records"`
	ResolvedRecords       int      `json:"resolved_records"`
	StoredRecords         int      `json:"stored_records"`
	UnmatchedPFRPlayers   int      `json:"unmatched_pfr_players"`
	UnmatchedPFRPlayerIDs []string `json:"unmatched_pfr_player_ids,omitempty"`
}

type Service struct {
	Source     Source
	Identities identity.BulkResolver
	Snaps      history.SnapWriter
	Now        func() time.Time
}

func (s Service) Sync(ctx context.Context, request Request) (Result, error) {
	if s.Source == nil || s.Identities == nil || s.Snaps == nil {
		return Result{}, fmt.Errorf("snap source, identity resolver, and snap repository are required")
	}
	if request.Season < 2012 || request.Season > 2100 {
		return Result{}, fmt.Errorf("snap-count season must be between 2012 and 2100")
	}
	records, err := s.Source.SnapCounts(ctx, request.Season)
	if err != nil {
		return Result{}, err
	}
	result := Result{Season: request.Season, SourceRecords: len(records)}
	defensive := make([]nflverse.SnapCount, 0, len(records))
	uniquePFRIDs := make(map[string]struct{})
	for _, record := range records {
		if record.Season != request.Season || strings.TrimSpace(record.PFRPlayerID) == "" || !isDefensive(record) {
			continue
		}
		defensive = append(defensive, record)
		uniquePFRIDs[record.PFRPlayerID] = struct{}{}
	}
	result.DefensiveRecords = len(defensive)
	externalIDs := make([]player.ExternalID, 0, len(uniquePFRIDs))
	for pfrID := range uniquePFRIDs {
		externalIDs = append(externalIDs, player.ExternalID{Provider: player.ProviderPFR, Value: pfrID})
	}
	sort.Slice(externalIDs, func(i, j int) bool { return externalIDs[i].Value < externalIDs[j].Value })
	resolved, err := s.Identities.ResolvePlayers(ctx, externalIDs)
	if err != nil {
		return Result{}, fmt.Errorf("resolve PFR snap-count identities: %w", err)
	}
	for _, externalID := range externalIDs {
		if _, ok := resolved[externalID]; !ok {
			result.UnmatchedPFRPlayerIDs = append(result.UnmatchedPFRPlayerIDs, externalID.Value)
		}
	}
	result.UnmatchedPFRPlayers = len(result.UnmatchedPFRPlayerIDs)
	now := s.Now
	if now == nil {
		now = time.Now
	}
	runID := now().UTC().Format("20060102T150405.000000000Z")
	facts := make([]history.PlayerGameSnaps, 0, len(defensive))
	for _, record := range defensive {
		profile, ok := resolved[player.ExternalID{Provider: player.ProviderPFR, Value: record.PFRPlayerID}]
		if !ok {
			continue
		}
		fact := history.PlayerGameSnaps{
			PlayerID: profile.ID, GameID: record.GameID, Season: record.Season, Week: record.Week, GameType: record.GameType,
			Team: record.Team, Opponent: record.Opponent, Position: record.Position, PositionGroup: positionGroup(record.Position),
			OffenseSnaps: record.OffenseSnaps, OffenseSnapPct: record.OffenseSnapPct,
			DefenseSnaps: record.DefenseSnaps, DefenseSnapPct: record.DefenseSnapPct,
			SpecialTeamSnaps: record.SpecialTeamSnaps, SpecialTeamPct: record.SpecialTeamPct,
			Source: "nflverse-pfr", IngestionRunID: runID,
		}
		if err := fact.Validate(); err != nil {
			return Result{}, fmt.Errorf("normalize PFR player %s game %s: %w", record.PFRPlayerID, record.GameID, err)
		}
		facts = append(facts, fact)
	}
	result.ResolvedRecords = len(facts)
	if len(facts) == 0 {
		return Result{}, fmt.Errorf("no defensive snap records resolved to canonical players; sync player identities first")
	}
	if err := s.Snaps.PutPlayerGameSnaps(ctx, facts); err != nil {
		return Result{}, err
	}
	result.StoredRecords = len(facts)
	return result, nil
}

func isDefensive(record nflverse.SnapCount) bool {
	return record.DefenseSnaps > 0 || record.DefenseSnapPct > 0 || positionGroup(record.Position) != ""
}

func positionGroup(position string) string {
	switch strings.ToUpper(strings.TrimSpace(position)) {
	case "DE", "DT", "NT", "DL", "EDGE":
		return "DL"
	case "LB", "ILB", "OLB", "MLB":
		return "LB"
	case "CB", "DB", "S", "FS", "SS", "SAF":
		return "DB"
	default:
		return ""
	}
}
