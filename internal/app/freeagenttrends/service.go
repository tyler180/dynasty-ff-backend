// Package freeagenttrends ranks current MFL defensive free agents by sustained
// increases in their defensive snap participation.
package freeagenttrends

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

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

	// DefaultRecencyWeeks is how many of the most recent NFL regular-season
	// weeks a free agent's recent games must fall within.
	DefaultRecencyWeeks = 6
	maxRecencyWeeks     = 18
	// recentGames matches the usage model's default recent window, which this
	// service does not override.
	recentGames = 3
)

type FreeAgentSource interface {
	DefensiveFreeAgents(context.Context, int, string) ([]mflsync.DefensiveFreeAgent, error)
}

type Request struct {
	Year     int
	LeagueID string
	Seasons  []int
	Limit    int
	// MaxAge, when positive, keeps only players younger than MaxAge today.
	// Players without a known birthdate are kept with an unknown age.
	MaxAge int
	// RecencyWeeks keeps only players whose last three regular-season games
	// all fall within the most recent RecencyWeeks NFL regular-season weeks
	// played. Nil uses DefaultRecencyWeeks; zero disables the filter.
	RecencyWeeks *int
}

// RecencyWindow is the oldest NFL regular-season week a player's recent games
// may fall in.
type RecencyWindow struct {
	Weeks      int `json:"weeks"`
	FromSeason int `json:"from_season"`
	FromWeek   int `json:"from_week"`
}

type Result struct {
	LeagueID                  string   `json:"league_id"`
	LeagueSeason              int      `json:"league_season"`
	SnapSeasons               []int    `json:"snap_seasons"`
	AvailableDefensivePlayers int      `json:"available_defensive_players"`
	ResolvedPlayers           int      `json:"resolved_players"`
	PlayersWithSnapData       int      `json:"players_with_snap_data"`
	UnresolvedMFLPlayerIDs    []string `json:"unresolved_mfl_player_ids,omitempty"`
	ExcludedByAge             int      `json:"excluded_by_age"`
	// ExcludedByRecency counts players whose recent games fall outside the
	// window; they are left out of Trends whatever their signal.
	ExcludedByRecency int                 `json:"excluded_by_recency"`
	RecencyWindow     *RecencyWindow      `json:"recency_window,omitempty"`
	Method            string              `json:"method"`
	Config            usage.Config        `json:"config"`
	Trends            []usage.PlayerTrend `json:"trends"`
}

