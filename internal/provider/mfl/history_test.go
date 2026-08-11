package mflsync

import (
	"testing"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/analysis/source"
)

func TestPlayerScoresAndReplacementLevels(t *testing.T) {
	payload := map[string]any{"playerScores": map[string]any{"playerScore": []any{
		map[string]any{"id": "a", "score": "160.5"},
		map[string]any{"id": "b", "score": "144"},
	}}}
	if scores := playerScores(payload); scores["a"] != 160.5 || scores["b"] != 144 {
		t.Fatalf("scores = %#v", scores)
	}

	history := source.HistoricalPoints{Seasons: []source.HistoricalSeason{{
		Season:                2025,
		ByPlayerID:            map[string]float64{"a": 160, "b": 144, "c": 128},
		GamesPlayedByPlayerID: map[string]int{"a": 16, "b": 16, "c": 16},
	}}}
	catalog := map[string]catalogPlayer{
		"a": {ID: "a", Position: "WR", NFLTeam: "ATL"},
		"b": {ID: "b", Position: "WR", NFLTeam: "BUF"},
		"c": {ID: "c", Position: "WR", NFLTeam: "CHI"},
	}
	levels := replacementLevels(history, catalog, map[string]bool{"a": true, "b": true, "c": true})
	if levels.PointsPerGameByPosition["WR"] != 8 {
		t.Fatalf("WR replacement = %.2f, want 8", levels.PointsPerGameByPosition["WR"])
	}
}

func TestLoadLeagueConfig(t *testing.T) {
	config, err := LoadLeagueConfig("../../../config/league-79286.json", "79286")
	if err != nil {
		t.Fatal(err)
	}
	base := NewBase(2026, "79286", "0005", time.Time{})
	ApplyLeagueConfig(&base, config)
	if !base.HasSalaryMultipliers || !base.HasRookieSalarySchedule || salaryForPick(base.Raw, 1, 6) != 15 {
		t.Fatalf("league config was not applied: %+v", base)
	}
}
