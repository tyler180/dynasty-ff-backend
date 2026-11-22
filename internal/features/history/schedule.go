package history

import (
	"context"
	"fmt"
	"strings"
)

// ScheduledGame is one NFL game as observed in a season's snap-count feed.
// The feed lists every played game, so these items double as the record of
// which NFL weeks have been played.
type ScheduledGame struct {
	Season   int      `json:"season"`
	Week     int      `json:"week"`
	GameID   string   `json:"game_id"`
	GameType string   `json:"game_type,omitempty"`
	Teams    []string `json:"teams"`
}

func (g ScheduledGame) Validate() error {
	if g.Season < 2012 || g.Season > 2100 {
		return fmt.Errorf("scheduled game season must be between 2012 and 2100")
	}
	if g.Week < 1 || g.Week > 25 {
		return fmt.Errorf("scheduled game %s week must be between 1 and 25", g.GameID)
	}
	if strings.TrimSpace(g.GameID) == "" {
		return fmt.Errorf("scheduled game ID is required")
	}
	if len(g.Teams) == 0 {
		return fmt.Errorf("scheduled game %s needs at least one team", g.GameID)
	}
	return nil
}

// IsRegularSeason matches the usage model: an empty game type is regular
// season, and postseason games have no fantasy impact.
func (g ScheduledGame) IsRegularSeason() bool {
	return IsRegularSeasonGameType(g.GameType)
}

func IsRegularSeasonGameType(gameType string) bool {
	gameType = strings.ToUpper(strings.TrimSpace(gameType))
	return gameType == "" || gameType == "REG"
}

type ScheduleWriter interface {
	// PutSeasonSchedule replaces the stored games for one season.
	PutSeasonSchedule(context.Context, int, []ScheduledGame) error
}

type ScheduleReader interface {
	// SeasonSchedules returns the stored games for the given seasons.
	SeasonSchedules(context.Context, []int) ([]ScheduledGame, error)
}