type Service struct {
	FreeAgents FreeAgentSource
	Identities identity.BulkResolver
	Snaps      history.SnapReader
	// Schedule supplies the NFL weeks played; required unless recency is off.
	Schedule history.ScheduleReader
	// Now returns the current time; ages are computed on its UTC date. Nil uses time.Now.
	Now func() time.Time
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
	if request.MaxAge < 0 || request.MaxAge > 60 {
		return Result{}, fmt.Errorf("max_age must be between 0 (no filter) and 60")
	}
	recencyWeeks := DefaultRecencyWeeks
	if request.RecencyWeeks != nil {
		recencyWeeks = *request.RecencyWeeks
	}
	if recencyWeeks < 0 || recencyWeeks > maxRecencyWeeks {
		return Result{}, fmt.Errorf("recency_weeks must be between 0 (no filter) and %d", maxRecencyWeeks)
	}
	if recencyWeeks > 0 && s.Schedule == nil {
		return Result{}, fmt.Errorf("an NFL schedule source is required for recency filtering")
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
		modelPlayer := usage.Player{
			ID: string(profile.ID), Name: profile.DisplayName,
			Position: candidate.Position, PositionGroup: candidate.PositionGroup,
		}
		if profile.BirthDate != nil {
			modelPlayer.BirthdateUnix = profile.BirthDate.Unix()
		}
		modelPlayers = append(modelPlayers, modelPlayer)
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
	withSnaps := make(map[string]struct{})
	for _, fact := range facts {
		withSnaps[string(fact.PlayerID)] = struct{}{}
	}
	// Recency filters the output, not the model input: the model always gets
	// the full pool, so its config and method are reported as usual.
	var window *RecencyWindow
	stale := map[player.ID]struct{}{}
	if recencyWeeks > 0 {
		window, err = s.recencyWindow(ctx, request.Year, recencyWeeks)
		if err != nil {
			return Result{}, err
		}
		stale = staleByRecency(facts, *window)
	}
	observations := make([]usage.Observation, 0, len(facts))
	for _, fact := range facts {
		observations = append(observations, usage.Observation{
			PlayerID: string(fact.PlayerID), GameID: fact.GameID, Season: fact.Season, Week: fact.Week,
			GameType: fact.GameType, PositionGroup: fact.PositionGroup,
			DefenseSnaps: fact.DefenseSnaps, TeamDefenseSnaps: fact.TeamDefenseSnaps,
			DefenseSnapPct: fact.DefenseSnapPct,
		})
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	report, err := usage.Analyze(usage.Input{
		Players: modelPlayers, Observations: observations,
		Config: usage.Config{MaxAge: request.MaxAge, AgeAsOf: now().UTC().Format("2006-01-02")},
	})
	if err != nil {
		return Result{}, fmt.Errorf("analyze defensive snap trends: %w", err)
	}
	top := make([]usage.PlayerTrend, 0, limit)
	for _, trend := range report.Trends {
		if trend.Signal != usage.SignalRising {
			continue
		}
		if _, ok := stale[player.ID(trend.PlayerID)]; ok {
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
		ExcludedByAge: report.Summary.ExcludedByAge, ExcludedByRecency: len(stale), RecencyWindow: window,
		Method: report.Method, Config: report.Config, Trends: top,
	}, nil
}

type seasonWeek struct{ season, week int }

func (w seasonWeek) before(other seasonWeek) bool {
	return w.season < other.season || (w.season == other.season && w.week < other.week)
}

// recencyWindow finds the oldest of the most recent `weeks` NFL
// regular-season weeks played, across the league season and the one before.
func (s Service) recencyWindow(ctx context.Context, year, weeks int) (*RecencyWindow, error) {
	games, err := s.Schedule.SeasonSchedules(ctx, []int{year - 1, year})
	if err != nil {
		return nil, fmt.Errorf("load NFL schedule: %w", err)
	}
	played := make(map[seasonWeek]struct{})
	for _, game := range games {
		if game.IsRegularSeason() {
			played[seasonWeek{game.Season, game.Week}] = struct{}{}
		}
	}
	if len(played) == 0 {
		return nil, fmt.Errorf("no NFL regular-season games stored for %d-%d; run sync_snap_counts for those seasons", year-1, year)
	}
	ordered := make([]seasonWeek, 0, len(played))
	for week := range played {
		ordered = append(ordered, week)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[j].before(ordered[i]) })
	oldest := ordered[min(weeks, len(ordered))-1]
	return &RecencyWindow{Weeks: weeks, FromSeason: oldest.season, FromWeek: oldest.week}, nil
}

// staleByRecency returns players whose last three regular-season games are
// not all within the window. Players with no regular-season games are left
// for the model, which reports them as lacking data.
func staleByRecency(facts []history.PlayerGameSnaps, window RecencyWindow) map[player.ID]struct{} {
	games := make(map[player.ID][]seasonWeek)
	for _, fact := range facts {
		if history.IsRegularSeasonGameType(fact.GameType) {
			games[fact.PlayerID] = append(games[fact.PlayerID], seasonWeek{fact.Season, fact.Week})
		}
	}
	from := seasonWeek{window.FromSeason, window.FromWeek}
	stale := make(map[player.ID]struct{})
	for id, played := range games {
		sort.Slice(played, func(i, j int) bool { return played[j].before(played[i]) })
		for _, game := range played[:min(recentGames, len(played))] {
			if game.before(from) {
				stale[id] = struct{}{}
				break
			}
		}
	}
	return stale
}

func normalizedSeasons(year int, seasons []int) []int {
	if len(seasons) == 0 {
		// Include the league season itself so the most recent games are used; the
		// model windows span season boundaries early in a season.
		return []int{year - 2, year - 1, year}
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
