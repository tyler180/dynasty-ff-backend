package lambdaapp

import (
	"context"
	"testing"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/app/freeagenttrends"
	"github.com/tyler180/dynasty-ff-backend/internal/app/identitysync"
	"github.com/tyler180/dynasty-ff-backend/internal/app/mflingest"
	"github.com/tyler180/dynasty-ff-backend/internal/app/snapcountsync"
	"github.com/tyler180/dynasty-ff-backend/internal/app/snapshotanalysis"
	"github.com/tyler180/dynasty-ff-backend/internal/domain/league"
	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
	"github.com/tyler180/dynasty-ff-backend/internal/identity"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
	source "github.com/tyler180/dynasty-ff-models/analysis"
)

type fakeSnapshots struct{ snapshot league.Snapshot }

func (f *fakeSnapshots) PutSnapshot(_ context.Context, snapshot league.Snapshot) error {
	f.snapshot = snapshot
	return nil
}
func (f *fakeSnapshots) LatestSnapshot(context.Context, league.ID, league.FranchiseID, int) (league.Snapshot, error) {
	return f.snapshot, nil
}
func (f *fakeSnapshots) SnapshotAt(context.Context, league.ID, league.FranchiseID, int, time.Time) (league.Snapshot, error) {
	return f.snapshot, nil
}

type fakeIdentities struct{ profile player.Profile }

type fakeSyncer struct {
	result  mflingest.Result
	request *mflingest.Request
}

type fakeIdentitySyncer struct{ result identitysync.Result }

type fakeSyncStarter struct{ request *Request }

type fakeAnalyzer struct{ result snapshotanalysis.Result }

type fakeFreeAgentTrendAnalyzer struct {
	result  freeagenttrends.Result
	request *freeagenttrends.Request
}

type fakeSnapCounts struct {
	result     []history.PlayerGameSnaps
	syncResult snapcountsync.Result
	query      history.SnapQuery
}

func (f *fakeSnapCounts) Sync(context.Context, snapcountsync.Request) (snapcountsync.Result, error) {
	return f.syncResult, nil
}

func (f *fakeSnapCounts) PlayerGameSnaps(_ context.Context, query history.SnapQuery) ([]history.PlayerGameSnaps, error) {
	f.query = query
	return f.result, nil
}

func (f fakeAnalyzer) Analyze(context.Context, snapshotanalysis.Request) (snapshotanalysis.Result, error) {
	return f.result, nil
}

func (f fakeFreeAgentTrendAnalyzer) Analyze(_ context.Context, request freeagenttrends.Request) (freeagenttrends.Result, error) {
	if f.request != nil {
		*f.request = request
	}
	return f.result, nil
}

func (f fakeIdentitySyncer) Sync(context.Context, identitysync.Request) (identitysync.Result, error) {
	return f.result, nil
}

func (f fakeSyncStarter) Start(_ context.Context, request Request) error {
	if f.request != nil {
		*f.request = request
	}
	return nil
}

func (f fakeSyncer) Sync(_ context.Context, request mflingest.Request) (mflingest.Result, error) {
	if f.request != nil {
		*f.request = request
	}
	return f.result, nil
}

func (f *fakeIdentities) PutPlayer(_ context.Context, profile player.Profile) error {
	f.profile = profile
	return nil
}
func (f *fakeIdentities) PutAlias(context.Context, identity.Alias) error { return nil }
func (f *fakeIdentities) GetPlayer(context.Context, player.ID) (player.Profile, error) {
	return f.profile, nil
}
func (f *fakeIdentities) ResolvePlayer(context.Context, player.ExternalID) (player.Profile, error) {
	return f.profile, nil
}

