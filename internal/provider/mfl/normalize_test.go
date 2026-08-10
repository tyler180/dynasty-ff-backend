package mflsync

import (
	"reflect"
	"testing"
)

func TestBirthdatesParsesRealPlayerProfileShape(t *testing.T) {
	payload := map[string]any{"playerProfiles": map[string]any{"playerProfile": []any{
		map[string]any{"id": "15751", "player": map[string]any{"id": "15751", "dob": "Jul 24, 2001"}},
		map[string]any{"id": "15418", "player": map[string]any{"id": "15418", "dob": "Jul 19, 1998"}},
	}}}
	result := birthdates(payload)
	if result["15751"] != 995932800 || result["15418"] != 900806400 {
		t.Fatalf("birthdates = %v", result)
	}
}

func TestDraftResultsProvideOwnedPicksAndState(t *testing.T) {
	base := BaseDocument{
		Raw: map[string]any{"draft": map[string]any{"rookie_salary_schedule": []any{
			map[string]any{"selection_range": "1.01-1.04", "salary": 17.0},
			map[string]any{"selection_range": "round 2", "salary": 7.0},
		}}},
	}
	payload := map[string]any{"draftResults": map[string]any{"draftUnit": map[string]any{"draftPick": []any{
		map[string]any{"round": "01", "pick": "01", "franchise": "0002", "player": "17000"},
		map[string]any{"round": "01", "pick": "02", "franchise": "0005", "player": "", "comments": "[Pick traded from Other Team.] "},
		map[string]any{"round": "01", "pick": "03", "franchise": "0005", "player": "17001"},
		map[string]any{"round": "02", "pick": "01", "franchise": "0005", "player": ""},
	}}}}
	picks, ok := ownedPicksFromDraftResults(payload, "0005", base, map[string]string{"0005": "Team McLean"})
	if !ok || len(picks) != 2 {
		t.Fatalf("picks = %+v, ok = %v", picks, ok)
	}
	if picks[0].Pick != "1.02" || picks[0].Overall != 2 || picks[0].Salary != 17 || picks[0].OriginalOwner != "Other Team" {
		t.Fatalf("first pick = %+v", picks[0])
	}
	if picks[1].Pick != "2.01" || picks[1].Overall != 4 || picks[1].Salary != 7 {
		t.Fatalf("second pick = %+v", picks[1])
	}
	status, _ := draftStatus(payload, "not_scheduled")
	if status != "in_progress" {
		t.Fatalf("status = %q, want in_progress", status)
	}
}

func TestFuturePickInventorySelectsOnlyRequestedFranchise(t *testing.T) {
	payload := map[string]any{"futureDraftPicks": map[string]any{"franchise": []any{
		map[string]any{"id": "0005", "futureDraftPick": []any{
			map[string]any{"year": "2027", "round": "1", "originalPickFor": "0005"},
			map[string]any{"year": "2027", "round": "2", "originalPickFor": "0002"},
		}},
		map[string]any{"id": "0002", "futureDraftPick": map[string]any{"year": "2027", "round": "3", "originalPickFor": "0002"}},
	}}}
	result, ok := futurePickInventory(payload, "0005", map[string]string{"0005": "Team McLean", "0002": "Other Team"})
	if !ok || len(result) != 2 {
		t.Fatalf("inventory = %#v, ok = %v", result, ok)
	}
	first, _ := object(result[0])
	second, _ := object(result[1])
	if !reflect.DeepEqual(first["rounds"], []int{2}) || !reflect.DeepEqual(second["rounds"], []int{1}) {
		t.Fatalf("inventory included wrong rounds: %#v", result)
	}
}

func TestParseLiveDraftTracksOnClockAndSelectedPlayers(t *testing.T) {
	payload := map[string]any{"format": "xml", "xml": `<?xml version="1.0"?>
<draftResults franchise_id="0005" paused="0" stopped="0" round="02" pick="03">
  <draftPick round="01" pick="01" franchise="0002" player="17000" />
  <draftPick round="01" pick="02" franchise="0005" />
</draftResults>`}
	info, err := parseLiveDraft(payload)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != "in_progress" || info.OnClockFranchise != "0005" || info.Round != 2 || info.Pick != 3 {
		t.Fatalf("live draft info = %+v", info)
	}
	if !reflect.DeepEqual(info.DraftedPlayerIDs, []string{"17000"}) {
		t.Fatalf("drafted IDs = %v", info.DraftedPlayerIDs)
	}
	if !reflect.DeepEqual(info.CompletedPicks, []string{"1.01"}) {
		t.Fatalf("completed picks = %v", info.CompletedPicks)
	}
}
