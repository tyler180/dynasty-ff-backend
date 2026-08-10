package mflsync

import (
	"fmt"
	"math"
	"strings"

	"github.com/tylermclean/dynasty-ff-backend/internal/analysis/source"
)

func Validate(snapshot source.Snapshot) error {
	if err := snapshot.Validate(); err != nil {
		return fmt.Errorf("validate synced snapshot: %w", err)
	}
	seen := make(map[string]bool, len(snapshot.Roster))
	counts := map[string]int{"ROSTER": 0, "INJURED_RESERVE": 0, "TAXI_SQUAD": 0}
	for index, player := range snapshot.Roster {
		if strings.TrimSpace(player.ID) == "" || strings.TrimSpace(player.Name) == "" || strings.TrimSpace(player.Position) == "" {
			return fmt.Errorf("validate synced snapshot: roster[%d] is missing ID, name, or position", index)
		}
		if seen[player.ID] {
			return fmt.Errorf("validate synced snapshot: duplicate roster player ID %s", player.ID)
		}
		seen[player.ID] = true
		if _, ok := counts[player.Status]; !ok {
			return fmt.Errorf("validate synced snapshot: player %s has unsupported status %s", player.ID, player.Status)
		}
		if player.Salary < 0 || player.CurrentCapHit < 0 || math.IsNaN(player.Salary) || math.IsNaN(player.CurrentCapHit) {
			return fmt.Errorf("validate synced snapshot: player %s has an invalid salary", player.ID)
		}
		counts[player.Status]++
	}
	if counts["ROSTER"] != snapshot.Franchise.ActivePlayers ||
		counts["INJURED_RESERVE"] != snapshot.Franchise.InjuredReservePlayers ||
		counts["TAXI_SQUAD"] != snapshot.Franchise.TaxiSquadPlayers {
		return fmt.Errorf("validate synced snapshot: franchise roster counts do not match normalized roster")
	}
	if snapshot.Franchise.TotalCapHit < 0 || snapshot.Franchise.CurrentCapSpace != round(snapshot.League.SalaryCap-snapshot.Franchise.TotalCapHit, 2) {
		return fmt.Errorf("validate synced snapshot: franchise cap totals are inconsistent")
	}
	return nil
}
