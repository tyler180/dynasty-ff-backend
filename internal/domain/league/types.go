// Package league defines provider-independent fantasy league state.
package league

import (
	"fmt"
	"strings"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
)

type ID string
type FranchiseID string

type RosterStatus string

const (
	RosterActive         RosterStatus = "active"
	RosterInjuredReserve RosterStatus = "injured_reserve"
	RosterTaxi           RosterStatus = "taxi"
)

// Snapshot is league state as observed at a point in time. Historical
// snapshots are immutable; a new observation produces a new snapshot.
type Snapshot struct {
	League                  League             `json:"league"`
	Franchise               Franchise          `json:"franchise"`
	Roster                  []RosterAssignment `json:"roster"`
	DraftAssets             []DraftAsset       `json:"draft_assets,omitempty"`
	DraftStatus             string             `json:"draft_status,omitempty"`
	DraftAvailabilityWindow AvailabilityWindow `json:"draft_availability_window,omitempty"`
	HistoricalPoints        HistoricalPoints   `json:"historical_points,omitempty"`
	ReplacementLevels       ReplacementLevels  `json:"replacement_levels,omitempty"`
	Projections             Projections        `json:"projections,omitempty"`
	RookieCandidates        []RookieCandidate  `json:"rookie_candidates,omitempty"`
	ObservedAt              time.Time          `json:"observed_at"`
	Source                  string             `json:"source"`
}

type League struct {
	ID                  ID      `json:"id"`
	Name                string  `json:"name"`
	Season              int     `json:"season"`
	SalaryCap           float64 `json:"salary_cap"`
	ActiveRosterLimit   int     `json:"active_roster_limit"`
	InjuredReserveLimit int     `json:"injured_reserve_limit"`
	TaxiSquadLimit      int     `json:"taxi_squad_limit"`
}

type Franchise struct {
	ID   FranchiseID `json:"id"`
	Name string      `json:"name"`
}

type RosterAssignment struct {
	PlayerID        player.ID    `json:"player_id"`
	Status          RosterStatus `json:"status"`
	Position        string       `json:"position,omitempty"`
	NFLTeam         string       `json:"nfl_team,omitempty"`
	Salary          float64      `json:"salary"`
	CurrentCapHit   float64      `json:"current_cap_hit"`
	ContractThrough int          `json:"contract_through,omitempty"`
	DynastyRank     float64      `json:"dynasty_rank,omitempty"`
	MarketValue     float64      `json:"market_value,omitempty"`
	MarketSource    string       `json:"market_source,omitempty"`
}

type AvailabilityWindow struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

type HistoricalPoints struct {
	Source  string             `json:"source,omitempty"`
	Seasons []HistoricalSeason `json:"seasons,omitempty"`
}

type HistoricalSeason struct {
	Season                int                `json:"season"`
	ByPlayerID            map[string]float64 `json:"by_player_id"`
	GamesPlayedByPlayerID map[string]int     `json:"games_played_by_player_id,omitempty"`
}

type ReplacementLevels struct {
	Source                  string             `json:"source,omitempty"`
	Method                  string             `json:"method,omitempty"`
	MinimumHistoricalGames  int                `json:"minimum_historical_games,omitempty"`
	PointsPerGameByPosition map[string]float64 `json:"points_per_game_by_position,omitempty"`
}

type Projections struct {
	Season     int                `json:"season,omitempty"`
	Source     string             `json:"source,omitempty"`
	ByPlayerID map[string]float64 `json:"by_player_id,omitempty"`
}

type RookieCandidate struct {
	PlayerID        player.ID       `json:"player_id"`
	Position        string          `json:"position"`
	NFLTeam         string          `json:"nfl_team,omitempty"`
	RookieYear      int             `json:"rookie_year"`
	RookieRank      float64         `json:"rookie_rank,omitempty"`
	RookieADP       float64         `json:"rookie_adp,omitempty"`
	DynastyRank     float64         `json:"dynasty_rank,omitempty"`
	MarketValue     float64         `json:"market_value,omitempty"`
	ProjectedPoints map[int]float64 `json:"projected_points,omitempty"`
	Source          string          `json:"source"`
	UpdatedAt       string          `json:"updated_at,omitempty"`
}

type DraftAsset struct {
	Season              int         `json:"season"`
	Round               int         `json:"round"`
	Pick                int         `json:"pick,omitempty"`
	Overall             int         `json:"overall,omitempty"`
	OriginalFranchiseID FranchiseID `json:"original_franchise_id,omitempty"`
	CurrentFranchiseID  FranchiseID `json:"current_franchise_id"`
	Salary              float64     `json:"salary,omitempty"`
}

func (s Snapshot) Validate() error {
	if strings.TrimSpace(string(s.League.ID)) == "" || strings.TrimSpace(string(s.Franchise.ID)) == "" {
		return fmt.Errorf("league and franchise IDs are required")
	}
	if s.League.Season < 2000 || s.League.Season > 2100 {
		return fmt.Errorf("league season is invalid")
	}
	if s.ObservedAt.IsZero() {
		return fmt.Errorf("snapshot observed_at is required")
	}
	if strings.TrimSpace(s.Source) == "" {
		return fmt.Errorf("snapshot source is required")
	}
	return nil
}
