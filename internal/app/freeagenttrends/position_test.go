package freeagenttrends

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
	mflsync "github.com/tyler180/dynasty-ff-backend/internal/provider/mfl"
)

// positionService has one rising free agent in each of DL, LB and DB, and a
// second rising LB, so every group has a trend and LB has two.
func positionService() Service {
	type candidate struct{ mflID, id, group string }
	candidates := []candidate{
		{"100", "dl-1", "DL"}, {"101", "lb-1", "LB"}, {"102", "lb-2", "LB"}, {"103", "db-1", "DB"},
	}
	snaps := &fakeSnaps{}
	freeAgents := fakeFreeAgents{}
	identities := fakeIdentities{}
	for _, c := range candidates {
		freeAgents = append(freeAgents, mflsync.DefensiveFreeAgent{MFLID: c.mflID, Name: c.id, Position: c.group, PositionGroup: c.group})
		identities[player.ExternalID{Provider: player.ProviderMFL, Value: c.mflID}] = player.Profile{ID: player.ID(c.id), DisplayName: c.id}
		for week, count := range []int{10, 12, 14, 50, 52, 54} {
			fact := snapFact(player.ID(c.id), week+1, count)
			fact.PositionGroup = c.group
			snaps.facts = append(snaps.facts, fact)
		}
	}
	return Service{FreeAgents: freeAgents, Identities: identities, Snaps: snaps}
}

func trendIDs(result Result) []string {
	ids := make([]string, 0, len(result.Trends))
	for _, trend := range result.Trends {
		ids = append(ids, trend.PlayerID)
	}
	return ids
}

func TestAnalyzeFiltersByPositionGroup(t *testing.T) {
	tests := []struct {
		name       string
		groups     []string
		limit      int
		wantIDs    []string
		wantGroups []string
	}{
		{"no filter keeps every group", nil, 0, []string{"db-1", "dl-1", "lb-1", "lb-2"}, nil},
		{"one group", []string{"LB"}, 0, []string{"lb-1", "lb-2"}, []string{"LB"}},
		{"lower case and duplicates", []string{"db", " DB ", "dl"}, 0, []string{"db-1", "dl-1"}, []string{"DL", "DB"}},
		{"blank entries are ignored", []string{"", " "}, 0, []string{"db-1", "dl-1", "lb-1", "lb-2"}, nil},
		{"limit counts filtered players", []string{"LB", "DB"}, 2, nil, []string{"LB", "DB"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := positionService().Analyze(context.Background(), Request{
				Year: 2026, LeagueID: "79286", Seasons: []int{2025}, Limit: test.limit,
				RecencyWeeks: recencyOff(), PositionGroups: test.groups,
			})
			if err != nil {
				t.Fatal(err)
			}
			ids := trendIDs(result)
			if test.wantIDs != nil {
				sorted := append([]string(nil), ids...)
				sort.Strings(sorted)
				if !reflect.DeepEqual(sorted, test.wantIDs) {
					t.Fatalf("trends = %v, want %v", ids, test.wantIDs)
				}
			} else {
				if len(ids) != test.limit {
					t.Fatalf("trends = %v, want %d", ids, test.limit)
				}
				for _, trend := range result.Trends {
					if trend.PositionGroup == "DL" {
						t.Fatalf("DL player %s passed an LB,DB filter", trend.PlayerID)
					}
				}
			}
			if !reflect.DeepEqual(result.PositionGroups, test.wantGroups) {
				t.Fatalf("position groups = %v, want %v", result.PositionGroups, test.wantGroups)
			}
			if result.ResolvedPlayers != 4 {
				t.Fatalf("resolved = %d; the filter must not shrink the pool", result.ResolvedPlayers)
			}
		})
	}
}

func TestAnalyzeRejectsUnknownPositionGroup(t *testing.T) {
	_, err := positionService().Analyze(context.Background(), Request{
		Year: 2026, LeagueID: "79286", Seasons: []int{2025}, RecencyWeeks: recencyOff(), PositionGroups: []string{"LB", "QB"},
	})
	if err == nil || !strings.Contains(err.Error(), `position_groups must be DL, LB or DB, got "QB"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestAnalyzePositionFilterWithNoMatchesIsEmpty(t *testing.T) {
	service := positionService()
	service.FreeAgents = service.FreeAgents.(fakeFreeAgents)[:1] // DL only
	result, err := service.Analyze(context.Background(), Request{
		Year: 2026, LeagueID: "79286", Seasons: []int{2025}, RecencyWeeks: recencyOff(), PositionGroups: []string{"DB"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trends) != 0 || result.Trends == nil {
		t.Fatalf("trends = %#v, want an empty list", result.Trends)
	}
}
