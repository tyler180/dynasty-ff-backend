package mflsync

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/analysis/source"
	"github.com/tyler180/dynasty-ff-backend/internal/domain/league"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
)

type mapPlayerResolver map[string]player.Profile

func (r mapPlayerResolver) ResolvePlayer(_ context.Context, id player.ExternalID) (player.Profile, error) {
	profile, ok := r[id.Value]
	if !ok {
		return player.Profile{}, fmt.Errorf("not found")
	}
	return profile, nil
}

func TestNormalizeRecordsResolvesMFLIDsBeforeBuildingLeagueFacts(t *testing.T) {
	observedAt := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	snapshot := source.Snapshot{
		SnapshotDate: "2026-08-09",
		League: source.League{
			ID: "79286", Name: "League", SalaryCap: 250, ActiveRosterLimit: 26,
		},
		Franchise: source.Franchise{ID: "0005", Name: "Team"},
		Roster: []source.Player{{
			ID: "15751", Name: "Drake London", Position: "WR", NFLTeam: "ATL",
			Salary: 10, CurrentCapHit: 10, Status: "ROSTER", RookieYear: 2022,
		}},
		BirthdatesUnix: map[string]int64{"15751": time.Date(2001, 7, 24, 0, 0, 0, 0, time.UTC).Unix()},
		HistoricalPoints: source.HistoricalPoints{Source: "MFL", Seasons: []source.HistoricalSeason{{
			Season: 2025, ByPlayerID: map[string]float64{"15751": 170}, GamesPlayedByPlayerID: map[string]int{"15751": 17},
		}}},
		ReplacementLevels: source.ReplacementLevels{Source: "MFL free agents", PointsPerGameByPosition: map[string]float64{"WR": 8}},
		Projections:       source.Projections{Season: 2026, Source: "test", ByPlayerID: map[string]float64{"15751": 220}},
		Draft: source.Draft{
			Status: "scheduled", AvailabilityPollWindow: source.AvailabilityPollWindow{Start: "2026-08-20", End: "2026-08-21"},
			CurrentYearPicks: []source.Pick{{Pick: "1.06", Overall: 6, Salary: 15}},
		},
	}
	resolver := mapPlayerResolver{"15751": {ID: player.ID("player-123"), DisplayName: "Drake London"}}

	records, err := NormalizeRecords(context.Background(), snapshot, resolver, observedAt)
	if err != nil {
		t.Fatal(err)
	}
	if got := records.LeagueSnapshot.Roster[0].PlayerID; got != player.ID("player-123") {
		t.Fatalf("roster player ID = %q, want canonical player-123", got)
	}
	if records.LeagueSnapshot.Roster[0].Status != league.RosterActive {
		t.Fatalf("roster status = %q, want active", records.LeagueSnapshot.Roster[0].Status)
	}
	if assignment := records.LeagueSnapshot.Roster[0]; assignment.Position != "WR" || assignment.NFLTeam != "ATL" {
		t.Fatalf("roster facts = %+v", assignment)
	}
	if records.Players[0].BirthDate == nil || records.Players[0].RookieYear != 2022 {
		t.Fatalf("legacy profile enrichment was not preserved: %+v", records.Players[0])
	}
	if records.Aliases[0].ExternalID.Provider != player.ProviderMFL || records.Aliases[0].ExternalID.Value != "15751" {
		t.Fatalf("MFL alias = %+v", records.Aliases[0])
	}
	if asset := records.LeagueSnapshot.DraftAssets[0]; asset.Round != 1 || asset.Pick != 6 || asset.Overall != 6 {
		t.Fatalf("draft asset = %+v", asset)
	}
	canonicalID := "player-123"
	if got := records.LeagueSnapshot.HistoricalPoints.Seasons[0].ByPlayerID[canonicalID]; got != 170 {
		t.Fatalf("canonical historical points = %v", got)
	}
	if got := records.LeagueSnapshot.Projections.ByPlayerID[canonicalID]; got != 220 {
		t.Fatalf("canonical projection = %v", got)
	}
	if records.LeagueSnapshot.DraftStatus != "scheduled" || records.LeagueSnapshot.DraftAvailabilityWindow.Start != "2026-08-20" {
		t.Fatalf("draft state = %+v", records.LeagueSnapshot)
	}
}
