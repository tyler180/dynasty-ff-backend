// Package history defines historical football facts consumed by analyses.
package history

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
)

// PlayerGameSnaps is one player's participation in one NFL game. Percentages
// are observations from the provider; aggregates should be calculated from
// snap totals rather than by averaging game percentages.
type PlayerGameSnaps struct {
	PlayerID         player.ID `json:"player_id"`
	SourcePlayerID   string    `json:"source_player_id,omitempty"`
	PlayerName       string    `json:"player_name,omitempty"`
	GameID           string    `json:"game_id"`
	SourceGameID     string    `json:"source_game_id,omitempty"`
	Season           int       `json:"season"`
	Week             int       `json:"week"`
	GameDate         time.Time `json:"game_date,omitzero"`
	GameType         string    `json:"game_type,omitempty"`
	Team             string    `json:"team"`
	Opponent         string    `json:"opponent"`
	Position         string    `json:"position"`
	PositionGroup    string    `json:"position_group"`
	OffenseSnaps     int       `json:"offense_snaps"`
	OffenseSnapPct   float64   `json:"offense_snap_pct"`
	DefenseSnaps     int       `json:"defense_snaps"`
	TeamDefenseSnaps int       `json:"team_defense_snaps,omitempty"`
	DefenseSnapPct   float64   `json:"defense_snap_pct"`
	SpecialTeamSnaps int       `json:"special_teams_snaps"`
	SpecialTeamPct   float64   `json:"special_teams_snap_pct"`
	Source           string    `json:"source"`
	IngestionRunID   string    `json:"ingestion_run_id"`
}

func (s PlayerGameSnaps) Validate() error {
	if strings.TrimSpace(string(s.PlayerID)) == "" || strings.TrimSpace(s.GameID) == "" {
		return fmt.Errorf("player ID and game ID are required")
	}
	if s.Season < 2012 || s.Season > 2100 || s.Week < 1 || s.Week > 25 {
		return fmt.Errorf("snap season or week is invalid")
	}
	if s.OffenseSnaps < 0 || s.DefenseSnaps < 0 || s.SpecialTeamSnaps < 0 {
		return fmt.Errorf("snap counts cannot be negative")
	}
	if s.TeamDefenseSnaps < 0 || (s.TeamDefenseSnaps > 0 && s.DefenseSnaps > s.TeamDefenseSnaps) {
		return fmt.Errorf("team defensive snaps must be at least the player's defensive snaps")
	}
	for _, percentage := range []float64{s.OffenseSnapPct, s.DefenseSnapPct, s.SpecialTeamPct} {
		if percentage < 0 || percentage > 1 {
			return fmt.Errorf("snap percentages must be between 0 and 1")
		}
	}
	if strings.TrimSpace(s.Source) == "" {
		return fmt.Errorf("snap source is required")
	}
	return nil
}

type SnapQuery struct {
	PlayerIDs      []player.ID
	Seasons        []int
	PositionGroups []string
}

func (q SnapQuery) Validate() error {
	if len(q.PlayerIDs) == 0 {
		return fmt.Errorf("at least one player ID is required")
	}
	if len(q.PlayerIDs) > 100 {
		return fmt.Errorf("no more than 100 player IDs can be queried at once")
	}
	if len(q.Seasons) == 0 {
		return fmt.Errorf("at least one season is required")
	}
	if len(q.Seasons) > 10 {
		return fmt.Errorf("no more than 10 seasons can be queried at once")
	}
	for _, season := range q.Seasons {
		if season < 2012 || season > 2100 {
			return fmt.Errorf("snap season is invalid")
		}
	}
	for _, group := range q.PositionGroups {
		switch strings.ToUpper(strings.TrimSpace(group)) {
		case "DL", "LB", "DB":
		default:
			return fmt.Errorf("position groups must be DL, LB, or DB")
		}
	}
	return nil
}

type SnapReader interface {
	PlayerGameSnaps(context.Context, SnapQuery) ([]PlayerGameSnaps, error)
}

type SnapWriter interface {
	PutPlayerGameSnaps(context.Context, []PlayerGameSnaps) error
}

// SnapDatasetState tracks the content version of one imported season. The
// version is derived from normalized source records, so repeated scheduled
// syncs can avoid rewriting an unchanged dataset.
type SnapDatasetState struct {
	Season        int
	SourceVersion string
	Version       string
	RecordCount   int
	ImportedAt    time.Time
}

type SnapDatasetStateStore interface {
	SnapDatasetState(context.Context, int) (SnapDatasetState, error)
	PutSnapDatasetState(context.Context, SnapDatasetState) error
}

type SourceFile struct {
	Dataset     string
	Season      int
	Version     string
	SourceURL   string
	ContentType string
	Payload     []byte
}

type SourceFileWriter interface {
	PutSourceFile(context.Context, SourceFile) error
}
