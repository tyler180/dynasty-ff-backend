package freeagenttrends

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
	"github.com/tyler180/dynasty-ff-models/usage"
)

func pfrFact(id player.ID, season, week int, pfrID string) history.PlayerGameSnaps {
	return history.PlayerGameSnaps{PlayerID: id, Season: season, Week: week, SourcePlayerID: pfrID}
}

func TestLatestPFRPlayerIDs(t *testing.T) {
	tests := []struct {
		name  string
		facts []history.PlayerGameSnaps
		want  map[player.ID]string
	}{
		{"one ID", []history.PlayerGameSnaps{pfrFact("p", 2026, 1, "WilsMa01"), pfrFact("p", 2026, 2, "WilsMa01")},
			map[player.ID]string{"p": "WilsMa01"}},
		{"most recent fact wins regardless of order", []history.PlayerGameSnaps{
			pfrFact("p", 2026, 3, "NewxId00"), pfrFact("p", 2025, 18, "OldxId00"), pfrFact("p", 2026, 1, "OldxId00"),
		}, map[player.ID]string{"p": "NewxId00"}},
		{"blank IDs are ignored", []history.PlayerGameSnaps{pfrFact("p", 2026, 1, "WilsMa01"), pfrFact("p", 2026, 5, "  ")},
			map[player.ID]string{"p": "WilsMa01"}},
		{"no ID at all", []history.PlayerGameSnaps{pfrFact("p", 2026, 1, "")}, map[player.ID]string{}},
		{"players kept apart", []history.PlayerGameSnaps{pfrFact("a", 2026, 1, "AaaaAa00"), pfrFact("b", 2026, 1, "BbbbBb00")},
			map[player.ID]string{"a": "AaaaAa00", "b": "BbbbBb00"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := latestPFRPlayerIDs(test.facts); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ids = %v, want %v", got, test.want)
			}
		})
	}
}

func TestAnalyzeAttachesPFRPlayerID(t *testing.T) {
	snaps := &fakeSnaps{}
	for week, count := range []int{10, 12, 14, 50, 52, 54} {
		fact := snapFact("player-rising", week+1, count)
		fact.SourcePlayerID = "WilsMa01"
		snaps.facts = append(snaps.facts, fact)
	}
	service := Service{
		FreeAgents: fakeFreeAgents{{MFLID: "100", Name: "Marco Wilson", Position: "CB", PositionGroup: "DB"}},
		Identities: fakeIdentities{{Provider: player.ProviderMFL, Value: "100"}: {ID: "player-rising", DisplayName: "Marco Wilson"}},
		Snaps:      snaps,
	}
	result, err := service.Analyze(context.Background(), Request{Year: 2026, LeagueID: "79286", Seasons: []int{2025}, RecencyWeeks: recencyOff()})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trends) != 1 || result.Trends[0].PFRPlayerID != "WilsMa01" || result.Trends[0].PlayerID != "player-rising" {
		t.Fatalf("trends = %+v", result.Trends)
	}
}

func TestTrendJSONStaysFlat(t *testing.T) {
	encoded, err := json.Marshal(Trend{PlayerTrend: usage.PlayerTrend{PlayerID: "player-1", Name: "Player"}, PFRPlayerID: "PlayAa00"})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["player_id"] != "player-1" || fields["name"] != "Player" || fields["pfr_player_id"] != "PlayAa00" {
		t.Fatalf("trend JSON = %s", encoded)
	}
	if _, nested := fields["PlayerTrend"]; nested {
		t.Fatalf("model trend must be inlined, got %s", encoded)
	}
	withoutID, _ := json.Marshal(Trend{PlayerTrend: usage.PlayerTrend{PlayerID: "player-1"}})
	var withoutFields map[string]any
	if err := json.Unmarshal(withoutID, &withoutFields); err != nil {
		t.Fatal(err)
	}
	if _, present := withoutFields["pfr_player_id"]; present {
		t.Fatalf("missing ID should be omitted: %s", withoutID)
	}
}
