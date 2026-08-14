package mflsync

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	source "github.com/tyler180/dynasty-ff-models/analysis"
)

type BaseDocument struct {
	Snapshot                source.Snapshot
	Raw                     map[string]any
	SalaryMultipliers       map[string]float64
	Bootstrapped            bool
	HasSalaryMultipliers    bool
	HasRookieSalarySchedule bool
}

func NewBase(year int, leagueID, franchiseID string, snapshotDate time.Time) BaseDocument {
	if snapshotDate.IsZero() {
		snapshotDate = time.Now()
	}
	snapshot := source.Snapshot{
		SnapshotDate:   snapshotDate.Format("2006-01-02"),
		Purpose:        "Live MFL data assembled in memory for dynasty analysis.",
		League:         source.League{ID: leagueID},
		Franchise:      source.Franchise{ID: franchiseID},
		BirthdatesUnix: map[string]int64{},
		Projections: source.Projections{
			Season: year, ByPlayerID: map[string]float64{},
		},
		Draft: source.Draft{Status: "unknown", CurrentYearPicks: []source.Pick{}},
	}
	return BaseDocument{
		Snapshot: snapshot,
		Raw:      map[string]any{},
		SalaryMultipliers: map[string]float64{
			"ROSTER": 1, "INJURED_RESERVE": 0.5, "TAXI_SQUAD": 0,
		},
		Bootstrapped: true,
	}
}

func LoadBase(path string) (BaseDocument, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return BaseDocument{}, fmt.Errorf("read base snapshot: %w", err)
	}
	var snapshot source.Snapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return BaseDocument{}, fmt.Errorf("decode base snapshot: %w", err)
	}
	if err := snapshot.Validate(); err != nil {
		return BaseDocument{}, fmt.Errorf("validate base snapshot: %w", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return BaseDocument{}, fmt.Errorf("decode base document: %w", err)
	}
	multipliers := map[string]float64{
		"ROSTER":          1,
		"INJURED_RESERVE": 0.5,
		"TAXI_SQUAD":      0,
	}
	if league, ok := object(raw["league"]); ok {
		if values, ok := object(league["salary_cap_multipliers"]); ok {
			for status := range multipliers {
				if value, ok := floatValue(values[status]); ok {
					multipliers[status] = value
				}
			}
		}
	}
	return BaseDocument{
		Snapshot: snapshot, Raw: raw, SalaryMultipliers: multipliers,
		HasSalaryMultipliers: true, HasRookieSalarySchedule: hasRookieSalarySchedule(raw),
	}, nil
}

func hasRookieSalarySchedule(raw map[string]any) bool {
	draft, ok := object(raw["draft"])
	return ok && len(values(draft["rookie_salary_schedule"])) > 0
}

type Result struct {
	Snapshot source.Snapshot
	Extra    map[string]any
	Warnings []string
	SyncedAt time.Time
}

func Render(base BaseDocument, result Result) ([]byte, error) {
	document, err := cloneObject(base.Raw)
	if err != nil {
		return nil, err
	}
	overlayPayload, err := json.Marshal(result.Snapshot)
	if err != nil {
		return nil, fmt.Errorf("encode refreshed snapshot: %w", err)
	}
	var overlay map[string]any
	if err := json.Unmarshal(overlayPayload, &overlay); err != nil {
		return nil, fmt.Errorf("decode refreshed snapshot overlay: %w", err)
	}
	deepMerge(document, overlay)
	deepMerge(document, result.Extra)
	document["sync"] = map[string]any{
		"adapter":   "dynasty-sync",
		"synced_at": result.SyncedAt.UTC().Format(time.RFC3339),
		"warnings":  result.Warnings,
	}
	payload, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode output snapshot: %w", err)
	}
	return append(payload, '\n'), nil
}

func WriteFile(path string, payload []byte) error {
	if path == "" {
		return errors.New("output path is required")
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".dynasty-sync-*.json")
	if err != nil {
		return fmt.Errorf("create temporary output: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(payload); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary output: %w", err)
	}
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return fmt.Errorf("set output permissions: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary output: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace output snapshot: %w", err)
	}
	return nil
}

func cloneObject(value map[string]any) (map[string]any, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("clone base document: %w", err)
	}
	var cloned map[string]any
	if err := json.Unmarshal(payload, &cloned); err != nil {
		return nil, fmt.Errorf("clone base document: %w", err)
	}
	return cloned, nil
}

func deepMerge(destination, overlay map[string]any) {
	for key, value := range overlay {
		overlayObject, overlayIsObject := object(value)
		destinationObject, destinationIsObject := object(destination[key])
		if overlayIsObject && destinationIsObject {
			deepMerge(destinationObject, overlayObject)
			continue
		}
		destination[key] = value
	}
}
