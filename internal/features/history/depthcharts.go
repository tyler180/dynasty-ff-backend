package history

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
)

// DepthChartObservation describes one stable interval (2025+) or weekly
// observation (through 2024) of a player's listed team depth-chart role.
type DepthChartObservation struct {
	PlayerID        player.ID `json:"player_id"`
	SourcePlayerID  string    `json:"source_player_id"`
	SourceProvider  string    `json:"source_provider"`
	PlayerName      string    `json:"player_name,omitempty"`
	Season          int       `json:"season"`
	Week            int       `json:"week,omitempty"`
	GameType        string    `json:"game_type,omitempty"`
	ObservedAt      time.Time `json:"observed_at,omitzero"`
	LastSeenAt      time.Time `json:"last_seen_at,omitzero"`
	Team            string    `json:"team"`
	PositionGroupID int       `json:"position_group_id,omitempty"`
	PositionGroup   string    `json:"position_group,omitempty"`
	PositionID      int       `json:"position_id,omitempty"`
	Position        string    `json:"position,omitempty"`
	PositionName    string    `json:"position_name,omitempty"`
	PositionSlot    int       `json:"position_slot,omitempty"`
	DepthRank       int       `json:"depth_rank"`
	Source          string    `json:"source"`
	IngestionRunID  string    `json:"ingestion_run_id"`
}

func (o DepthChartObservation) Validate() error {
	if strings.TrimSpace(string(o.PlayerID)) == "" || strings.TrimSpace(o.SourcePlayerID) == "" || strings.TrimSpace(o.SourceProvider) == "" {
		return fmt.Errorf("canonical player ID, source player ID, and source provider are required")
	}
	if o.Season < 2001 || o.Season > 2100 || strings.TrimSpace(o.Team) == "" || o.DepthRank < 1 {
		return fmt.Errorf("depth-chart season, team, or rank is invalid")
	}
	if o.Week == 0 && o.ObservedAt.IsZero() {
		return fmt.Errorf("depth-chart week or observation time is required")
	}
	if o.Week < 0 || o.Week > 25 {
		return fmt.Errorf("depth-chart week is invalid")
	}
	if !o.ObservedAt.IsZero() && (o.LastSeenAt.IsZero() || o.LastSeenAt.Before(o.ObservedAt)) {
		return fmt.Errorf("depth-chart last-seen time must not precede observation time")
	}
	if strings.TrimSpace(o.Source) == "" {
		return fmt.Errorf("depth-chart source is required")
	}
	return nil
}

type DepthChartQuery struct {
	PlayerIDs []player.ID
	Seasons   []int
}

func (q DepthChartQuery) Validate() error {
	if len(q.PlayerIDs) == 0 || len(q.PlayerIDs) > 100 {
		return fmt.Errorf("between one and 100 player IDs are required")
	}
	if len(q.Seasons) == 0 || len(q.Seasons) > 10 {
		return fmt.Errorf("between one and ten seasons are required")
	}
	for _, season := range q.Seasons {
		if season < 2001 || season > 2100 {
			return fmt.Errorf("depth-chart season is invalid")
		}
	}
	return nil
}

type DepthChartReader interface {
	DepthChartObservations(context.Context, DepthChartQuery) ([]DepthChartObservation, error)
}

type DepthChartWriter interface {
	PutDepthChartObservations(context.Context, []DepthChartObservation) error
}

type DepthChartDatasetState struct {
	Season        int
	SourceVersion string
	Version       string
	RecordCount   int
	ImportedAt    time.Time
}

type DepthChartDatasetStateStore interface {
	DepthChartDatasetState(context.Context, int) (DepthChartDatasetState, error)
	PutDepthChartDatasetState(context.Context, DepthChartDatasetState) error
}
