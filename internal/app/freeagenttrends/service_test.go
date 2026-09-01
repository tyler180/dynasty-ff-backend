package freeagenttrends

import (
	"context"
	"reflect"
	"testing"

	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
	mflsync "github.com/tyler180/dynasty-ff-backend/internal/provider/mfl"
)

type fakeFreeAgents []mflsync.DefensiveFreeAgent

func (f fakeFreeAgents) DefensiveFreeAgents(context.Context, int, string) ([]mflsync.DefensiveFreeAgent, error) {
	return f, nil
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

type fakeSnaps struct {
	facts   []history.PlayerGameSnaps
	queries []history.SnapQuery
}

func (f *fakeSnaps) PlayerGameSnaps(_ context.Context, query history.SnapQuery) ([]history.PlayerGameSnaps, error) {
	f.queries = append(f.queries, query)
	allowed := make(map[player.ID]struct{}, len(query.PlayerIDs))
	for _, id := range query.PlayerIDs {
		allowed[id] = struct{}{}
	}
	var result []history.PlayerGameSnaps
	for _, fact := range f.facts {
		if _, ok := allowed[fact.PlayerID]; ok {
			result = append(result, fact)
		}
	}
	return result, nil
}

func TestAnalyzeReturnsOnlyTopRisingResolvedFreeAgents(t *testing.T) {
	available := fakeFreeAgents{
		{MFLID: "100", Name: "Rising Linebacker", Position: "LB", PositionGroup: "LB"},
		{MFLID: "200", Name: "Stable Safety", Position: "S", PositionGroup: "DB"},
		{MFLID: "300", Name: "Missing Alias", Position: "DE", PositionGroup: "DL"},
	}
	identities := fakeIdentities{
		{Provider: player.ProviderMFL, Value: "100"}: {ID: "player-rising", DisplayName: "Rising Linebacker"},
		{Provider: player.ProviderMFL, Value: "200"}: {ID: "player-stable", DisplayName: "Stable Safety"},
	}
	snaps := &fakeSnaps{}
	for week, count := range []int{10, 12, 14, 50, 52, 54} {
		snaps.facts = append(snaps.facts, snapFact("player-rising", week+1, count))
	}
	for week, count := range []int{40, 41, 39, 40, 42, 38} {
		snaps.facts = append(snaps.facts, snapFact("player-stable", week+1, count))
	}

	result, err := (Service{FreeAgents: available, Identities: identities, Snaps: snaps}).Analyze(context.Background(), Request{
		Year: 2026, LeagueID: "79286", Seasons: []int{2025}, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trends) != 1 || result.Trends[0].PlayerID != "player-rising" || result.Trends[0].Signal != "rising" {
		t.Fatalf("trends = %+v", result.Trends)
	}
	if result.AvailableDefensivePlayers != 3 || result.ResolvedPlayers != 2 || result.PlayersWithSnapData != 2 {
		t.Fatalf("result counts = %+v", result)
	}
	if !reflect.DeepEqual(result.UnresolvedMFLPlayerIDs, []string{"300"}) {
		t.Fatalf("unresolved MFL IDs = %v", result.UnresolvedMFLPlayerIDs)
	}
	if len(snaps.queries) != 1 || !reflect.DeepEqual(snaps.queries[0].Seasons, []int{2025}) {
		t.Fatalf("snap queries = %+v", snaps.queries)
	}
}

func TestAnalyzeDefaultsToPriorThreeSeasons(t *testing.T) {
	available := fakeFreeAgents{{MFLID: "100", Name: "Player", Position: "LB", PositionGroup: "LB"}}
	identities := fakeIdentities{
		{Provider: player.ProviderMFL, Value: "100"}: {ID: "player-1", DisplayName: "Player"},
	}
	snaps := &fakeSnaps{}
	_, err := (Service{FreeAgents: available, Identities: identities, Snaps: snaps}).Analyze(context.Background(), Request{
		Year: 2026, LeagueID: "79286",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps.queries) != 1 || !reflect.DeepEqual(snaps.queries[0].Seasons, []int{2023, 2024, 2025}) {
		t.Fatalf("snap queries = %+v", snaps.queries)
	}
}

func snapFact(id player.ID, week, defenseSnaps int) history.PlayerGameSnaps {
	return history.PlayerGameSnaps{
		PlayerID: id, GameID: "2025_" + string(rune('A'+week)), Season: 2025, Week: week,
		GameType: "REG", PositionGroup: "LB", DefenseSnaps: defenseSnaps, TeamDefenseSnaps: 60,
	}
}
