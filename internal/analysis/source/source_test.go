package source

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAnalyzeTeamMcLeanSnapshot(t *testing.T) {
	path := teamMcLeanFixture(t)
	snapshot, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	analysis := Analyze(snapshot)

	if analysis.Cap.Used != 240 || analysis.Cap.Space != 10 {
		t.Fatalf("cap = %+v, want $240 used and $10 open", analysis.Cap)
	}
	if analysis.Roster.Active.Open != 2 || analysis.Roster.Taxi.Open != 0 {
		t.Fatalf("roster = %+v, want two active and zero taxi slots open", analysis.Roster)
	}
	if got := playerNames(analysis.TaxiCompliance.MustLeaveTaxi); got != "Adonai Mitchell,Devaughn Vele" {
		t.Fatalf("must leave taxi = %q", got)
	}
	if analysis.Draft.PickCount != 11 || analysis.Draft.TotalSalaryIfActive != 51 {
		t.Fatalf("draft = %+v, want 11 picks and $51", analysis.Draft)
	}
	if first := analysis.Draft.Picks[0]; first.Pick != "1.06" || first.ActiveCapShortfall != 5 || first.FitsTaxiNow {
		t.Fatalf("first pick = %+v", first)
	}

	promoteBoth := analysis.ComplianceScenarios[0]
	if len(promoteBoth.Promote) != 2 || promoteBoth.AdditionalCapRelief != 6 || promoteBoth.RosterLegal {
		t.Fatalf("promote-both scenario = %+v", promoteBoth)
	}
	legalMixed := analysis.ComplianceScenarios[1]
	if len(legalMixed.Promote) != 1 || legalMixed.Promote[0].Name != "Devaughn Vele" || !legalMixed.RosterLegal || legalMixed.ResultingCapHit != 245 {
		t.Fatalf("legal mixed scenario = %+v", legalMixed)
	}
	if got := analysis.HistoricalEfficiency.MostEfficient[0].Name; got != "Geno Smith" {
		t.Fatalf("most efficient = %q, want Geno Smith", got)
	}
}

func TestDropEvaluationUsesSalaryProjectionAndAge(t *testing.T) {
	snapshot := Snapshot{
		SnapshotDate: "2026-08-08",
		League:       League{Name: "Test League", SalaryCap: 100, ActiveRosterLimit: 5, InjuredReserveLimit: 1, TaxiSquadLimit: 1},
		Franchise:    Franchise{Name: "Test Team", TotalCapHit: 25},
		Roster: []Player{
			{ID: "old", Name: "Older RB", Position: "RB", Salary: 10, Status: "ROSTER"},
			{ID: "young", Name: "Younger RB", Position: "RB", Salary: 10, Status: "ROSTER"},
			{ID: "cheap", Name: "Cheap Player", Position: "WR", Salary: 5, Status: "ROSTER"},
		},
		BirthdatesUnix: map[string]int64{
			"old":   time.Date(1996, 1, 1, 0, 0, 0, 0, time.UTC).Unix(),
			"young": time.Date(2003, 1, 1, 0, 0, 0, 0, time.UTC).Unix(),
			"cheap": time.Date(1998, 1, 1, 0, 0, 0, 0, time.UTC).Unix(),
		},
		Projections: Projections{Season: 2026, Source: "test", ByPlayerID: map[string]float64{
			"old": 100, "young": 100, "cheap": 20,
		}},
		Draft: Draft{CurrentYearPicks: []Pick{{Pick: "1.01", Salary: 10}}},
	}

	analysis := AnalyzeWithOptions(snapshot, AnalysisOptions{CapReliefTarget: 8})
	drops := analysis.DropEvaluation
	if !drops.Available || len(drops.Candidates) != 3 {
		t.Fatalf("drop evaluation = %+v", drops)
	}
	if drops.BestForTarget == nil || drops.BestForTarget.PlayerID != "old" {
		t.Fatalf("best target cut = %+v, want old", drops.BestForTarget)
	}
	if drops.Candidates[0].PlayerID != "cheap" {
		t.Fatalf("top unrestricted drop = %q, want cheap", drops.Candidates[0].PlayerID)
	}
	if drops.Candidates[1].PlayerID != "old" || drops.Candidates[2].PlayerID != "young" {
		t.Fatalf("age did not rank equal-production RBs correctly: %+v", drops.Candidates)
	}
}

