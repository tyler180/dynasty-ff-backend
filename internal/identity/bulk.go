package identity

import (
	"context"

	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
)

// BulkResolver resolves a large provider crosswalk without issuing one
// DynamoDB request per player.
type BulkResolver interface {
	ResolvePlayers(context.Context, []player.ExternalID) (map[player.ExternalID]player.Profile, error)
}
