package depthchartsync

import (
	"context"
	"testing"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
	"github.com/tyler180/dynasty-ff-backend/internal/provider/nflverse"
)

type fakeSource struct{ records []nflverse.DepthChartRecord }

func (f fakeSource) DepthChartsDataset(context.Context, int) (nflverse.DepthChartsDataset, error) {
	return nflverse.DepthChartsDataset{
		Records: f.records, SourceRows: 10, Payload: []byte("csv"),
		SourceURL: "https://example.test/depth.csv", SourceVersion: "sha256:source",
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
	facts []history.DepthChartObservation
	state history.DepthChartDatasetState
}

func (f *fakeStore) PutDepthChartObservations(_ context.Context, facts []history.DepthChartObservation) error {
	f.facts = append([]history.DepthChartObservation(nil), facts...)
	return nil
}

func (f *fakeStore) DepthChartDatasetState(context.Context, int) (history.DepthChartDatasetState, error) {
	return f.state, nil
}

func (f *fakeStore) PutDepthChartDatasetState(_ context.Context, state history.DepthChartDatasetState) error {
	f.state = state
	return nil
}

func TestSyncResolvesGSISAndESPNAndSkipsUnchangedData(t *testing.T) {
	store := &fakeStore{}
	gsis := player.ExternalID{Provider: player.ProviderGSIS, Value: "00-001"}
	espn := player.ExternalID{Provider: player.ProviderESPN, Value: "123"}
	service := Service{
		Source: fakeSource{records: []nflverse.DepthChartRecord{
			{GSISPlayerID: gsis.Value, PlayerName: "A Player", Season: 2026, ObservedAt: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC), LastSeenAt: time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC), Team: "PHI", Position: "LDE", PositionSlot: 1, DepthRank: 1},
			{ESPNPlayerID: espn.Value, PlayerName: "B Player", Season: 2026, ObservedAt: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC), LastSeenAt: time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC), Team: "PHI", Position: "RDE", PositionSlot: 2, DepthRank: 2},
			{GSISPlayerID: "unmatched", Season: 2026, ObservedAt: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC), LastSeenAt: time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC), Team: "PHI", DepthRank: 3},
		}},
		Identities: fakeIdentities{gsis: {ID: "player-1", DisplayName: "A Player"}, espn: {ID: "player-2", DisplayName: "B Player"}},
		Charts:     store, State: store, Now: func() time.Time { return time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC) },
	}
	first, err := service.Sync(context.Background(), Request{Season: 2026})
	if err != nil {
		t.Fatal(err)
	}
	if first.SourceRows != 10 || first.StoredObservations != 2 || first.UnmatchedExternalPlayers != 1 || len(store.facts) != 2 {
		t.Fatalf("result/facts = %+v / %+v", first, store.facts)
	}
	if store.facts[1].SourceProvider != "espn" || store.facts[1].PlayerID != "player-2" {
		t.Fatalf("ESPN fact = %+v", store.facts[1])
	}
	store.facts = nil
	second, err := service.Sync(context.Background(), Request{Season: 2026})
	if err != nil || !second.Unchanged || second.StoredObservations != 0 || len(store.facts) != 0 {
		t.Fatalf("second sync = %+v, facts = %+v, err = %v", second, store.facts, err)
	}
}
