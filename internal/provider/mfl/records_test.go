package mflsync

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tylermclean/dynasty-ff-backend/internal/analysis/source"
	"github.com/tylermclean/dynasty-ff-backend/internal/domain/league"
	"github.com/tylermclean/dynasty-ff-backend/internal/identity/player"
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
			ID: "15751", Name: "Drake London", Salary: 10, CurrentCapHit: 10, Status: "ROSTER", RookieYear: 2022,
		}},
		BirthdatesUnix: map[string]int64{"15751": time.Date(2001, 7, 24, 0, 0, 0, 0, time.UTC).Unix()},
		Draft:          source.Draft{CurrentYearPicks: []source.Pick{{Pick: "1.06", Overall: 6, Salary: 15}}},
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
	if records.Players[0].BirthDate == nil || records.Players[0].RookieYear != 2022 {
		t.Fatalf("legacy profile enrichment was not preserved: %+v", records.Players[0])
	}
	if records.Aliases[0].ExternalID.Provider != player.ProviderMFL || records.Aliases[0].ExternalID.Value != "15751" {
		t.Fatalf("MFL alias = %+v", records.Aliases[0])
	}
	if asset := records.LeagueSnapshot.DraftAssets[0]; asset.Round != 1 || asset.Pick != 6 || asset.Overall != 6 {
		t.Fatalf("draft asset = %+v", asset)
	}
}
