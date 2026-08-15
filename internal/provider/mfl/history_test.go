package mflsync

import (
	"testing"
	"time"

	source "github.com/tyler180/dynasty-ff-models/analysis"
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
		"a": {ID: "a", Name: "A", Position: "WR", NFLTeam: "ATL"},
		"b": {ID: "b", Name: "B", Position: "WR", NFLTeam: "BUF"},
		"c": {ID: "c", Name: "C", Position: "WR", NFLTeam: "CHI"},
	}
	freeAgents := map[string]any{"freeAgents": map[string]any{"player": []any{
		map[string]any{"id": "a", "status": "free_agent"},
		map[string]any{"id": "b", "status": "locked"},
		map[string]any{"id": "c", "status": "free_agent"},
	}}}
	bids := []winningBid{
		{PlayerID: "a", Franchise: "0001", Amount: 1},
		{PlayerID: "b", Franchise: "0002", Amount: 3},
		{PlayerID: "c", Franchise: "0003", Amount: 7},
	}
	levels := replacementLevels(history, catalog, freeAgents, bids, 2026, 1)
	if levels.PointsPerGameByPosition["WR"] != 8 {
		t.Fatalf("WR replacement = %.2f, want 8", levels.PointsPerGameByPosition["WR"])
	}
	if candidates := levels.CandidatesByPosition["WR"]; len(candidates) != 3 || candidates[0].PlayerID != "a" || candidates[1].AvailabilityStatus != "locked" {
		t.Fatalf("WR replacement candidates = %+v", candidates)
	}
	if candidate := levels.CandidatesByPosition["WR"][0]; candidate.EstimatedWinningBid != 7 || candidate.BidLow != 3 || candidate.BidHigh != 7 || candidate.BidObservations != 3 {
		t.Fatalf("WR bid estimate = %+v", candidate)
	}
}

func TestWinningBidsParsesBBIDTransactions(t *testing.T) {
	payload := map[string]any{"transactions": map[string]any{"transaction": []any{
		map[string]any{"type": "BBID_WAIVER", "franchise": "0005", "transaction": "17039,|3.00|16586,"},
		map[string]any{"type": "FREE_AGENT", "franchise": "0005", "transaction": "|17039,"},
	}}}
	bids := winningBids(payload)
	if len(bids) != 1 || bids[0].PlayerID != "17039" || bids[0].Amount != 3 || bids[0].Franchise != "0005" {
		t.Fatalf("winning bids = %+v", bids)
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
