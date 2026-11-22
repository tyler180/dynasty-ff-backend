package freeagenttrends

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
)

type fakeSchedule struct {
	games   []history.ScheduledGame
	err     error
	queries [][]int
}

func (f *fakeSchedule) SeasonSchedules(_ context.Context, seasons []int) ([]history.ScheduledGame, error) {
	f.queries = append(f.queries, seasons)
	if f.err != nil {
		return nil, f.err
	}
	wanted := make(map[int]bool, len(seasons))
	for _, season := range seasons {
		wanted[season] = true
	}
	var result []history.ScheduledGame
	for _, game := range f.games {
		if wanted[game.Season] {
			result = append(result, game)
		}
	}
	return result, nil
}

// weeksPlayed returns one game per week; postseason weeks get game type POST.
func weeksPlayed(season, firstWeek, lastWeek int, gameType string) []history.ScheduledGame {
	var games []history.ScheduledGame
	for week := firstWeek; week <= lastWeek; week++ {
		games = append(games, history.ScheduledGame{
			Season: season, Week: week, GameID: "g", GameType: gameType, Teams: []string{"BUF", "MIA"},
		})
	}
	return games
}

func game(id player.ID, season, week int, gameType string) history.PlayerGameSnaps {
	return history.PlayerGameSnaps{PlayerID: id, Season: season, Week: week, GameType: gameType}
}

func TestStaleByRecency(t *testing.T) {
	window := RecencyWindow{Weeks: 6, FromSeason: 2026, FromWeek: 6}
	tests := []struct {
		name  string
		games []history.PlayerGameSnaps
		stale bool
	}{
		{"last three games inside the window", []history.PlayerGameSnaps{
			game("p", 2026, 1, "REG"), game("p", 2026, 9, "REG"), game("p", 2026, 10, "REG"), game("p", 2026, 11, "REG"),
		}, false},
		{"oldest recent game exactly at the window start", []history.PlayerGameSnaps{
			game("p", 2026, 6, "REG"), game("p", 2026, 7, "REG"), game("p", 2026, 8, "REG"),
		}, false},
		{"one of the last three games before the window", []history.PlayerGameSnaps{
			game("p", 2026, 4, "REG"), game("p", 2026, 8, "REG"), game("p", 2026, 11, "REG"),
		}, true},
		{"last games were last season", []history.PlayerGameSnaps{
			game("p", 2025, 16, "REG"), game("p", 2025, 17, "REG"), game("p", 2025, 18, "REG"),
		}, true},
		{"postseason games do not count as recent", []history.PlayerGameSnaps{
			game("p", 2025, 10, "REG"), game("p", 2025, 11, "REG"), game("p", 2025, 12, "REG"),
			game("p", 2026, 9, "WC"), game("p", 2026, 10, "DIV"), game("p", 2026, 11, "SB"),
		}, true},
		{"fewer than three games, all recent", []history.PlayerGameSnaps{
			game("p", 2026, 10, "REG"), game("p", 2026, 11, ""),
		}, false},
		{"only postseason games is left to the model", []history.PlayerGameSnaps{
			game("p", 2025, 19, "WC"),
		}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, stale := staleByRecency(test.games, window)["p"]
			if stale != test.stale {
				t.Fatalf("stale = %v, want %v", stale, test.stale)
			}
		})
	}
}

func TestRecencyWindow(t *testing.T) {
	season2025 := append(weeksPlayed(2025, 1, 18, "REG"), weeksPlayed(2025, 19, 22, "POST")...)
	tests := []struct {
		name  string
		games []history.ScheduledGame
		weeks int
		want  RecencyWindow
	}{
		{"mid-season stays in the league season", append(season2025, weeksPlayed(2026, 1, 11, "REG")...), 6,
			RecencyWindow{Weeks: 6, FromSeason: 2026, FromWeek: 6}},
		{"early season reaches into last season", append(season2025, weeksPlayed(2026, 1, 2, "REG")...), 6,
			RecencyWindow{Weeks: 6, FromSeason: 2025, FromWeek: 15}},
		{"offseason skips postseason weeks", season2025, 6,
			RecencyWindow{Weeks: 6, FromSeason: 2025, FromWeek: 13}},
		{"fewer weeks stored than requested uses the oldest", weeksPlayed(2026, 1, 3, "REG"), 6,
			RecencyWindow{Weeks: 6, FromSeason: 2026, FromWeek: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schedule := &fakeSchedule{games: test.games}
			got, err := (Service{Schedule: schedule}).recencyWindow(context.Background(), 2026, test.weeks)
			if err != nil {
				t.Fatal(err)
			}
			if *got != test.want {
				t.Fatalf("window = %+v, want %+v", *got, test.want)
			}
			if !reflect.DeepEqual(schedule.queries, [][]int{{2025, 2026}}) {
				t.Fatalf("schedule queries = %v", schedule.queries)
			}
		})
	}
}

