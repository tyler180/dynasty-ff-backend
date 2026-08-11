// Package league defines provider-independent fantasy league state.
package league

import (
	"fmt"
	"strings"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
)

type ID string
type FranchiseID string

type RosterStatus string

const (
	RosterActive         RosterStatus = "active"
	RosterInjuredReserve RosterStatus = "injured_reserve"
	RosterTaxi           RosterStatus = "taxi"
)

// Snapshot is league state as observed at a point in time. Historical
// snapshots are immutable; a new observation produces a new snapshot.
type Snapshot struct {
	League      League             `json:"league"`
	Franchise   Franchise          `json:"franchise"`
	Roster      []RosterAssignment `json:"roster"`
	DraftAssets []DraftAsset       `json:"draft_assets,omitempty"`
	ObservedAt  time.Time          `json:"observed_at"`
	Source      string             `json:"source"`
}

type League struct {
	ID                  ID      `json:"id"`
	Name                string  `json:"name"`
	Season              int     `json:"season"`
	SalaryCap           float64 `json:"salary_cap"`
	ActiveRosterLimit   int     `json:"active_roster_limit"`
	InjuredReserveLimit int     `json:"injured_reserve_limit"`
	TaxiSquadLimit      int     `json:"taxi_squad_limit"`
}

type Franchise struct {
	ID   FranchiseID `json:"id"`
	Name string      `json:"name"`
}

type RosterAssignment struct {
	PlayerID        player.ID    `json:"player_id"`
	Status          RosterStatus `json:"status"`
	Salary          float64      `json:"salary"`
	CurrentCapHit   float64      `json:"current_cap_hit"`
	ContractThrough int          `json:"contract_through,omitempty"`
}

type DraftAsset struct {
	Season              int         `json:"season"`
	Round               int         `json:"round"`
	Pick                int         `json:"pick,omitempty"`
	Overall             int         `json:"overall,omitempty"`
	OriginalFranchiseID FranchiseID `json:"original_franchise_id,omitempty"`
	CurrentFranchiseID  FranchiseID `json:"current_franchise_id"`
	Salary              float64     `json:"salary,omitempty"`
}

func (s Snapshot) Validate() error {
	if strings.TrimSpace(string(s.League.ID)) == "" || strings.TrimSpace(string(s.Franchise.ID)) == "" {
		return fmt.Errorf("league and franchise IDs are required")
	}
	if s.League.Season < 2000 || s.League.Season > 2100 {
		return fmt.Errorf("league season is invalid")
	}
	if s.ObservedAt.IsZero() {
		return fmt.Errorf("snapshot observed_at is required")
	}
	if strings.TrimSpace(s.Source) == "" {
		return fmt.Errorf("snapshot source is required")
	}
	return nil
}
