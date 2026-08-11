package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	mflsync "github.com/tyler180/dynasty-ff-backend/internal/provider/mfl"
)

func main() {
	basePath := flag.String("base", "", "verified source snapshot whose non-MFL rules should be preserved")
	outputPath := flag.String("output", "", "output snapshot path, or - for stdout")
	command := flag.String("mcp-command", strings.TrimSpace(os.Getenv("MFL_MCP_COMMAND")), "path to the mfl-mcp executable; defaults to MFL_MCP_COMMAND")
	leagueID := flag.String("league", "", "MFL league ID; defaults to the base snapshot")
	franchiseID := flag.String("franchise", "", "MFL franchise ID; defaults to the base snapshot")
	year := flag.Int("year", 0, "MFL season; defaults to the base snapshot year")
	projectionPath := flag.String("projections", "", "optional JSON projections file keyed by MFL player ID")
	snapshotDate := flag.String("snapshot-date", "", "snapshot date in YYYY-MM-DD; defaults to today")
	includeDraft := flag.Bool("draft", true, "refresh current picks, future picks, and draft status")
	liveDraft := flag.Bool("live-draft", false, "use MFL's near-real-time draft feed and exclude live selections from the rookie pool")
	timeout := flag.Duration("timeout", 3*time.Minute, "overall MCP sync timeout")
	var commandArguments stringList
	flag.Var(&commandArguments, "mcp-arg", "argument passed to the MCP executable; may be repeated")
	flag.Parse()

	if *basePath == "" || *outputPath == "" || *command == "" {
		fail(fmt.Errorf("-base, -output, and -mcp-command (or MFL_MCP_COMMAND) are required"))
	}
	if *liveDraft && !*includeDraft {
		fail(fmt.Errorf("-live-draft requires -draft=true"))
	}
	if *timeout <= 0 {
		fail(fmt.Errorf("-timeout must be positive"))
	}
	base, err := mflsync.LoadBase(*basePath)
	if err != nil {
		fail(err)
	}
	when := time.Now()
	if *snapshotDate != "" {
		when, err = time.ParseInLocation("2006-01-02", *snapshotDate, time.Local)
		if err != nil {
			fail(fmt.Errorf("parse snapshot date: %w", err))
		}
	}
	resolvedYear := *year
	if resolvedYear == 0 {
		if parsed, parseErr := time.Parse("2006-01-02", base.Snapshot.SnapshotDate); parseErr == nil {
			resolvedYear = parsed.Year()
		}
	}
	projectionsPathValue := strings.TrimSpace(*projectionPath)
	options := mflsync.Options{
		Year:         resolvedYear,
		LeagueID:     strings.TrimSpace(*leagueID),
		FranchiseID:  strings.TrimSpace(*franchiseID),
		SnapshotDate: when,
		IncludeDraft: *includeDraft,
		LiveDraft:    *liveDraft,
	}
	if projectionsPathValue != "" {
		projections, err := mflsync.LoadProjections(projectionsPathValue, resolvedYear)
		if err != nil {
			fail(err)
		}
		options.Projections = &projections
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	client, err := mflsync.ConnectCommand(ctx, *command, commandArguments...)
	if err != nil {
		fail(err)
	}
	defer client.Close()

	result, err := mflsync.Sync(ctx, client, base, options)
	if err != nil {
		fail(err)
	}
	payload, err := mflsync.Render(base, result)
	if err != nil {
		fail(err)
	}
	if *outputPath == "-" {
		if _, err := os.Stdout.Write(payload); err != nil {
			fail(fmt.Errorf("write stdout: %w", err))
		}
	} else if err := mflsync.WriteFile(*outputPath, payload); err != nil {
		fail(err)
	}
	fmt.Fprintf(os.Stderr, "dynasty-sync: wrote %d players with $%.2f cap usage; %d warning(s)\n",
		len(result.Snapshot.Roster), result.Snapshot.Franchise.TotalCapHit, len(result.Warnings))
}

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }

func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "dynasty-sync:", err)
	os.Exit(1)
}