func TestHistoricalProjectionFallbackIsExplicit(t *testing.T) {
	snapshot, err := Read(teamMcLeanFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	strict := AnalyzeWithOptions(snapshot, AnalysisOptions{CapReliefTarget: 6})
	if strict.DropEvaluation.Available {
		t.Fatal("drop evaluation should be unavailable without explicit projections")
	}
	fallback := AnalyzeWithOptions(snapshot, AnalysisOptions{CapReliefTarget: 6, ProjectionFallback: "historical"})
	if !fallback.DropEvaluation.Available || fallback.DropEvaluation.BestForTarget == nil {
		t.Fatalf("historical fallback did not produce candidates: %+v", fallback.DropEvaluation)
	}
	if !strings.Contains(fallback.DropEvaluation.ProjectionSource, "historical") || !strings.Contains(fallback.DropEvaluation.Caution, "exploratory") {
		t.Fatalf("fallback is not clearly labeled: %+v", fallback.DropEvaluation)
	}
	if fallback.DropEvaluation.BestForTarget.PlayerID != "16287" {
		t.Fatalf("best replacement-aware cut = %s, want Tre Tucker", fallback.DropEvaluation.BestForTarget.Name)
	}
	for _, protected := range []string{"Luther Burden", "Malik Nabers", "Travis Hunter"} {
		if !dropCandidateNamed(fallback.DropEvaluation.HoldDevelop, protected) {
			t.Errorf("%s was not protected as hold/develop", protected)
		}
		if dropCandidateNamed(fallback.DropEvaluation.DropCandidates, protected) {
			t.Errorf("%s incorrectly appeared as a drop candidate", protected)
		}
	}
	for _, tradeFirst := range []string{"Joe Burrow", "De'Von Achane", "George Kittle"} {
		if !dropCandidateNamed(fallback.DropEvaluation.TradeFirst, tradeFirst) {
			t.Errorf("%s was not classified trade-first", tradeFirst)
		}
	}
}

func TestWeightedHistoricalUsesFourSeasonsAndIgnoresMissing(t *testing.T) {
	history := HistoricalPoints{Seasons: []HistoricalSeason{
		{Season: 2021, ByPlayerID: map[string]float64{"veteran": 1000}, GamesPlayedByPlayerID: map[string]int{"veteran": 1}},
		{Season: 2023, ByPlayerID: map[string]float64{"veteran": 60}, GamesPlayedByPlayerID: map[string]int{"veteran": 12}},
		{Season: 2025, ByPlayerID: map[string]float64{"veteran": 100, "rookie": 50, "zero": 100}, GamesPlayedByPlayerID: map[string]int{"veteran": 5, "rookie": 2, "zero": 10}},
		{Season: 2022, ByPlayerID: map[string]float64{"veteran": 40}, GamesPlayedByPlayerID: map[string]int{"veteran": 8}},
		{Season: 2024, ByPlayerID: map[string]float64{"veteran": 80, "zero": 0}, GamesPlayedByPlayerID: map[string]int{"veteran": 10, "zero": 10}},
	}}

	values, seasons := weightedHistorical(history)
	if got := values["veteran"].PointsPerGame; got != 9.76 {
		t.Fatalf("veteran weighted PPG = %.2f, want 9.76", got)
	}
	if got := values["rookie"].PointsPerGame; got != 25 {
		t.Fatalf("rookie weighted PPG = %.2f, want 25", got)
	}
	if got := values["zero"].PointsPerGame; got != 5.71 {
		t.Fatalf("recorded zero-point games were not retained: %.2f", got)
	}
	if !reflect.DeepEqual(seasons, []int{2025, 2024, 2023, 2022}) {
		t.Fatalf("seasons = %v", seasons)
	}
}

func TestFormatTextHighlightsConstraints(t *testing.T) {
	snapshot, err := Read(teamMcLeanFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	report := FormatText(Analyze(snapshot))
	for _, expected := range []string{
		"Cap: $240.00 / $250.00",
		"Adonai Mitchell ($11), Devaughn Vele ($5)",
		"stash picks [1.06, 1.07] on taxi",
		"needs $6.00 additional cap relief",
		"No rookie or multi-year veteran projections",
	} {
		if !strings.Contains(report, expected) {
			t.Errorf("report does not contain %q", expected)
		}
	}
}

func playerNames(players []PlayerSummary) string {
	names := make([]string, 0, len(players))
	for _, player := range players {
		names = append(names, player.Name)
	}
	return strings.Join(names, ",")
}

func dropCandidateNamed(players []DropCandidate, name string) bool {
	for _, player := range players {
		if player.Name == name {
			return true
		}
	}
	return false
}

func teamMcLeanFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "data", "team-mclean-2026-source.json")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return path + ".bak"
}
