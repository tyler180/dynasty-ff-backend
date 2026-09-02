// Package freeagenttrends ranks current MFL defensive free agents by sustained
// increases in their defensive snap participation.
package freeagenttrends

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
	"github.com/tyler180/dynasty-ff-backend/internal/identity"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
	mflsync "github.com/tyler180/dynasty-ff-backend/internal/provider/mfl"
	"github.com/tyler180/dynasty-ff-models/usage"
)

const (
	defaultLimit = 10
	maxLimit     = 50
	queryBatch   = 100
)

type FreeAgentSource interface {
	DefensiveFreeAgents(context.Context, int, string) ([]mflsync.DefensiveFreeAgent, error)
}

type Request struct {
	Year     int
	LeagueID string
	Seasons  []int
	Limit    int
}

type Result struct {
	LeagueID                  string              `json:"league_id"`
	LeagueSeason              int                 `json:"league_season"`
	SnapSeasons               []int               `json:"snap_seasons"`
	AvailableDefensivePlayers int                 `json:"available_defensive_players"`
	ResolvedPlayers           int                 `json:"resolved_players"`
	PlayersWithSnapData       int                 `json:"players_with_snap_data"`
	UnresolvedMFLPlayerIDs    []string            `json:"unresolved_mfl_player_ids,omitempty"`
	Method                    string              `json:"method"`
	Config                    usage.Config        `json:"config"`
	Trends                    []usage.PlayerTrend `json:"trends"`
}

type Service struct {
	FreeAgents FreeAgentSource
	Identities identity.BulkResolver
	Snaps      history.SnapReader
}

func (s Service) Analyze(ctx context.Context, request Request) (Result, error) {
	if s.FreeAgents == nil || s.Identities == nil || s.Snaps == nil {
		return Result{}, fmt.Errorf("free-agent source, identity resolver, and snap repository are required")
	}
	if request.Year < 2000 || request.Year > 2100 || strings.TrimSpace(request.LeagueID) == "" {
		return Result{}, fmt.Errorf("league season and league ID are required")
	}
	seasons := normalizedSeasons(request.Year, request.Seasons)
	if len(seasons) == 0 || len(seasons) > 10 {
		return Result{}, fmt.Errorf("between one and ten snap seasons are required")
	}
	limit := request.Limit
	if limit == 0 {
		limit = defaultLimit
	}
	if limit < 1 || limit > maxLimit {
		return Result{}, fmt.Errorf("limit must be between 1 and %d", maxLimit)
	}

	available, err := s.FreeAgents.DefensiveFreeAgents(ctx, request.Year, request.LeagueID)
	if err != nil {
		return Result{}, fmt.Errorf("load MFL defensive free agents: %w", err)
	}
	externalIDs := make([]player.ExternalID, 0, len(available))
	for _, candidate := range available {
		externalIDs = append(externalIDs, player.ExternalID{Provider: player.ProviderMFL, Value: candidate.MFLID})
	}
	profiles, err := s.Identities.ResolvePlayers(ctx, externalIDs)
	if err != nil {
		return Result{}, fmt.Errorf("resolve MFL defensive free agents: %w", err)
	}

	modelPlayers := make([]usage.Player, 0, len(profiles))
	playerIDs := make([]player.ID, 0, len(profiles))
	unresolved := make([]string, 0)
	for _, candidate := range available {
		externalID := player.ExternalID{Provider: player.ProviderMFL, Value: candidate.MFLID}
		profile, ok := profiles[externalID]
		if !ok {
			unresolved = append(unresolved, candidate.MFLID)
			continue
		}
		modelPlayers = append(modelPlayers, usage.Player{
			ID: string(profile.ID), Name: profile.DisplayName,
			Position: candidate.Position, PositionGroup: candidate.PositionGroup,
		})
		playerIDs = append(playerIDs, profile.ID)
	}
	sort.Strings(unresolved)
	if len(modelPlayers) == 0 {
		return Result{}, fmt.Errorf("no MFL defensive free agents resolved to canonical players")
	}

	var facts []history.PlayerGameSnaps
	for start := 0; start < len(playerIDs); start += queryBatch {
		end := min(start+queryBatch, len(playerIDs))
		batch, err := s.Snaps.PlayerGameSnaps(ctx, history.SnapQuery{
			PlayerIDs: playerIDs[start:end], Seasons: seasons, PositionGroups: []string{"DL", "LB", "DB"},
		})
		if err != nil {
			return Result{}, fmt.Errorf("load defensive snap facts: %w", err)
		}
		facts = append(facts, batch...)
	}
	observations := make([]usage.Observation, 0, len(facts))
	withSnaps := make(map[string]struct{})
	for _, fact := range facts {
		observations = append(observations, usage.Observation{
			PlayerID: string(fact.PlayerID), GameID: fact.GameID, Season: fact.Season, Week: fact.Week,
			GameType: fact.GameType, PositionGroup: fact.PositionGroup,
			DefenseSnaps: fact.DefenseSnaps, TeamDefenseSnaps: fact.TeamDefenseSnaps,
			DefenseSnapPct: fact.DefenseSnapPct,
		})
		withSnaps[string(fact.PlayerID)] = struct{}{}
	}
	report, err := usage.Analyze(usage.Input{Players: modelPlayers, Observations: observations})
	if err != nil {
		return Result{}, fmt.Errorf("analyze defensive snap trends: %w", err)
	}
	top := make([]usage.PlayerTrend, 0, limit)
	for _, trend := range report.Trends {
		if trend.Signal != usage.SignalRising {
			continue
		}
		top = append(top, trend)
		if len(top) == limit {
			break
		}
	}
	return Result{
		LeagueID: request.LeagueID, LeagueSeason: request.Year, SnapSeasons: seasons,
		AvailableDefensivePlayers: len(available), ResolvedPlayers: len(modelPlayers),
		PlayersWithSnapData: len(withSnaps), UnresolvedMFLPlayerIDs: unresolved,
		Method: report.Method, Config: report.Config, Trends: top,
	}, nil
}

func normalizedSeasons(year int, seasons []int) []int {
	if len(seasons) == 0 {
		return []int{year - 3, year - 2, year - 1}
	}
	unique := make(map[int]struct{}, len(seasons))
	for _, season := range seasons {
		if season < 2012 || season > 2100 {
			return nil
		}
		unique[season] = struct{}{}
	}
	result := make([]int, 0, len(unique))
	for season := range unique {
		result = append(result, season)
	}
	sort.Ints(result)
	return result
}
