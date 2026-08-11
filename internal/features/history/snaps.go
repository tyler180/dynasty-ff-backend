// Package history defines historical football facts consumed by analyses.
package history

import (
	"context"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
)

// PlayerGameSnaps is one player's participation in one NFL game. Percentages
// are observations from the provider; aggregates should be calculated from
// snap totals rather than by averaging game percentages.
type PlayerGameSnaps struct {
	PlayerID         player.ID `json:"player_id"`
	GameID           string    `json:"game_id"`
	Season           int       `json:"season"`
	Week             int       `json:"week"`
	GameDate         time.Time `json:"game_date"`
	Team             string    `json:"team"`
	Opponent         string    `json:"opponent"`
	Position         string    `json:"position"`
	PositionGroup    string    `json:"position_group"`
	OffenseSnaps     int       `json:"offense_snaps"`
	OffenseSnapPct   float64   `json:"offense_snap_pct"`
	DefenseSnaps     int       `json:"defense_snaps"`
	DefenseSnapPct   float64   `json:"defense_snap_pct"`
	SpecialTeamSnaps int       `json:"special_teams_snaps"`
	SpecialTeamPct   float64   `json:"special_teams_snap_pct"`
	Source           string    `json:"source"`
	IngestionRunID   string    `json:"ingestion_run_id"`
}

type SnapQuery struct {
	PlayerIDs      []player.ID
	Seasons        []int
	PositionGroups []string
}

type SnapReader interface {
	PlayerGameSnaps(context.Context, SnapQuery) ([]PlayerGameSnaps, error)
}

type SnapWriter interface {
	PutPlayerGameSnaps(context.Context, []PlayerGameSnaps) error
}
