package history

import (
	"testing"

	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
)

func TestPlayerGameSnapsValidatesPercentages(t *testing.T) {
	fact := PlayerGameSnaps{
		PlayerID: "player-1", GameID: "2025_01_DAL_PHI", Season: 2025, Week: 1,
		DefenseSnaps: 52, DefenseSnapPct: 0.83, Source: "nflverse-pfr",
	}
	if err := fact.Validate(); err != nil {
		t.Fatal(err)
	}
	fact.DefenseSnapPct = 83
	if err := fact.Validate(); err == nil {
		t.Fatal("expected percentage outside 0-1 to fail")
	}
}

func TestSnapQueryRequiresCanonicalPlayersAndSeasons(t *testing.T) {
	query := SnapQuery{PlayerIDs: []player.ID{"player-1"}, Seasons: []int{2025}}
	if err := query.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (SnapQuery{Seasons: []int{2025}}).Validate(); err == nil {
		t.Fatal("expected missing players to fail")
	}
}
