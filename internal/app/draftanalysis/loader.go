// Package draftanalysis orchestrates data loading and the existing draft and
// roster analyses. It is shared by the CLI and future Lambda handlers.
package draftanalysis

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	mflsync "github.com/tyler180/dynasty-ff-backend/internal/provider/mfl"
	source "github.com/tyler180/dynasty-ff-models/analysis"
)

type LoadOptions struct {
	SourcePath       string
	RefreshMFL       bool
	Command          string
	CommandArguments []string
	LeagueID         string
	FranchiseID      string
	Year             int
	ProjectionPath   string
	LeagueConfigPath string
	SnapshotDate     string
	IncludeDraft     bool
	LiveDraft        bool
	FastDraft        bool
	Timeout          time.Duration
	ExportPath       string
}

type LoadResult struct {
	Snapshot source.Snapshot
	Refresh  *RefreshSummary
}

type RefreshSummary struct {
	RosterPlayers int
	TotalCapHit   float64
	Warnings      []string
	SyncedAt      time.Time
}

type ManagedCaller interface {
	mflsync.Caller
	Close() error
}

type ConnectFunc func(context.Context, string, ...string) (ManagedCaller, error)

type Loader struct {
	Connect ConnectFunc
	Now     func() time.Time
}

func NewLoader() Loader {
	return Loader{
		Connect: func(ctx context.Context, command string, arguments ...string) (ManagedCaller, error) {
			return mflsync.ConnectCommand(ctx, command, arguments...)
		},
		Now: time.Now,
	}
}

func (l Loader) Load(ctx context.Context, options LoadOptions) (LoadResult, error) {
	if !options.RefreshMFL {
		if options.ExportPath != "" || options.ProjectionPath != "" || options.LeagueConfigPath != "" || options.LiveDraft || options.FastDraft ||
			options.LeagueID != "" || options.FranchiseID != "" || options.Year != 0 ||
			options.SnapshotDate != "" || len(options.CommandArguments) > 0 {
			return LoadResult{}, fmt.Errorf("MFL refresh options require -refresh-mfl")
		}
		snapshot, err := source.Read(options.SourcePath)
		return LoadResult{Snapshot: snapshot}, err
	}
	if options.Command == "" {
		return LoadResult{}, fmt.Errorf("-mcp-command or MFL_MCP_COMMAND is required with -refresh-mfl")
	}
	if options.LiveDraft && !options.IncludeDraft {
		return LoadResult{}, fmt.Errorf("-live-draft requires -draft=true")
	}
	if options.Timeout <= 0 {
		return LoadResult{}, fmt.Errorf("-timeout must be positive")
	}
	if options.ExportPath == "-" {
		return LoadResult{}, fmt.Errorf("-export-snapshot must be a file path; stdout is reserved for analysis")
	}

	now := l.Now
	if now == nil {
		now = time.Now
	}
	when := now()
	var err error
	if options.SnapshotDate != "" {
		when, err = time.Parse("2006-01-02", options.SnapshotDate)
		if err != nil {
			return LoadResult{}, fmt.Errorf("parse snapshot date: %w", err)
		}
	}

	resolvedYear := options.Year
	if resolvedYear == 0 && options.SourcePath != "" {
		base, loadErr := mflsync.LoadBase(options.SourcePath)
		if loadErr != nil {
			return LoadResult{}, loadErr
		}
		if parsed, parseErr := time.Parse("2006-01-02", base.Snapshot.SnapshotDate); parseErr == nil {
			resolvedYear = parsed.Year()
		}
	}
	if resolvedYear == 0 {
		resolvedYear = when.Year()
	}
	if resolvedYear < 2000 || resolvedYear > 2100 {
		return LoadResult{}, fmt.Errorf("year must be a season between 2000 and 2100")
	}

	var base mflsync.BaseDocument
	resolvedLeagueID := options.LeagueID
	resolvedFranchiseID := options.FranchiseID
	if options.SourcePath != "" {
		base, err = mflsync.LoadBase(options.SourcePath)
		if err != nil {
			return LoadResult{}, err
		}
		if resolvedLeagueID == "" {
			resolvedLeagueID = base.Snapshot.League.ID
		}
		if resolvedFranchiseID == "" {
			resolvedFranchiseID = base.Snapshot.Franchise.ID
		}
	} else {
		if resolvedLeagueID == "" {
			return LoadResult{}, fmt.Errorf("-league or MFL_LEAGUE_ID is required without -source")
		}
		if resolvedFranchiseID == "" {
			return LoadResult{}, fmt.Errorf("-franchise or MFL_FRANCHISE_ID is required without -source")
		}
		base = mflsync.NewBase(resolvedYear, resolvedLeagueID, resolvedFranchiseID, when)
	}

	configPath := options.LeagueConfigPath
	if configPath == "" && options.SourcePath == "" {
		candidate := filepath.Join("config", "league-"+resolvedLeagueID+".json")
		if _, statErr := os.Stat(candidate); statErr == nil {
			configPath = candidate
		}
	}
	if configPath != "" {
		config, err := mflsync.LoadLeagueConfig(configPath, base.Snapshot.League.ID)
		if err != nil {
			return LoadResult{}, err
		}
		mflsync.ApplyLeagueConfig(&base, config)
	}

	syncOptions := mflsync.Options{
		Year:         resolvedYear,
		LeagueID:     resolvedLeagueID,
		FranchiseID:  resolvedFranchiseID,
		SnapshotDate: when,
		IncludeDraft: options.IncludeDraft,
		LiveDraft:    options.LiveDraft,
		FastDraft:    options.FastDraft,
	}
	if options.ProjectionPath != "" {
		projections, err := mflsync.LoadProjections(options.ProjectionPath, resolvedYear)
		if err != nil {
			return LoadResult{}, err
		}
		syncOptions.Projections = &projections
	}

	connect := l.Connect
	if connect == nil {
		connect = NewLoader().Connect
	}
	refreshContext, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	caller, err := connect(refreshContext, options.Command, options.CommandArguments...)
	if err != nil {
		return LoadResult{}, err
	}
	defer caller.Close()

	result, err := mflsync.Sync(refreshContext, caller, base, syncOptions)
	if err != nil {
		return LoadResult{}, err
	}
	if options.ExportPath != "" {
		payload, err := mflsync.Render(base, result)
		if err != nil {
			return LoadResult{}, err
		}
		if err := mflsync.WriteFile(options.ExportPath, payload); err != nil {
			return LoadResult{}, err
		}
	}
	return LoadResult{
		Snapshot: result.Snapshot,
		Refresh: &RefreshSummary{
			RosterPlayers: len(result.Snapshot.Roster),
			TotalCapHit:   result.Snapshot.Franchise.TotalCapHit,
			Warnings:      append([]string(nil), result.Warnings...),
			SyncedAt:      result.SyncedAt,
		},
	}, nil
}

func ParseOptionalYear(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	year, err := strconv.Atoi(value)
	if err != nil || year < 2000 || year > 2100 {
		return 0, fmt.Errorf("year must be a season between 2000 and 2100")
	}
	return year, nil
}
