package history

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
)

// PlayerGameStats is one canonical player's complete nflverse weekly-stat row.
// Numeric metrics are separated from textual attributes so later models can
// consume new source columns without a storage migration.
type PlayerGameStats struct {
	PlayerID       player.ID          `json:"player_id"`
	SourcePlayerID string             `json:"source_player_id"`
	PlayerName     string             `json:"player_name,omitempty"`
	DisplayName    string             `json:"display_name,omitempty"`
	Position       string             `json:"position,omitempty"`
	PositionGroup  string             `json:"position_group,omitempty"`
	Season         int                `json:"season"`
	Week           int                `json:"week"`
	GameType       string             `json:"game_type,omitempty"`
	GameID         string             `json:"game_id"`
	Team           string             `json:"team,omitempty"`
	Opponent       string             `json:"opponent,omitempty"`
	Metrics        map[string]float64 `json:"metrics,omitempty"`
	Attributes     map[string]string  `json:"attributes,omitempty"`
	Source         string             `json:"source"`
	IngestionRunID string             `json:"ingestion_run_id"`
}

func (s PlayerGameStats) Validate() error {
	if strings.TrimSpace(string(s.PlayerID)) == "" || strings.TrimSpace(s.SourcePlayerID) == "" || strings.TrimSpace(s.GameID) == "" {
		return fmt.Errorf("canonical player ID, source player ID, and game ID are required")
	}
	if s.Season < 1999 || s.Season > 2100 || s.Week < 1 || s.Week > 25 {
		return fmt.Errorf("player-stat season or week is invalid")
	}
	if strings.TrimSpace(s.Source) == "" {
		return fmt.Errorf("player-stat source is required")
	}
	for name, value := range s.Metrics {
		if strings.TrimSpace(name) == "" || math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("player-stat metrics must have names and finite values")
		}
	}
	for name := range s.Attributes {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("player-stat attributes must have names")
		}
	}
	return nil
}

type PlayerStatsQuery struct {
	PlayerIDs []player.ID
	Seasons   []int
}

func (q PlayerStatsQuery) Validate() error {
	if len(q.PlayerIDs) == 0 || len(q.PlayerIDs) > 100 {
		return fmt.Errorf("between one and 100 player IDs are required")
	}
	if len(q.Seasons) == 0 || len(q.Seasons) > 10 {
		return fmt.Errorf("between one and ten seasons are required")
	}
	for _, season := range q.Seasons {
		if season < 1999 || season > 2100 {
			return fmt.Errorf("player-stat season is invalid")
		}
	}
	return nil
}

type PlayerStatsReader interface {
	PlayerGameStats(context.Context, PlayerStatsQuery) ([]PlayerGameStats, error)
}

type PlayerStatsWriter interface {
	PutPlayerGameStats(context.Context, []PlayerGameStats) error
}

type PlayerStatsDatasetState struct {
	Season        int
	SourceVersion string
	Version       string
	RecordCount   int
	ImportedAt    time.Time
}

type PlayerStatsDatasetStateStore interface {
	PlayerStatsDatasetState(context.Context, int) (PlayerStatsDatasetState, error)
	PutPlayerStatsDatasetState(context.Context, PlayerStatsDatasetState) error
}
