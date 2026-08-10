package source

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

func Read(path string) (Snapshot, error) {
	file, err := os.Open(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("open source snapshot: %w", err)
	}
	defer file.Close()

	var snapshot Snapshot
	if err := json.NewDecoder(file).Decode(&snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("decode source snapshot: %w", err)
	}
	if err := snapshot.Validate(); err != nil {
		return Snapshot{}, fmt.Errorf("validate source snapshot: %w", err)
	}
	return snapshot, nil
}

func (s Snapshot) Validate() error {
	if _, err := time.Parse("2006-01-02", s.SnapshotDate); err != nil {
		return errors.New("snapshot_date must use YYYY-MM-DD")
	}
	if s.League.Name == "" || s.Franchise.Name == "" {
		return errors.New("league and franchise names are required")
	}
	if s.League.SalaryCap <= 0 || s.League.ActiveRosterLimit <= 0 || s.League.TaxiSquadLimit < 0 {
		return errors.New("league cap and roster limits are invalid")
	}
	if len(s.Roster) == 0 {
		return errors.New("roster is required")
	}
	return nil
}
