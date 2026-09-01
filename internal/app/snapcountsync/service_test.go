package snapcountsync

import (
	"context"
	"testing"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
	"github.com/tyler180/dynasty-ff-backend/internal/provider/nflverse"
)

type fakeSource []nflverse.SnapCount

func (f fakeSource) SnapCounts(context.Context, int) ([]nflverse.SnapCount, error) { return f, nil }

type fakeIdentities map[player.ExternalID]player.Profile

func (f fakeIdentities) ResolvePlayers(_ context.Context, ids []player.ExternalID) (map[player.ExternalID]player.Profile, error) {
	result := make(map[player.ExternalID]player.Profile)
	for _, id := range ids {
		if profile, ok := f[id]; ok {
			result[id] = profile
		}
	}
	return result, nil
}

type fakeSnaps struct{ facts []history.PlayerGameSnaps }

func (f *fakeSnaps) PutPlayerGameSnaps(_ context.Context, facts []history.PlayerGameSnaps) error {
	f.facts = append([]history.PlayerGameSnaps(nil), facts...)
	return nil
}

func TestSyncStoresResolvedDefensiveSnapPercentages(t *testing.T) {
	store := &fakeSnaps{}
	pfrID := player.ExternalID{Provider: player.ProviderPFR, Value: "DeanNa00"}
	service := Service{
		Source: fakeSource{
			{GameID: "2025_01_DAL_PHI", Season: 2025, Week: 1, PFRPlayerID: "DeanNa00", Position: "LB", Team: "PHI", Opponent: "DAL", DefenseSnaps: 52, DefenseSnapPct: 0.83},
			{GameID: "2025_01_DAL_PHI", Season: 2025, Week: 1, PFRPlayerID: "FullTi00", Position: "S", Team: "PHI", Opponent: "DAL", DefenseSnaps: 63, DefenseSnapPct: 1},
			{GameID: "2025_01_DAL_PHI", Season: 2025, Week: 1, PFRPlayerID: "HurtJa00", Position: "QB", Team: "PHI", Opponent: "DAL", OffenseSnaps: 62, OffenseSnapPct: 1},
			{GameID: "2025_01_NYG_WAS", Season: 2025, Week: 1, PFRPlayerID: "UnmaTc00", Position: "CB", Team: "NYG", Opponent: "WAS", DefenseSnapPct: 0},
		},
		Identities: fakeIdentities{pfrID: {ID: "player-dean", DisplayName: "Nakobe Dean"}},
		Snaps:      store,
		Now:        func() time.Time { return time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC) },
	}
	result, err := service.Sync(context.Background(), Request{Season: 2025})
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceRecords != 4 || result.DefensiveRecords != 3 || result.StoredRecords != 1 || len(result.UnmatchedPFRPlayerIDs) != 2 {
		t.Fatalf("result = %+v", result)
	}
	if len(store.facts) != 1 || store.facts[0].DefenseSnapPct != 0.83 || store.facts[0].TeamDefenseSnaps != 63 || store.facts[0].PositionGroup != "LB" || store.facts[0].PlayerID != "player-dean" {
		t.Fatalf("facts = %+v", store.facts)
	}
}

func TestDeriveTeamDefenseSnapsUsesHighestParticipationRow(t *testing.T) {
	records := []nflverse.SnapCount{
		{GameID: "game-1", Team: "PHI", DefenseSnaps: 12, DefenseSnapPct: 0.19},
		{GameID: "game-1", Team: "PHI", DefenseSnaps: 63, DefenseSnapPct: 1},
		{GameID: "game-1", Team: "DAL", DefenseSnaps: 61, DefenseSnapPct: 0.98},
	}
	totals := deriveTeamDefenseSnaps(records)
	if totals[gameTeamKey(records[0])] != 63 || totals[gameTeamKey(records[2])] != 62 {
		t.Fatalf("totals = %+v", totals)
	}
}
