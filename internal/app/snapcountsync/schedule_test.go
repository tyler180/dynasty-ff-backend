package snapcountsync

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
	"github.com/tyler180/dynasty-ff-backend/internal/provider/nflverse"
)

type fakeSchedule struct {
	puts [][]history.ScheduledGame
	err  error
}

func (f *fakeSchedule) PutSeasonSchedule(_ context.Context, _ int, games []history.ScheduledGame) error {
	if f.err != nil {
		return f.err
	}
	f.puts = append(f.puts, append([]history.ScheduledGame(nil), games...))
	return nil
}

func TestDeriveScheduleListsEveryGameWithBothTeams(t *testing.T) {
	records := []nflverse.SnapCount{
		{GameID: "2025_02_KC_LV", Season: 2025, Week: 2, GameType: "REG", Team: "KC", Opponent: "LV"},
		{GameID: "2025_01_DAL_PHI", Season: 2025, Week: 1, GameType: "REG", Team: "PHI", Opponent: "DAL"},
		// Same game from the other sideline, and a row with no player ID.
		{GameID: "2025_01_DAL_PHI", Season: 2025, Week: 1, GameType: "REG", Team: "DAL", Opponent: "PHI"},
		{GameID: "2025_19_BUF_DEN", Season: 2025, Week: 19, GameType: "WC", Team: "DEN"},
		// Rows that cannot place a game are skipped.
		{GameID: "", Season: 2025, Week: 3, Team: "NYJ"},
		{GameID: "2024_18_NE_NYJ", Season: 2024, Week: 18, Team: "NE"},
		{GameID: "2025_04_NO_TB", Season: 2025, Week: 4},
	}
	got := deriveSchedule(2025, records)
	want := []history.ScheduledGame{
		{Season: 2025, Week: 1, GameID: "2025_01_DAL_PHI", GameType: "REG", Teams: []string{"DAL", "PHI"}},
		{Season: 2025, Week: 2, GameID: "2025_02_KC_LV", GameType: "REG", Teams: []string{"KC", "LV"}},
		{Season: 2025, Week: 19, GameID: "2025_19_BUF_DEN", GameType: "WC", Teams: []string{"DEN"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schedule = %+v\nwant %+v", got, want)
	}
}

func TestSyncWritesScheduleEvenWhenSnapsUnchanged(t *testing.T) {
	schedule := &fakeSchedule{}
	pfrID := player.ExternalID{Provider: player.ProviderPFR, Value: "DeanNa00"}
	service := Service{
		Source: fakeSource{
			{GameID: "game-1", Season: 2025, Week: 1, GameType: "REG", PFRPlayerID: "DeanNa00", Position: "LB", Team: "PHI", Opponent: "DAL", DefenseSnaps: 52, DefenseSnapPct: 0.83},
		},
		Identities: fakeIdentities{pfrID: {ID: "player-dean", DisplayName: "Nakobe Dean"}},
		Snaps:      &fakeSnaps{}, State: &fakeState{}, Schedule: schedule,
		Now: func() time.Time { return time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC) },
	}
	first, err := service.Sync(context.Background(), Request{Season: 2025})
	if err != nil || first.Unchanged || first.ScheduledGames != 1 {
		t.Fatalf("first sync = %+v, err = %v", first, err)
	}
	second, err := service.Sync(context.Background(), Request{Season: 2025})
	if err != nil || !second.Unchanged || second.ScheduledGames != 1 {
		t.Fatalf("second sync = %+v, err = %v", second, err)
	}
	if len(schedule.puts) != 2 {
		t.Fatalf("schedule writes = %d, want one per sync so old seasons backfill", len(schedule.puts))
	}
}

func TestSyncFailsWhenScheduleWriteFails(t *testing.T) {
	pfrID := player.ExternalID{Provider: player.ProviderPFR, Value: "DeanNa00"}
	service := Service{
		Source: fakeSource{
			{GameID: "game-1", Season: 2025, Week: 1, PFRPlayerID: "DeanNa00", Position: "LB", Team: "PHI", DefenseSnaps: 52, DefenseSnapPct: 0.83},
		},
		Identities: fakeIdentities{pfrID: {ID: "player-dean", DisplayName: "Nakobe Dean"}},
		Snaps:      &fakeSnaps{}, Schedule: &fakeSchedule{err: errors.New("throttled")},
	}
	_, err := service.Sync(context.Background(), Request{Season: 2025})
	if err == nil || !strings.Contains(err.Error(), "store 2025 schedule: throttled") {
		t.Fatalf("err = %v", err)
	}
}

type failingSnaps struct{}

func (failingSnaps) PutPlayerGameSnaps(context.Context, []history.PlayerGameSnaps) error {
	return errors.New("snap write failed")
}

func TestSyncSkipsScheduleWhenSnapWriteFails(t *testing.T) {
	schedule := &fakeSchedule{}
	pfrID := player.ExternalID{Provider: player.ProviderPFR, Value: "DeanNa00"}
	service := Service{
		Source: fakeSource{
			{GameID: "game-1", Season: 2025, Week: 1, PFRPlayerID: "DeanNa00", Position: "LB", Team: "PHI", DefenseSnaps: 52, DefenseSnapPct: 0.83},
		},
		Identities: fakeIdentities{pfrID: {ID: "player-dean", DisplayName: "Nakobe Dean"}},
		Snaps:      failingSnaps{}, Schedule: schedule,
	}
	if _, err := service.Sync(context.Background(), Request{Season: 2025}); err == nil {
		t.Fatal("expected the snap write failure")
	}
	if len(schedule.puts) != 0 {
		t.Fatalf("schedule written for a week whose snaps failed to store: %+v", schedule.puts)
	}
}
