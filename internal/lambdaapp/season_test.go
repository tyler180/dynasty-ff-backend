package lambdaapp

import (
	"context"
	"testing"
	"time"
)

func TestCurrentNFLSeason(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want int
	}{
		{"new year playoffs belong to prior season", time.Date(2027, time.January, 15, 12, 0, 0, 0, time.UTC), 2026},
		{"offseason belongs to prior season", time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC), 2025},
		{"last offseason instant", time.Date(2026, time.August, 31, 23, 59, 59, 0, time.UTC), 2025},
		{"first season instant", time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC), 2026},
		{"mid season", time.Date(2026, time.November, 20, 0, 0, 0, 0, time.UTC), 2026},
		{"non-UTC input is evaluated in UTC", time.Date(2026, time.August, 31, 20, 0, 0, 0, time.FixedZone("PDT", -7*3600)), 2026},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := currentNFLSeason(test.now); got != test.want {
				t.Fatalf("currentNFLSeason(%s) = %d, want %d", test.now, got, test.want)
			}
		})
	}
}

func TestHandlerSyncsDeriveSeasonWhenOmitted(t *testing.T) {
	tests := []struct {
		name      string
		requested int
		want      int
	}{
		{"omitted season uses current NFL season", 0, 2026},
		{"explicit season is kept", 2024, 2024},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, err := New(&fakeSnapshots{}, &fakeIdentities{})
			if err != nil {
				t.Fatal(err)
			}
			handler.now = func() time.Time { return time.Date(2026, time.November, 20, 0, 0, 0, 0, time.UTC) }
			snaps, stats, charts := &fakeSnapCounts{}, &fakePlayerStats{}, &fakeDepthCharts{}
			handler.WithSnapCounts(snaps, snaps).WithPlayerStats(stats, stats).WithDepthCharts(charts, charts)

			for _, action := range []string{ActionSyncSnapCounts, ActionSyncPlayerStats, ActionSyncDepthCharts} {
				if _, err := handler.Handle(context.Background(), Request{Action: action, Season: test.requested}); err != nil {
					t.Fatalf("%s: %v", action, err)
				}
			}
			if snaps.syncSeason != test.want || stats.syncSeason != test.want || charts.syncSeason != test.want {
				t.Fatalf("synced seasons = snaps %d, stats %d, charts %d; want %d", snaps.syncSeason, stats.syncSeason, charts.syncSeason, test.want)
			}
		})
	}
}

// Scheduled EventBridge targets send "season": null when no year is pinned.
func TestEntrypointDerivesSeasonForNullScheduledSeason(t *testing.T) {
	handler, err := New(&fakeSnapshots{}, &fakeIdentities{})
	if err != nil {
		t.Fatal(err)
	}
	handler.now = func() time.Time { return time.Date(2026, time.November, 20, 0, 0, 0, 0, time.UTC) }
	snaps := &fakeSnapCounts{}
	handler.WithSnapCounts(snaps, snaps)
	entrypoint, err := NewEntrypoint(handler)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entrypoint.Handle(context.Background(), []byte(`{"action":"sync_snap_counts","season":null}`)); err != nil {
		t.Fatal(err)
	}
	if snaps.syncSeason != 2026 {
		t.Fatalf("synced season = %d, want 2026", snaps.syncSeason)
	}
}
