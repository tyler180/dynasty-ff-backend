// Package mflingest orchestrates read-only MFL acquisition, canonical identity
// resolution, and normalized snapshot persistence.
package mflingest

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/app/draftanalysis"
	"github.com/tyler180/dynasty-ff-backend/internal/domain/league"
	"github.com/tyler180/dynasty-ff-backend/internal/identity"
	mflsync "github.com/tyler180/dynasty-ff-backend/internal/provider/mfl"
	"github.com/tyler180/dynasty-ff-backend/internal/storage/leaguestore"
)

type Credentials interface {
	Environment(context.Context) (map[string]string, error)
}

type Request struct {
	Year             int
	LeagueID         string
	FranchiseID      string
	LeagueConfigPath string
	IncludeDraft     bool
	LiveDraft        bool
	Timeout          time.Duration
}

type Result struct {
	Snapshot league.Snapshot
	Warnings []string
	SyncedAt time.Time
}

type Service struct {
	MCPCommand  string
	Credentials Credentials
	Identities  identity.Repository
	Snapshots   leaguestore.Writer
	Now         func() time.Time
}

func (s Service) Sync(ctx context.Context, request Request) (Result, error) {
	if s.MCPCommand == "" {
		return Result{}, fmt.Errorf("MFL MCP command is required")
	}
	if s.Credentials == nil || s.Identities == nil || s.Snapshots == nil {
		return Result{}, fmt.Errorf("MFL credentials, identity repository, and snapshot repository are required")
	}
	if request.Year < 2000 || request.Year > 2100 || request.LeagueID == "" || request.FranchiseID == "" {
		return Result{}, fmt.Errorf("MFL year, league ID, and franchise ID are required")
	}
	if request.Timeout <= 0 {
		return Result{}, fmt.Errorf("MFL sync timeout must be positive")
	}
	environment, err := s.Credentials.Environment(ctx)
	if err != nil {
		return Result{}, err
	}
	if environment == nil {
		environment = map[string]string{}
	}
	environment["MFL_YEAR"] = strconv.Itoa(request.Year)
	environment["MFL_LEAGUE_ID"] = request.LeagueID

	now := s.Now
	if now == nil {
		now = time.Now
	}
	loader := draftanalysis.NewLoader()
	loader.Now = now
	loader.Connect = func(ctx context.Context, command string, arguments ...string) (draftanalysis.ManagedCaller, error) {
		return mflsync.ConnectCommandWithEnvironment(ctx, command, environment, arguments...)
	}
	loaded, err := loader.Load(ctx, draftanalysis.LoadOptions{
		RefreshMFL:       true,
		Command:          s.MCPCommand,
		Year:             request.Year,
		LeagueID:         request.LeagueID,
		FranchiseID:      request.FranchiseID,
		LeagueConfigPath: request.LeagueConfigPath,
		IncludeDraft:     request.IncludeDraft,
		LiveDraft:        request.LiveDraft,
		Timeout:          request.Timeout,
	})
	if err != nil {
		return Result{}, err
	}
	records, err := mflsync.NormalizeRecords(ctx, loaded.Snapshot, s.Identities, loaded.Refresh.SyncedAt)
	if err != nil {
		return Result{}, err
	}
	if err := s.Snapshots.PutSnapshot(ctx, records.LeagueSnapshot); err != nil {
		return Result{}, err
	}
	return Result{
		Snapshot: records.LeagueSnapshot,
		Warnings: append([]string(nil), loaded.Refresh.Warnings...),
		SyncedAt: loaded.Refresh.SyncedAt,
	}, nil
}
