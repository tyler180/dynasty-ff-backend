package mflingest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/domain/league"
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

func TestCarryForwardEnrichmentPreservesFreshLiveData(t *testing.T) {
	current := league.Snapshot{
		DraftAssets:      []league.DraftAsset{{Season: 2026, Round: 2, Pick: 6}},
		RookieCandidates: []league.RookieCandidate{{PlayerID: "rookie", RookieYear: 2026, RookieADP: 7.5}},
	}
	prior := league.Snapshot{
		DraftAssets:      []league.DraftAsset{{Season: 2026, Round: 1, Pick: 6}},
		RookieCandidates: []league.RookieCandidate{{PlayerID: "old-rookie", RookieYear: 2026}},
		HistoricalPoints: league.HistoricalPoints{Seasons: []league.HistoricalSeason{{Season: 2025}}},
		ReplacementLevels: league.ReplacementLevels{CandidatesByPosition: map[string][]league.ReplacementCandidate{
			"WR": {{PlayerID: "replacement", Name: "Replacement", Position: "WR"}},
		}},
		Projections: league.Projections{Season: 2026, ByPlayerID: map[string]float64{"veteran": 100}},
	}

	carryForwardEnrichment(&current, prior)
	if len(current.HistoricalPoints.Seasons) != 1 || len(current.ReplacementLevels.CandidatesByPosition["WR"]) != 1 || current.Projections.ByPlayerID["veteran"] != 100 {
		t.Fatalf("enrichment was not preserved: %+v", current)
	}
	if current.DraftAssets[0].Pick != 6 || current.RookieCandidates[0].RookieADP != 7.5 {
		t.Fatalf("fresh live data was overwritten: %+v / %+v", current.DraftAssets, current.RookieCandidates)
	}
}

func TestEnrichEvaluationsWarnsWhenRosterMarketCoverageIsIncomplete(t *testing.T) {
	valuedID := player.ID("valued-canonical")
	missingID := player.ID("missing-canonical")
	identities := fakeIdentities{
		byMFL: map[string]player.Profile{
			"15751": {ID: valuedID, DisplayName: "Valued"},
			"99999": {ID: missingID, DisplayName: "Missing"},
		},
		byFP: map[string]player.Profile{
			"19788": {ID: valuedID, DisplayName: "Valued"},
		},
	}
	snapshot := source.Snapshot{Roster: []source.Player{
		{ID: "15751", Name: "Valued"},
		{ID: "99999", Name: "Missing"},
	}}
	warnings, err := enrichEvaluations(context.Background(), &snapshot, 2026, time.Now(), identities, fakeEvaluations{
		{FantasyProsID: "19788", DynastyRank: 5, MarketValue: 9000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "valued 1 of 2 rostered players") {
		t.Fatalf("warnings = %v", warnings)
	}
}

type fakeEvaluations []fantasypros.Evaluation

func (f fakeEvaluations) Evaluations(context.Context, int) ([]fantasypros.Evaluation, error) {
	return f, nil
}

type mutableIdentities struct {
	profiles map[player.ID]player.Profile
	aliases  map[player.ExternalID]player.ID
}

func (m *mutableIdentities) GetPlayer(_ context.Context, id player.ID) (player.Profile, error) {
	return m.profiles[id], nil
}
func (m *mutableIdentities) PutPlayer(_ context.Context, profile player.Profile) error {
	m.profiles[profile.ID] = profile
	return nil
}
func (m *mutableIdentities) PutAlias(_ context.Context, alias identity.Alias) error {
	m.aliases[alias.ExternalID] = alias.PlayerID
	return nil
}
func (m *mutableIdentities) ResolvePlayer(_ context.Context, externalID player.ExternalID) (player.Profile, error) {
	return m.profiles[m.aliases[externalID]], nil
}
func (m *mutableIdentities) ResolvePlayers(_ context.Context, externalIDs []player.ExternalID) (map[player.ExternalID]player.Profile, error) {
	result := make(map[player.ExternalID]player.Profile)
	for _, externalID := range externalIDs {
		if id, found := m.aliases[externalID]; found {
			result[externalID] = m.profiles[id]
		}
	}
	return result, nil
}

func TestEnsureMFLIdentitiesBootstrapsNewlyObservedPlayer(t *testing.T) {
	repository := &mutableIdentities{profiles: map[player.ID]player.Profile{}, aliases: map[player.ExternalID]player.ID{}}
	snapshot := source.Snapshot{
		Roster:           []source.Player{{ID: "17750", Name: "New Rookie", RookieYear: 2026}},
		RookieCandidates: []source.RookieCandidate{{ID: "17750", Name: "New Rookie", RookieYear: 2026}},
	}
	warnings, err := ensureMFLIdentities(context.Background(), &snapshot, time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC), repository)
	if err != nil {
		t.Fatal(err)
	}
	externalID := player.ExternalID{Provider: player.ProviderMFL, Value: "17750"}
	canonicalID, found := repository.aliases[externalID]
	if !found || repository.profiles[canonicalID].DisplayName != "New Rookie" || repository.profiles[canonicalID].RookieYear != 2026 {
		t.Fatalf("bootstrapped identity = %+v / %+v", repository.aliases, repository.profiles)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "17750") {
		t.Fatalf("warnings = %v", warnings)
	}
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
	replacementID := player.ID("replacement-canonical")
	identities := fakeIdentities{
		byMFL: map[string]player.Profile{
			"17000": {ID: rookieID, DisplayName: "Rookie"},
			"15751": {ID: veteranID, DisplayName: "Veteran"},
			"16000": {ID: replacementID, DisplayName: "Replacement"},
		},
		byFP: map[string]player.Profile{
			"28000": {ID: rookieID, DisplayName: "Rookie"},
			"19788": {ID: veteranID, DisplayName: "Veteran"},
			"29000": {ID: replacementID, DisplayName: "Replacement"},
		},
	}
	snapshot := source.Snapshot{
		Roster: []source.Player{{ID: "15751", Name: "Veteran"}},
		ReplacementLevels: source.ReplacementLevels{CandidatesByPosition: map[string][]source.ReplacementCandidate{
			"WR": {{PlayerID: "16000", Name: "Replacement", Position: "WR"}},
		}},
		RookieCandidates: []source.RookieCandidate{{
			ID: "17000", Name: "Rookie", Position: "WR", RookieYear: 2026, RookieADP: 25.5, Source: "MFL rookie-only ADP",
		}},
	}
	warnings, err := enrichEvaluations(context.Background(), &snapshot, 2026, time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC), identities, fakeEvaluations{
		{FantasyProsID: "28000", RookieRank: 2, DynastyRank: 50, MarketValue: 4000, ProjectedPoints: 120},
		{FantasyProsID: "19788", DynastyRank: 5, MarketValue: 9000, ProjectedPoints: 250},
		{FantasyProsID: "29000", DynastyRank: 105, MarketValue: 900, ProjectedPoints: 110},
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
	if got := snapshot.Roster[0]; got.DynastyRank != 5 || got.MarketValue != 9000 || got.MarketSource == "" {
		t.Fatalf("veteran market evaluation = %+v", got)
	}
	if got := snapshot.ReplacementLevels.CandidatesByPosition["WR"][0]; got.DynastyRank != 105 || got.MarketValue != 900 || got.ProjectedPoints != 110 || !strings.Contains(got.Source, "FantasyPros") {
		t.Fatalf("replacement market evaluation = %+v", got)
	}
}
