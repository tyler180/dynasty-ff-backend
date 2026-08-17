// Package player defines provider-independent player identity.
package player

import (
	"crypto/sha256"
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

// DeterministicID creates a stable canonical ID when a provider-native player
// appears before it is present in the cross-provider identity feed.
func DeterministicID(externalID ExternalID) (ID, error) {
	if err := externalID.Validate(); err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte("dynasty-ff-player-v1:" + string(externalID.Provider) + ":" + strings.TrimSpace(externalID.Value)))
	digest[6] = (digest[6] & 0x0f) | 0x50
	digest[8] = (digest[8] & 0x3f) | 0x80
	return ID(fmt.Sprintf("player-%x-%x-%x-%x-%x", digest[0:4], digest[4:6], digest[6:8], digest[8:10], digest[10:16])), nil
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
