// Package player defines provider-independent player identity.
package player

import (
	"fmt"
	"strings"
	"time"
)

// ID is the application's stable player identifier. Provider identifiers such
// as MFL and PFR IDs are aliases and must not be used as canonical IDs.
type ID string

// Provider identifies an external player data source.
type Provider string

const (
	ProviderMFL         Provider = "mfl"
	ProviderGSIS        Provider = "gsis"
	ProviderPFR         Provider = "pfr"
	ProviderPFF         Provider = "pff"
	ProviderESPN        Provider = "espn"
	ProviderSleeper     Provider = "sleeper"
	ProviderFantasyPros Provider = "fantasypros"
	ProviderOverTheCap  Provider = "overthecap"
	ProviderNFL         Provider = "nfl"
	ProviderYahoo       Provider = "yahoo"
	ProviderCBS         Provider = "cbs"
	ProviderFleaflicker Provider = "fleaflicker"
	ProviderRotowire    Provider = "rotowire"
	ProviderKTC         Provider = "ktc"
	ProviderFantasyData Provider = "fantasydata"
	ProviderSportradar  Provider = "sportradar"
	ProviderCFBRef      Provider = "cfbref"
)

// ExternalID associates a provider's identifier with a canonical player.
type ExternalID struct {
	Provider Provider `json:"provider"`
	Value    string   `json:"value"`
}

func (id ExternalID) Validate() error {
	if strings.TrimSpace(string(id.Provider)) == "" {
		return fmt.Errorf("external player ID provider is required")
	}
	if strings.TrimSpace(id.Value) == "" {
		return fmt.Errorf("external player ID value is required")
	}
	return nil
}

// Profile contains player attributes that are shared by analyses. Mutable
// observations such as current team and weekly position belong in dated facts.
type Profile struct {
	ID          ID           `json:"id"`
	DisplayName string       `json:"display_name"`
	BirthDate   *time.Time   `json:"birth_date,omitempty"`
	RookieYear  int          `json:"rookie_year,omitempty"`
	Draft       *DraftRecord `json:"draft,omitempty"`
}

type DraftRecord struct {
	Year    int    `json:"year"`
	Round   int    `json:"round,omitempty"`
	Pick    int    `json:"pick,omitempty"`
	NFLTeam string `json:"nfl_team,omitempty"`
}

func (p Profile) Validate() error {
	if strings.TrimSpace(string(p.ID)) == "" {
		return fmt.Errorf("canonical player ID is required")
	}
	if strings.TrimSpace(p.DisplayName) == "" {
		return fmt.Errorf("player display name is required")
	}
	return nil
}
