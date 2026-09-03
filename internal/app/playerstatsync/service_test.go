package playerstatsync

import (
	"context"
	"testing"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
	"github.com/tyler180/dynasty-ff-backend/internal/provider/nflverse"
)

type fakeSource struct{ records []nflverse.PlayerStat }

func (f fakeSource) PlayerStatsDataset(context.Context, int) (nflverse.PlayerStatsDataset, error) {
	return nflverse.PlayerStatsDataset{
		Records: f.records, Payload: []byte("csv"), SourceURL: "https://example.test/stats.csv", SourceVersion: "sha256:source",
	}, nil
}

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

type fakeStore struct {
	facts []history.PlayerGameStats
	state history.PlayerStatsDatasetState
}

func (f *fakeStore) PutPlayerGameStats(_ context.Context, facts []history.PlayerGameStats) error {
	f.facts = append([]history.PlayerGameStats(nil), facts...)
	return nil
}
func (f *fakeStore) PlayerStatsDatasetState(context.Context, int) (history.PlayerStatsDatasetState, error) {
	return f.state, nil
}
func (f *fakeStore) PutPlayerStatsDatasetState(_ context.Context, state history.PlayerStatsDatasetState) error {
	f.state = state
	return nil
}

func TestSyncStoresEveryMetricAndSkipsUnchangedData(t *testing.T) {
	store := &fakeStore{}
	gsisID := player.ExternalID{Provider: player.ProviderGSIS, Value: "00-001"}
	service := Service{
		Source: fakeSource{records: []nflverse.PlayerStat{
			{GSISPlayerID: "00-001", DisplayName: "A Player", Position: "LB", PositionGroup: "LB", Season: 2025, Week: 1, GameType: "REG", GameID: "game-1", Team: "PHI", Opponent: "DAL", Metrics: map[string]float64{"def_sacks": 1.5}, Attributes: map[string]string{"headshot_url": "https://example.test/a.png"}},
			{GSISPlayerID: "unmatched", Season: 2025, Week: 1, GameID: "game-1", Metrics: map[string]float64{"targets": 2}},
		}},
		Identities: fakeIdentities{gsisID: {ID: "player-1", DisplayName: "A Player"}},
		Stats:      store, State: store, Now: func() time.Time { return time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) },
	}
	first, err := service.Sync(context.Background(), Request{Season: 2025})
	if err != nil {
		t.Fatal(err)
	}
	if first.StoredRecords != 1 || first.UnmatchedGSISPlayers != 1 || store.facts[0].Metrics["def_sacks"] != 1.5 || store.facts[0].Attributes["headshot_url"] == "" {
		t.Fatalf("result/facts = %+v / %+v", first, store.facts)
	}
	store.facts = nil
	second, err := service.Sync(context.Background(), Request{Season: 2025})
	if err != nil || !second.Unchanged || second.StoredRecords != 0 || len(store.facts) != 0 {
		t.Fatalf("second sync = %+v, facts = %+v, err = %v", second, store.facts, err)
	}
}
