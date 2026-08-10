// Package identity defines persistence boundaries for canonical player IDs.
package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tylermclean/dynasty-ff-backend/internal/identity/player"
)

var (
	ErrPlayerNotFound = errors.New("player identity not found")
	ErrAliasConflict  = errors.New("player alias conflicts with an existing resolution")
)

// Alias records how an external identifier was resolved. Manual resolutions
// can be preserved when automated provider crosswalks are refreshed.
type Alias struct {
	ExternalID       player.ExternalID `json:"external_id"`
	PlayerID         player.ID         `json:"player_id"`
	Source           string            `json:"source"`
	ResolutionMethod string            `json:"resolution_method"`
	Confidence       float64           `json:"confidence,omitempty"`
	ManualOverride   bool              `json:"manual_override,omitempty"`
	SourceUpdatedAt  *time.Time        `json:"source_updated_at,omitempty"`
	IngestedAt       time.Time         `json:"ingested_at"`
}

func (a Alias) Validate() error {
	if err := a.ExternalID.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(string(a.PlayerID)) == "" {
		return fmt.Errorf("alias canonical player ID is required")
	}
	if strings.TrimSpace(a.Source) == "" {
		return fmt.Errorf("alias source is required")
	}
	if strings.TrimSpace(a.ResolutionMethod) == "" {
		return fmt.Errorf("alias resolution method is required")
	}
	if a.Confidence < 0 || a.Confidence > 1 {
		return fmt.Errorf("alias confidence must be between 0 and 1")
	}
	return nil
}

type Reader interface {
	GetPlayer(context.Context, player.ID) (player.Profile, error)
	ResolvePlayer(context.Context, player.ExternalID) (player.Profile, error)
}

type Writer interface {
	PutPlayer(context.Context, player.Profile) error
	PutAlias(context.Context, Alias) error
}

type Repository interface {
	Reader
	Writer
}
