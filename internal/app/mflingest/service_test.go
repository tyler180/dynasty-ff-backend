package mflingest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/identity"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
	"github.com/tyler180/dynasty-ff-backend/internal/provider/fantasypros"
	source "github.com/tyler180/dynasty-ff-models/analysis"
)

func TestServiceRejectsIncompleteConfiguration(t *testing.T) {
	service := Service{}
	_, err := service.Sync(context.Background(), Request{
		Year: 2026, LeagueID: "79286", FranchiseID: "0005", Timeout: time.Minute,
	})
	if err == nil {
		t.Fatal("expected configuration error")
	}
}

type fakeEvaluations []fantasypros.Evaluation

func (f fakeEvaluations) Evaluations(context.Context, int) ([]fantasypros.Evaluation, error) {
	return f, nil
}

type fakeIdentities struct {
	byMFL map[string]player.Profile
	byFP  map[string]player.Profile
}

func (f fakeIdentities) GetPlayer(context.Context, player.ID) (player.Profile, error) {
	return player.Profile{}, nil
}
func (f fakeIdentities) PutPlayer(context.Context, player.Profile) error { return nil }
func (f fakeIdentities) PutAlias(context.Context, identity.Alias) error  { return nil }
func (f fakeIdentities) ResolvePlayer(_ context.Context, id player.ExternalID) (player.Profile, error) {
	if id.Provider == player.ProviderMFL {
		return f.byMFL[id.Value], nil
	}
	return f.byFP[id.Value], nil
}
func (f fakeIdentities) ResolvePlayers(_ context.Context, ids []player.ExternalID) (map[player.ExternalID]player.Profile, error) {
	result := map[player.ExternalID]player.Profile{}
	for _, id := range ids {
		if profile, ok := f.byFP[id.Value]; ok {
			result[id] = profile
		}
	}
	return result, nil
}

func TestEnrichEvaluationsJoinsThroughCanonicalIdentities(t *testing.T) {
	rookieID := player.ID("rookie-canonical")
	veteranID := player.ID("veteran-canonical")
	identities := fakeIdentities{
		byMFL: map[string]player.Profile{
			"17000": {ID: rookieID, DisplayName: "Rookie"},
			"15751": {ID: veteranID, DisplayName: "Veteran"},
		},
		byFP: map[string]player.Profile{
			"28000": {ID: rookieID, DisplayName: "Rookie"},
			"19788": {ID: veteranID, DisplayName: "Veteran"},
		},
	}
	snapshot := source.Snapshot{
		Roster: []source.Player{{ID: "15751", Name: "Veteran"}},
		RookieCandidates: []source.RookieCandidate{{
			ID: "17000", Name: "Rookie", Position: "WR", RookieYear: 2026, RookieADP: 25.5, Source: "MFL rookie-only ADP",
		}},
	}
	warnings, err := enrichEvaluations(context.Background(), &snapshot, 2026, time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC), identities, fakeEvaluations{
		{FantasyProsID: "28000", RookieRank: 2, DynastyRank: 50, MarketValue: 4000, ProjectedPoints: 120},
		{FantasyProsID: "19788", DynastyRank: 5, MarketValue: 9000, ProjectedPoints: 250},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if got := snapshot.RookieCandidates[0]; got.MarketValue != 4000 || got.RookieADP != 25.5 || got.ProjectedPoints[2026] != 120 || !strings.Contains(got.Source, "MFL rookie-only ADP") {
		t.Fatalf("rookie = %+v", got)
	}
	if got := snapshot.Projections.ByPlayerID["15751"]; got != 250 {
		t.Fatalf("veteran projection = %v", got)
	}
}
