package freeagenttrends

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
	mflsync "github.com/tyler180/dynasty-ff-backend/internal/provider/mfl"
)

type fakeFreeAgents []mflsync.DefensiveFreeAgent

func (f fakeFreeAgents) DefensiveFreeAgents(context.Context, int, string) ([]mflsync.DefensiveFreeAgent, error) {
	return f, nil
}

type fakeIdentities map[player.ExternalID]player.Profile

func (f fakeIdentities) ResolvePlayers(_ context.Context, ids []player.ExternalID) (map[player.ExternalID]player.Profile, error) {
	result := make(map[player.ExternalID]player.Profile)
	for _, id := range ids {
		if profile, ok := f[id]; ok {
			result[id] = profile
		}
	}
	return result, nil
}

type fakeSnaps struct {
	facts   []history.PlayerGameSnaps
	queries []history.SnapQuery
}

func (f *fakeSnaps) PlayerGameSnaps(_ context.Context, query history.SnapQuery) ([]history.PlayerGameSnaps, error) {
	f.queries = append(f.queries, query)
	allowed := make(map[player.ID]struct{}, len(query.PlayerIDs))
	for _, id := range query.PlayerIDs {
		allowed[id] = struct{}{}
	}
	seasons := make(map[int]struct{}, len(query.Seasons))
	for _, season := range query.Seasons {
		seasons[season] = struct{}{}
	}
	var result []history.PlayerGameSnaps
	for _, fact := range f.facts {
		if _, ok := seasons[fact.Season]; len(seasons) > 0 && !ok {
			continue
		}
		if _, ok := allowed[fact.PlayerID]; ok {
			result = append(result, fact)
		}
	}
	return result, nil
}

