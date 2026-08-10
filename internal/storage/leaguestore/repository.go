// Package leaguestore defines persistence boundaries for dated league state.
package leaguestore

import (
	"context"
	"time"

	"github.com/tylermclean/dynasty-ff-backend/internal/domain/league"
)

type Reader interface {
	LatestSnapshot(context.Context, league.ID, league.FranchiseID, int) (league.Snapshot, error)
	SnapshotAt(context.Context, league.ID, league.FranchiseID, int, time.Time) (league.Snapshot, error)
}

type Writer interface {
	PutSnapshot(context.Context, league.Snapshot) error
}

type Repository interface {
	Reader
	Writer
}
