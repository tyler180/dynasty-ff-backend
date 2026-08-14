package mflsync

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	source "github.com/tyler180/dynasty-ff-models/analysis"
)

type fakeCaller struct {
	responses map[string]any
	errors    map[string]error
	calls     []string
}

func (f *fakeCaller) Call(_ context.Context, tool string, _ any, destination any) error {
	f.calls = append(f.calls, tool)
	if err := f.errors[tool]; err != nil {
		return err
	}
	response, ok := f.responses[tool]
	if !ok {
		return errors.New("unexpected tool call")
	}
	payload, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, destination)
}

func TestSyncRefreshesMFLFieldsAndPreservesLeagueSpecificInputs(t *testing.T) {
	base, err := LoadBase(teamMcLeanFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := fixtureCaller()
	when := time.Date(2026, time.August, 9, 15, 30, 0, 0, time.FixedZone("MDT", -6*60*60))
	result, err := Sync(context.Background(), caller, base, Options{
		Year: 2026, LeagueID: "79286", FranchiseID: "0005", SnapshotDate: when, IncludeDraft: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	snapshot := result.Snapshot
	if snapshot.SnapshotDate != "2026-08-09" || snapshot.League.Name != "Fresh League Name" || snapshot.Franchise.Name != "Team McLean Live" {
		t.Fatalf("identity fields were not refreshed: %+v %+v", snapshot.League, snapshot.Franchise)
	}
	if len(snapshot.Roster) != 3 {
		t.Fatalf("roster length = %d, want 3", len(snapshot.Roster))
	}
	if snapshot.Franchise.ActivePlayers != 1 || snapshot.Franchise.InjuredReservePlayers != 1 || snapshot.Franchise.TaxiSquadPlayers != 1 {
		t.Fatalf("roster counts = %+v", snapshot.Franchise)
	}
	if snapshot.Franchise.TotalCapHit != 16 || snapshot.Franchise.CurrentCapSpace != 234 {
		t.Fatalf("cap summary = %+v, want total 16 and space 234", snapshot.Franchise)
	}
	if snapshot.BirthdatesUnix["15751"] != 946684800 {
		t.Fatalf("birthdate = %d, want 946684800", snapshot.BirthdatesUnix["15751"])
	}
	if got := pickLabels(snapshot.Draft.CurrentYearPicks); !reflect.DeepEqual(got, []string{"1.06", "2.06"}) {
		t.Fatalf("picks = %v, want [1.06 2.06]", got)
	}
	if snapshot.Draft.CurrentYearPicks[0].Salary != 15 || snapshot.Draft.CurrentYearPicks[1].Salary != 7 {
		t.Fatalf("pick salaries were not preserved: %+v", snapshot.Draft.CurrentYearPicks)
	}
	if snapshot.Draft.Status != "in_progress" || snapshot.Draft.StatusMessage != "Team McLean is on the clock" {
		t.Fatalf("draft status = %+v", snapshot.Draft)
	}
	pool, ok := object(result.Extra["available_rookie_pool"])
	if !ok || intField(pool, "unrostered_supported_position_count") != 1 {
		t.Fatalf("available rookie pool = %#v", result.Extra["available_rookie_pool"])
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %v", result.Warnings)
	}

	payload, err := Render(base, result)
	if err != nil {
		t.Fatal(err)
	}
	var rendered map[string]any
	if err := json.Unmarshal(payload, &rendered); err != nil {
		t.Fatal(err)
	}
	league, _ := object(rendered["league"])
	if _, ok := league["starting_lineup_2026"]; !ok {
		t.Fatal("render removed base league starting-lineup rules")
	}
	draft, _ := object(rendered["draft"])
	if _, ok := draft["rookie_salary_schedule"]; !ok {
		t.Fatal("render removed the base rookie salary schedule")
	}
	if _, ok := rendered["model_relevant_rules"]; !ok {
		t.Fatal("render removed non-MFL league rules")
	}
	if _, ok := rendered["sync"]; !ok {
		t.Fatal("render omitted sync metadata")
	}
	outputPath := filepath.Join(t.TempDir(), "live.json")
	if err := WriteFile(outputPath, payload); err != nil {
		t.Fatal(err)
	}
	refreshed, err := source.Read(outputPath)
	if err != nil {
		t.Fatalf("source reader rejected synchronized output: %v", err)
	}
	analysis := source.Analyze(refreshed)
	if analysis.Roster.Active.Used != 1 || analysis.Roster.InjuredReserve.Used != 1 || analysis.Roster.Taxi.Used != 1 {
		t.Fatalf("source analysis did not consume synchronized roster: %+v", analysis.Roster)
	}
}

func TestSyncBootstrapsDirectlyFromMFL(t *testing.T) {
	when := time.Date(2026, time.August, 9, 15, 30, 0, 0, time.UTC)
	base := NewBase(2026, "79286", "0005", when)
	result, err := Sync(context.Background(), fixtureCaller(), base, Options{
		Year: 2026, LeagueID: "79286", FranchiseID: "0005", SnapshotDate: when, IncludeDraft: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Snapshot.League.Name != "Fresh League Name" || result.Snapshot.Franchise.Name != "Team McLean Live" {
		t.Fatalf("MFL identities were not populated: %+v %+v", result.Snapshot.League, result.Snapshot.Franchise)
	}
	if len(result.Snapshot.Roster) != 3 || len(result.Snapshot.Draft.CurrentYearPicks) != 2 {
		t.Fatalf("MFL bootstrap produced roster=%d picks=%d", len(result.Snapshot.Roster), len(result.Snapshot.Draft.CurrentYearPicks))
	}
	if len(result.Warnings) != 2 {
		t.Fatalf("bootstrap warnings = %v, want the two missing-policy warnings", result.Warnings)
	}
	analysis := source.Analyze(result.Snapshot)
	if !strings.Contains(strings.Join(analysis.Warnings, "\n"), "rookie salary schedule") {
		t.Fatalf("analysis did not surface sync warnings: %v", analysis.Warnings)
	}
}

func TestSyncKeepsOptionalBaseDataWhenMFLToolIsUnavailable(t *testing.T) {
	base, err := LoadBase(teamMcLeanFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := fixtureCaller()
	caller.errors = map[string]error{
		"get_assets":             errors.New("draft is not scheduled"),
		"get_future_draft_picks": errors.New("not available"),
		"get_draft_results":      errors.New("draft is not scheduled"),
		"get_player_profiles":    errors.New("profile endpoint unavailable"),
	}
	result, err := Sync(context.Background(), caller, base, Options{
		Year: 2026, LeagueID: "79286", FranchiseID: "0005", SnapshotDate: time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC), IncludeDraft: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Snapshot.Draft.CurrentYearPicks, base.Snapshot.Draft.CurrentYearPicks) {
		t.Fatal("optional assets failure replaced verified base picks")
	}
	if len(result.Warnings) != 4 {
		t.Fatalf("warnings = %v, want four", result.Warnings)
	}
	if !strings.Contains(strings.Join(result.Snapshot.SourceReconciliation, "\n"), "Sync warning") {
		t.Fatal("optional failures were not recorded in reconciliation notes")
	}
}

func TestSyncLiveDraftExcludesJustSelectedRookie(t *testing.T) {
	base, err := LoadBase(teamMcLeanFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	caller := fixtureCaller()
	result, err := Sync(context.Background(), caller, base, Options{
		Year: 2026, LeagueID: "79286", FranchiseID: "0005", SnapshotDate: time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC), IncludeDraft: true, LiveDraft: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := object(result.Extra["available_rookie_pool"])
	if intField(pool, "unrostered_supported_position_count") != 0 {
		t.Fatalf("live drafted player remained available: %#v", pool)
	}
	if result.Snapshot.Draft.Status != "in_progress" || !strings.Contains(result.Snapshot.Draft.StatusMessage, "1.07") {
		t.Fatalf("live draft state = %+v", result.Snapshot.Draft)
	}
	if got := pickLabels(result.Snapshot.Draft.CurrentYearPicks); !reflect.DeepEqual(got, []string{"2.06"}) {
		t.Fatalf("completed live pick was not removed: %v", got)
	}
}

func TestLoadProjectionsSupportsBareMap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projections.json")
	if err := WriteFile(path, []byte(`{"15751":123.5,"15418":45}`)); err != nil {
		t.Fatal(err)
	}
	projections, err := LoadProjections(path, 2026)
	if err != nil {
		t.Fatal(err)
	}
	if projections.Season != 2026 || projections.Source != "projections.json" || projections.ByPlayerID["15751"] != 123.5 {
		t.Fatalf("projections = %+v", projections)
	}
}

func fixtureCaller() *fakeCaller {
	return &fakeCaller{responses: map[string]any{
		"get_rules":     map[string]any{"rules": map[string]any{"positionRules": map[string]any{}}},
		"get_all_rules": map[string]any{"allRules": map[string]any{"rule": []any{}}},
		"get_player_scores": map[string]any{"playerScores": map[string]any{"playerScore": []any{
			map[string]any{"id": "15751", "score": "160"},
			map[string]any{"id": "15418", "score": "120"},
			map[string]any{"id": "17000", "score": "80"},
		}}},
		"get_league": map[string]any{"league": map[string]any{
			"id": "79286", "name": "Fresh League Name", "salaryCapAmount": "250", "rosterSize": "26", "injuredReserve": "3", "taxiSquad": "4",
			"franchises": map[string]any{"count": "12", "franchise": []any{
				map[string]any{"id": "0005", "name": "Team McLean Live"},
				map[string]any{"id": "0002", "name": "Other Team"},
			}},
		}},
		"get_players": map[string]any{"players": map[string]any{"player": []any{
			map[string]any{"id": "15751", "name": "London, Drake", "position": "WR", "team": "ATL", "draft_year": "2022"},
			map[string]any{"id": "15418", "name": "Bynum, Camryn", "position": "S", "team": "IND", "draft_year": "2021"},
			map[string]any{"id": "17000", "name": "Rookie, Example", "position": "RB", "team": "DEN", "draft_year": "2026"},
			map[string]any{"id": "17001", "name": "Taken, Rookie", "position": "WR", "team": "SEA", "draft_year": "2026"},
		}}},
		"get_rosters": map[string]any{"rosters": map[string]any{"franchise": map[string]any{
			"id": "0005", "player": []any{
				map[string]any{"id": "15751", "salary": "10", "status": "ROSTER"},
				map[string]any{"id": "15418", "salary": "8", "status": "INJURED_RESERVE"},
				map[string]any{"id": "17001", "salary": "5", "status": "TAXI_SQUAD"},
			},
		}}},
		"get_salary_adjustments": map[string]any{"salaryAdjustments": map[string]any{"salaryAdjustment": map[string]any{
			"franchise_id": "0005", "amount": "2",
		}}},
		"get_player_profiles": map[string]any{"playerProfile": map[string]any{"player": []any{
			map[string]any{"id": "15751", "birthdate": "2000-01-01"},
			map[string]any{"id": "15418", "birthdate": "1998-07-19"},
		}}},
		"get_free_agents": map[string]any{"freeAgents": map[string]any{"leagueUnit": map[string]any{"player": []any{
			map[string]any{"id": "17000"}, map[string]any{"id": "15751"},
		}}}},
		"get_assets": map[string]any{"assets": map[string]any{"franchise": []any{
			map[string]any{"id": "0005", "currentYearDraftPick": []any{
				map[string]any{"id": "DP_00_05", "originalFranchise": "0005"},
				map[string]any{"id": "DP_01_05", "originalFranchise": "0005"},
			}},
			map[string]any{"id": "0002", "currentYearDraftPick": map[string]any{"id": "DP_00_01"}},
		}}},
		"get_future_draft_picks": map[string]any{"futureDraftPicks": map[string]any{"franchise": []any{
			map[string]any{"id": "0005", "futureDraftPick": []any{
				map[string]any{"year": "2027", "round": "1", "originalFranchise": "0005"},
				map[string]any{"year": "2027", "round": "2", "originalFranchise": "0005"},
			}},
			map[string]any{"id": "0002", "futureDraftPick": map[string]any{"year": "2027", "round": "1", "originalFranchise": "0002"}},
		}}},
		"get_draft_results": map[string]any{"draftResults": map[string]any{
			"status": "IN_PROGRESS", "message": "Team McLean is on the clock",
		}},
		"get_live_draft_results": map[string]any{"format": "xml", "xml": `<draftResults franchise_id="0005" paused="0" stopped="0" round="01" pick="07">
  <draftPick round="01" pick="06" franchise="0005" player="17000" />
  <draftPick round="01" pick="02" franchise="0002" />
</draftResults>`},
	}}
}

func pickLabels(picks []source.Pick) []string {
	labels := make([]string, 0, len(picks))
	for _, pick := range picks {
		labels = append(labels, pick.Pick)
	}
	return labels
}

func teamMcLeanFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "data", "team-mclean-2026-source.json")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return path + ".bak"
}