func TestAnalyzeReturnsOnlyTopRisingResolvedFreeAgents(t *testing.T) {
	available := fakeFreeAgents{
		{MFLID: "100", Name: "Rising Linebacker", Position: "LB", PositionGroup: "LB"},
		{MFLID: "200", Name: "Stable Safety", Position: "S", PositionGroup: "DB"},
		{MFLID: "300", Name: "Missing Alias", Position: "DE", PositionGroup: "DL"},
	}
	identities := fakeIdentities{
		{Provider: player.ProviderMFL, Value: "100"}: {ID: "player-rising", DisplayName: "Rising Linebacker"},
		{Provider: player.ProviderMFL, Value: "200"}: {ID: "player-stable", DisplayName: "Stable Safety"},
	}
	snaps := &fakeSnaps{}
	for week, count := range []int{10, 12, 14, 50, 52, 54} {
		snaps.facts = append(snaps.facts, snapFact("player-rising", week+1, count))
	}
	for week, count := range []int{40, 41, 39, 40, 42, 38} {
		snaps.facts = append(snaps.facts, snapFact("player-stable", week+1, count))
	}

	result, err := (Service{FreeAgents: available, Identities: identities, Snaps: snaps}).Analyze(context.Background(), Request{
		Year: 2026, LeagueID: "79286", RecencyWeeks: recencyOff(), Seasons: []int{2025}, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trends) != 1 || result.Trends[0].PlayerID != "player-rising" || result.Trends[0].Signal != "rising" {
		t.Fatalf("trends = %+v", result.Trends)
	}
	if result.AvailableDefensivePlayers != 3 || result.ResolvedPlayers != 2 || result.PlayersWithSnapData != 2 {
		t.Fatalf("result counts = %+v", result)
	}
	if !reflect.DeepEqual(result.UnresolvedMFLPlayerIDs, []string{"300"}) {
		t.Fatalf("unresolved MFL IDs = %v", result.UnresolvedMFLPlayerIDs)
	}
	if len(snaps.queries) != 1 || !reflect.DeepEqual(snaps.queries[0].Seasons, []int{2025}) {
		t.Fatalf("snap queries = %+v", snaps.queries)
	}
}

func TestAnalyzeDefaultsToThreeSeasonsEndingWithLeagueSeason(t *testing.T) {
	available := fakeFreeAgents{{MFLID: "100", Name: "Player", Position: "LB", PositionGroup: "LB"}}
	identities := fakeIdentities{
		{Provider: player.ProviderMFL, Value: "100"}: {ID: "player-1", DisplayName: "Player"},
	}
	snaps := &fakeSnaps{}
	_, err := (Service{FreeAgents: available, Identities: identities, Snaps: snaps}).Analyze(context.Background(), Request{
		Year: 2026, LeagueID: "79286", RecencyWeeks: recencyOff(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps.queries) != 1 || !reflect.DeepEqual(snaps.queries[0].Seasons, []int{2024, 2025, 2026}) {
		t.Fatalf("snap queries = %+v", snaps.queries)
	}
}

func snapFact(id player.ID, week, defenseSnaps int) history.PlayerGameSnaps {
	return history.PlayerGameSnaps{
		PlayerID: id, GameID: "2025_" + string(rune('A'+week)), Season: 2025, Week: week,
		GameType: "REG", PositionGroup: "LB", DefenseSnaps: defenseSnaps, TeamDefenseSnaps: 60,
		DefenseSnapPct: float64(defenseSnaps) / 60,
	}
}

func TestAnalyzePreservesPFRPercentageInWeeklyTrend(t *testing.T) {
	available := fakeFreeAgents{{MFLID: "100", Name: "Rising Linebacker", Position: "LB", PositionGroup: "LB"}}
	identities := fakeIdentities{
		{Provider: player.ProviderMFL, Value: "100"}: {ID: "player-rising", DisplayName: "Rising Linebacker"},
	}
	snaps := &fakeSnaps{}
	for week, count := range []int{12, 12, 12, 30, 30, 30} {
		fact := snapFact("player-rising", week+1, count)
		if week >= 3 {
			fact.DefenseSnapPct = 0.51
		}
		snaps.facts = append(snaps.facts, fact)
	}

	result, err := (Service{FreeAgents: available, Identities: identities, Snaps: snaps}).Analyze(context.Background(), Request{
		Year: 2026, LeagueID: "79286", RecencyWeeks: recencyOff(), Seasons: []int{2025}, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trends) != 1 || result.Trends[0].Weekly[3].DefenseSnapShare != 0.51 {
		t.Fatalf("trends = %+v", result.Trends)
	}
	if result.Trends[0].Recent.DefenseSnapShare != 0.50 {
		t.Fatalf("weighted recent window = %+v", result.Trends[0].Recent)
	}
}

// Early in a season the comparison windows span the season boundary: the
// baseline comes from the end of last season and the recent games from this one.
func TestAnalyzeDefaultSeasonsSpanIntoCurrentSeason(t *testing.T) {
	available := fakeFreeAgents{{MFLID: "100", Name: "Rising Linebacker", Position: "LB", PositionGroup: "LB"}}
	identities := fakeIdentities{
		{Provider: player.ProviderMFL, Value: "100"}: {ID: "player-rising", DisplayName: "Rising Linebacker"},
	}
	snaps := &fakeSnaps{}
	for _, week := range []int{16, 17, 18} {
		snaps.facts = append(snaps.facts, snapFact("player-rising", week, 12))
	}
	for _, week := range []int{1, 2, 3} {
		fact := snapFact("player-rising", week, 45)
		fact.Season, fact.GameID = 2026, "2026_"+string(rune('A'+week))
		snaps.facts = append(snaps.facts, fact)
	}

	result, err := (Service{FreeAgents: available, Identities: identities, Snaps: snaps}).Analyze(context.Background(), Request{
		Year: 2026, LeagueID: "79286", RecencyWeeks: recencyOff(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trends) != 1 {
		t.Fatalf("trends = %+v", result.Trends)
	}
	trend := result.Trends[0]
	if trend.LatestSeason != 2026 || trend.LatestWeek != 3 || trend.Baseline.Games != 3 || trend.Recent.Games != 3 {
		t.Fatalf("trend windows = latest %d week %d, baseline %+v, recent %+v", trend.LatestSeason, trend.LatestWeek, trend.Baseline, trend.Recent)
	}
}

func TestAnalyzeMaxAgeUsesProfileBirthdates(t *testing.T) {
	born := func(value string) *time.Time {
		date, err := time.Parse("2006-01-02", value)
		if err != nil {
			t.Fatal(err)
		}
		return &date
	}
	available := fakeFreeAgents{
		{MFLID: "100", Name: "Young", Position: "LB", PositionGroup: "LB"},
		{MFLID: "200", Name: "Veteran", Position: "LB", PositionGroup: "LB"},
		{MFLID: "300", Name: "Unknown Age", Position: "LB", PositionGroup: "LB"},
	}
	identities := fakeIdentities{
		{Provider: player.ProviderMFL, Value: "100"}: {ID: "young", DisplayName: "Young", BirthDate: born("2000-03-01")},
		{Provider: player.ProviderMFL, Value: "200"}: {ID: "veteran", DisplayName: "Veteran", BirthDate: born("1996-03-01")},
		{Provider: player.ProviderMFL, Value: "300"}: {ID: "unknown", DisplayName: "Unknown Age"},
	}
	snaps := &fakeSnaps{}
	for _, id := range []player.ID{"young", "veteran", "unknown"} {
		for week, count := range []int{10, 12, 14, 50, 52, 54} {
			fact := snapFact(id, week+1, count)
			fact.GameID = string(id) + fact.GameID
			snaps.facts = append(snaps.facts, fact)
		}
	}
	service := Service{
		FreeAgents: available, Identities: identities, Snaps: snaps,
		Now: func() time.Time { return time.Date(2026, time.November, 21, 12, 0, 0, 0, time.UTC) },
	}

	tests := []struct {
		name         string
		maxAge       int
		wantAges     map[string]int // -1 means unknown
		wantExcluded int
	}{
		{"no filter reports every age", 0, map[string]int{"young": 26, "veteran": 30, "unknown": -1}, 0},
		{"max age 28 drops the veteran and keeps unknown", 28, map[string]int{"young": 26, "unknown": -1}, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := service.Analyze(context.Background(), Request{Year: 2026, LeagueID: "79286", RecencyWeeks: recencyOff(), Seasons: []int{2025}, MaxAge: test.maxAge})
			if err != nil {
				t.Fatal(err)
			}
			if result.ExcludedByAge != test.wantExcluded || len(result.Trends) != len(test.wantAges) {
				t.Fatalf("excluded = %d, trends = %+v", result.ExcludedByAge, result.Trends)
			}
			for _, trend := range result.Trends {
				want, ok := test.wantAges[trend.PlayerID]
				switch {
				case !ok:
					t.Fatalf("unexpected player %s", trend.PlayerID)
				case want == -1 && trend.Age != nil:
					t.Fatalf("%s age = %d, want unknown", trend.PlayerID, *trend.Age)
				case want >= 0 && (trend.Age == nil || *trend.Age != want):
					t.Fatalf("%s age = %v, want %d", trend.PlayerID, trend.Age, want)
				}
			}
		})
	}

	if _, err := service.Analyze(context.Background(), Request{Year: 2026, LeagueID: "79286", RecencyWeeks: recencyOff(), MaxAge: 61}); err == nil {
		t.Fatal("expected max_age above 60 to be rejected")
	}
}

// recencyOff disables the recency filter for tests about other behavior.
func recencyOff() *int {
	off := 0
	return &off
}
