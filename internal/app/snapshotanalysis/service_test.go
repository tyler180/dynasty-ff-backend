package snapshotanalysis

import (
	"context"
	"testing"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/domain/league"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
)

type fakeSnapshots struct{ snapshot league.Snapshot }

func (f fakeSnapshots) LatestSnapshot(context.Context, league.ID, league.FranchiseID, int) (league.Snapshot, error) {
	return f.snapshot, nil
}
func (f fakeSnapshots) SnapshotAt(context.Context, league.ID, league.FranchiseID, int, time.Time) (league.Snapshot, error) {
	return f.snapshot, nil
}

type fakePlayers map[player.ID]player.Profile

func (f fakePlayers) GetPlayer(_ context.Context, id player.ID) (player.Profile, error) {
	return f[id], nil
}
func (f fakePlayers) ResolvePlayer(context.Context, player.ExternalID) (player.Profile, error) {
	return player.Profile{}, nil
}

func TestAnalyzeLatestNormalizedSnapshot(t *testing.T) {
	birthDate := time.Date(1997, 1, 1, 0, 0, 0, 0, time.UTC)
	observedAt := time.Date(2026, 8, 13, 23, 11, 52, 0, time.UTC)
	playerID := player.ID("player-1")
	service := Service{
		Snapshots: fakeSnapshots{snapshot: league.Snapshot{
			League: league.League{
				ID: "79286", Name: "League", Season: 2026, SalaryCap: 250,
				ActiveRosterLimit: 26, InjuredReserveLimit: 3, TaxiSquadLimit: 4,
			},
			Franchise: league.Franchise{ID: "0005", Name: "Team McLean"},
			Roster: []league.RosterAssignment{{
				PlayerID: playerID, Status: league.RosterActive, Position: "WR", NFLTeam: "ATL",
				Salary: 20, CurrentCapHit: 20,
			}},
			DraftAssets: []league.DraftAsset{{Season: 2026, Round: 1, Pick: 6, Overall: 6, Salary: 15}},
			HistoricalPoints: league.HistoricalPoints{Source: "MFL", Seasons: []league.HistoricalSeason{{
				Season: 2025, ByPlayerID: map[string]float64{string(playerID): 170},
				GamesPlayedByPlayerID: map[string]int{string(playerID): 17},
			}}},
			ReplacementLevels: league.ReplacementLevels{
				Source: "MFL free agents", PointsPerGameByPosition: map[string]float64{"WR": 8},
			},
			ObservedAt: observedAt, Source: "mfl",
		}},
		Players: fakePlayers{playerID: {
			ID: playerID, DisplayName: "Drake London", BirthDate: &birthDate, RookieYear: 2022,
		}},
	}

	result, err := service.Analyze(context.Background(), Request{
		Year: 2026, LeagueID: "79286", FranchiseID: "0005", ProjectionFallback: "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProjectionFallback != "historical" || !result.SnapshotObservedAt.Equal(observedAt) {
		t.Fatalf("result metadata = %+v", result)
	}
	if result.Analysis.Team != "Team McLean" || result.Analysis.Cap.Used != 20 || result.Analysis.Draft.PickCount != 1 {
		t.Fatalf("analysis = %+v", result.Analysis)
	}
	if !result.Analysis.DropEvaluation.Available || len(result.Analysis.DropEvaluation.Candidates) != 1 {
		t.Fatalf("drop evaluation = %+v", result.Analysis.DropEvaluation)
	}
	if got := result.Analysis.DropEvaluation.Candidates[0].Name; got != "Drake London" {
		t.Fatalf("candidate name = %q", got)
	}
}