func TestAnalyzeExcludesFreeAgentsWithoutRecentGames(t *testing.T) {
	available := fakeFreeAgents{
		{MFLID: "100", Name: "Current Linebacker", Position: "LB", PositionGroup: "LB"},
		{MFLID: "200", Name: "Released Linebacker", Position: "LB", PositionGroup: "LB"},
	}
	identities := fakeIdentities{
		{Provider: player.ProviderMFL, Value: "100"}: {ID: "player-current", DisplayName: "Current Linebacker"},
		{Provider: player.ProviderMFL, Value: "200"}: {ID: "player-released", DisplayName: "Released Linebacker"},
	}
	snaps := &fakeSnaps{}
	// Both rise the same way; one stopped playing after 2026 week 6.
	for index, count := range []int{10, 12, 14, 50, 52, 54} {
		current := snapFact("player-current", index+6, count)
		current.Season, current.GameID = 2026, "2026_c"+string(rune('A'+index))
		released := snapFact("player-released", index+1, count)
		released.Season, released.GameID = 2026, "2026_r"+string(rune('A'+index))
		snaps.facts = append(snaps.facts, current, released)
	}
	schedule := &fakeSchedule{games: weeksPlayed(2026, 1, 11, "REG")}

	result, err := (Service{FreeAgents: available, Identities: identities, Snaps: snaps, Schedule: schedule}).Analyze(
		context.Background(), Request{Year: 2026, LeagueID: "79286", Seasons: []int{2026}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExcludedByRecency != 1 || result.PlayersWithSnapData != 2 {
		t.Fatalf("excluded by recency = %d, players with snap data = %d", result.ExcludedByRecency, result.PlayersWithSnapData)
	}
	if result.RecencyWindow == nil || *result.RecencyWindow != (RecencyWindow{Weeks: 6, FromSeason: 2026, FromWeek: 6}) {
		t.Fatalf("recency window = %+v", result.RecencyWindow)
	}
	if len(result.Trends) != 1 || result.Trends[0].PlayerID != "player-current" {
		t.Fatalf("trends = %+v", result.Trends)
	}
}

func TestAnalyzeRecencyFailures(t *testing.T) {
	available := fakeFreeAgents{{MFLID: "100", Name: "Player", Position: "LB", PositionGroup: "LB"}}
	identities := fakeIdentities{{Provider: player.ProviderMFL, Value: "100"}: {ID: "player-1", DisplayName: "Player"}}
	weeks := func(value int) *int { return &value }
	tests := []struct {
		name     string
		schedule history.ScheduleReader
		weeks    *int
		wantErr  string
	}{
		{"no schedule source", nil, nil, "schedule source is required"},
		{"no regular-season games stored", &fakeSchedule{games: weeksPlayed(2025, 19, 22, "POST")}, nil, "run sync_snap_counts"},
		{"schedule read fails", &fakeSchedule{err: errors.New("throttled")}, nil, "load NFL schedule: throttled"},
		{"negative weeks", &fakeSchedule{}, weeks(-1), "recency_weeks must be between"},
		{"too many weeks", &fakeSchedule{}, weeks(19), "recency_weeks must be between"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := Service{FreeAgents: available, Identities: identities, Snaps: &fakeSnaps{}, Schedule: test.schedule}
			_, err := service.Analyze(context.Background(), Request{Year: 2026, LeagueID: "79286", RecencyWeeks: test.weeks})
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, test.wantErr)
			}
		})
	}
}

func TestAnalyzeRecencyOffSkipsSchedule(t *testing.T) {
	service := Service{
		FreeAgents: fakeFreeAgents{{MFLID: "100", Name: "Player", Position: "LB", PositionGroup: "LB"}},
		Identities: fakeIdentities{{Provider: player.ProviderMFL, Value: "100"}: {ID: "player-1", DisplayName: "Player"}},
		Snaps:      &fakeSnaps{},
	}
	result, err := service.Analyze(context.Background(), Request{Year: 2026, LeagueID: "79286", RecencyWeeks: recencyOff()})
	if err != nil {
		t.Fatal(err)
	}
	if result.RecencyWindow != nil || result.ExcludedByRecency != 0 {
		t.Fatalf("recency should be off: %+v", result)
	}
}