func TestHandlerStoresAndReadsSnapshot(t *testing.T) {
	snapshots := &fakeSnapshots{}
	handler, err := New(snapshots, &fakeIdentities{})
	if err != nil {
		t.Fatal(err)
	}
	want := league.Snapshot{
		League: league.League{ID: "79286", Season: 2026}, Franchise: league.Franchise{ID: "0005"},
		ObservedAt: time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC), Source: "test",
	}
	if _, err := handler.Handle(context.Background(), Request{Action: ActionPutSnapshot, Snapshot: &want}); err != nil {
		t.Fatal(err)
	}
	response, err := handler.Handle(context.Background(), Request{
		Action: ActionLatestSnapshot, LeagueID: "79286", FranchiseID: "0005", Season: 2026,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Snapshot == nil || !response.Snapshot.ObservedAt.Equal(want.ObservedAt) {
		t.Fatalf("response snapshot = %+v, want observed_at %s", response.Snapshot, want.ObservedAt)
	}
}

func TestHandlerStoresAndResolvesIdentity(t *testing.T) {
	identities := &fakeIdentities{}
	handler, err := New(&fakeSnapshots{}, identities)
	if err != nil {
		t.Fatal(err)
	}
	want := player.Profile{ID: "canonical-1", DisplayName: "Test Player"}
	if _, err := handler.Handle(context.Background(), Request{Action: ActionPutPlayer, Player: &want}); err != nil {
		t.Fatal(err)
	}
	response, err := handler.Handle(context.Background(), Request{Action: ActionGetPlayer, PlayerID: want.ID})
	if err != nil {
		t.Fatal(err)
	}
	if response.Player == nil || response.Player.ID != want.ID {
		t.Fatalf("response player = %+v, want %s", response.Player, want.ID)
	}
}

func TestHandlerRunsReadOnlyMFLSync(t *testing.T) {
	handler, err := New(&fakeSnapshots{}, &fakeIdentities{})
	if err != nil {
		t.Fatal(err)
	}
	want := league.Snapshot{
		League: league.League{ID: "79286", Season: 2026}, Franchise: league.Franchise{ID: "0005"},
		ObservedAt: time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC), Source: "mfl",
	}
	handler.WithSyncer(fakeSyncer{result: mflingest.Result{Snapshot: want, SyncedAt: want.ObservedAt}})
	response, err := handler.Handle(context.Background(), Request{
		Action: ActionSyncMFL, LeagueID: "79286", FranchiseID: "0005", Season: 2026,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Snapshot == nil || !response.Snapshot.ObservedAt.Equal(want.ObservedAt) {
		t.Fatalf("response snapshot = %+v, want observed_at %s", response.Snapshot, want.ObservedAt)
	}
}

func TestHandlerCanSkipFantasyProsDuringMFLSync(t *testing.T) {
	handler, err := New(&fakeSnapshots{}, &fakeIdentities{})
	if err != nil {
		t.Fatal(err)
	}
	var captured mflingest.Request
	handler.WithSyncer(fakeSyncer{request: &captured})
	_, err = handler.Handle(context.Background(), Request{
		Action: ActionSyncMFL, LeagueID: "79286", FranchiseID: "0005", Season: 2026,
		SkipFantasyPros: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !captured.SkipEvaluations {
		t.Fatal("skip_fantasypros was not forwarded to MFL ingestion")
	}
}

func TestHandlerStartsAsynchronousDraftSync(t *testing.T) {
	handler, err := New(&fakeSnapshots{}, &fakeIdentities{})
	if err != nil {
		t.Fatal(err)
	}
	var captured Request
	handler.WithSyncStarter(fakeSyncStarter{request: &captured})
	response, err := handler.Handle(context.Background(), Request{
		Action: ActionStartMFLSync, LeagueID: "79286", FranchiseID: "0005", Season: 2026,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "accepted" || captured.Action != ActionSyncMFL || !captured.LiveDraft || !captured.SkipFantasyPros || captured.TimeoutSeconds != 120 {
		t.Fatalf("response/request = %+v / %+v", response, captured)
	}
}

func TestHandlerBootstrapsIdentityBatch(t *testing.T) {
	identities := &fakeIdentities{}
	handler, err := New(&fakeSnapshots{}, identities)
	if err != nil {
		t.Fatal(err)
	}
	response, err := handler.Handle(context.Background(), Request{
		Action:  ActionPutIdentities,
		Players: []player.Profile{{ID: "canonical-1", DisplayName: "Test Player"}},
		Aliases: []identity.Alias{{
			ExternalID: player.ExternalID{Provider: player.ProviderMFL, Value: "15751"},
			PlayerID:   "canonical-1", Source: "test", ResolutionMethod: "manual",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.StoredPlayers != 1 || response.StoredAliases != 1 {
		t.Fatalf("stored players/aliases = %d/%d, want 1/1", response.StoredPlayers, response.StoredAliases)
	}
}

func TestHandlerSyncsIdentities(t *testing.T) {
	handler, err := New(&fakeSnapshots{}, &fakeIdentities{})
	if err != nil {
		t.Fatal(err)
	}
	want := identitysync.Result{Complete: true, EligiblePlayers: 42, CreatedPlayers: 40, ExistingPlayers: 2}
	handler.WithIdentitySyncer(fakeIdentitySyncer{result: want})
	response, err := handler.Handle(context.Background(), Request{
		Action: ActionSyncIdentities, Season: 2026, LeagueID: "79286",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "stored" || response.IdentitySync == nil || response.IdentitySync.EligiblePlayers != want.EligiblePlayers {
		t.Fatalf("identity sync response = %+v", response.IdentitySync)
	}
}

func TestHandlerReportsPartialIdentitySync(t *testing.T) {
	handler, err := New(&fakeSnapshots{}, &fakeIdentities{})
	if err != nil {
		t.Fatal(err)
	}
	handler.WithIdentitySyncer(fakeIdentitySyncer{result: identitysync.Result{
		Complete: false, PartialReason: "stopped before deadline", WrittenProfiles: 12,
	}})
	response, err := handler.Handle(context.Background(), Request{
		Action: ActionSyncIdentities, Season: 2026, LeagueID: "79286",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "partial" || response.IdentitySync == nil || response.IdentitySync.Complete {
		t.Fatalf("response = %+v", response)
	}
}

func TestHandlerAnalyzesLatestSnapshot(t *testing.T) {
	handler, err := New(&fakeSnapshots{}, &fakeIdentities{})
	if err != nil {
		t.Fatal(err)
	}
	want := snapshotanalysis.Result{Analysis: source.Analysis{Team: "Team McLean"}}
	handler.WithAnalyzer(fakeAnalyzer{result: want})
	response, err := handler.Handle(context.Background(), Request{
		Action: ActionAnalyze, Season: 2026, LeagueID: "79286", FranchiseID: "0005",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "ok" || response.Analysis == nil || response.Analysis.Analysis.Team != "Team McLean" {
		t.Fatalf("response = %+v", response)
	}
}

func TestHandlerSyncsAndReadsDefensiveSnapCounts(t *testing.T) {
	handler, err := New(&fakeSnapshots{}, &fakeIdentities{})
	if err != nil {
		t.Fatal(err)
	}
	snaps := &fakeSnapCounts{
		syncResult: snapcountsync.Result{Season: 2025, StoredRecords: 1},
		result:     []history.PlayerGameSnaps{{PlayerID: "player-1", Season: 2025, Week: 1, GameID: "game-1", DefenseSnapPct: 0.83, Source: "nflverse-pfr"}},
	}
	handler.WithSnapCounts(snaps, snaps)
	response, err := handler.Handle(context.Background(), Request{Action: ActionSyncSnapCounts, Season: 2025})
	if err != nil || response.SnapCountSync == nil || response.SnapCountSync.StoredRecords != 1 {
		t.Fatalf("sync response/error = %+v / %v", response, err)
	}
	response, err = handler.Handle(context.Background(), Request{
		Action: ActionGetSnapCounts, PlayerIDs: []player.ID{"player-1"}, Seasons: []int{2025}, PositionGroups: []string{"LB"},
	})
	if err != nil || len(response.SnapCounts) != 1 || response.SnapCounts[0].DefenseSnapPct != 0.83 {
		t.Fatalf("query response/error = %+v / %v", response, err)
	}
	if len(snaps.query.PlayerIDs) != 1 || snaps.query.PlayerIDs[0] != "player-1" {
		t.Fatalf("query = %+v", snaps.query)
	}
}

func TestHandlerReturnsTopDefensiveFreeAgentTrends(t *testing.T) {
	handler, err := New(&fakeSnapshots{}, &fakeIdentities{})
	if err != nil {
		t.Fatal(err)
	}
	var captured freeagenttrends.Request
	handler.WithFreeAgentTrends(fakeFreeAgentTrendAnalyzer{
		request: &captured,
		result:  freeagenttrends.Result{LeagueID: "79286", Trends: nil},
	})
	response, err := handler.Handle(context.Background(), Request{
		Action: ActionTopDefensiveFreeAgentTrends, Season: 2026, LeagueID: "79286",
		Seasons: []int{2023, 2024, 2025}, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "ok" || response.DefensiveFreeAgentTrends == nil || response.DefensiveFreeAgentTrends.LeagueID != "79286" {
		t.Fatalf("response = %+v", response)
	}
	if captured.Year != 2026 || captured.LeagueID != "79286" || captured.Limit != 10 || len(captured.Seasons) != 3 {
		t.Fatalf("captured request = %+v", captured)
	}
}
