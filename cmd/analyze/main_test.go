package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadSourceWithoutRefreshReadsBaseSnapshot(t *testing.T) {
	path := filepath.Join("..", "..", "data", "team-mclean-2026-source.json")
	if _, err := os.Stat(path); err != nil {
		path += ".bak"
	}
	snapshot, err := loadSource(sourceRefreshOptions{
		SourcePath: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.League.ID == "" || snapshot.Franchise.ID == "" || len(snapshot.Roster) == 0 {
		t.Fatalf("source snapshot was not loaded: league=%q franchise=%q roster=%d",
			snapshot.League.ID, snapshot.Franchise.ID, len(snapshot.Roster))
	}
}

func TestLoadSourceRefreshRequiresMCPCommand(t *testing.T) {
	_, err := loadSource(sourceRefreshOptions{RefreshMFL: true, Timeout: time.Minute})
	if err == nil || !strings.Contains(err.Error(), "MFL_MCP_COMMAND") {
		t.Fatalf("error = %v, want missing MCP command", err)
	}
}

func TestLoadSourceWithoutBaseRequiresMFLIdentity(t *testing.T) {
	t.Setenv("MFL_LEAGUE_ID", "")
	t.Setenv("MFL_FRANCHISE_ID", "")
	_, err := loadSource(sourceRefreshOptions{
		RefreshMFL: true,
		Command:    "mfl-mcp",
		Year:       2026,
		Timeout:    time.Minute,
	})
	if err == nil || !strings.Contains(err.Error(), "MFL_LEAGUE_ID") {
		t.Fatalf("error = %v, want missing MFL league identity", err)
	}
}

func TestYearFromEnvironment(t *testing.T) {
	t.Setenv("MFL_YEAR", "2027")
	year, err := yearFromEnvironment()
	if err != nil || year != 2027 {
		t.Fatalf("year = %d, error = %v", year, err)
	}
}

func TestLoadSourceRejectsLiveDraftWithoutDraftRefresh(t *testing.T) {
	_, err := loadSource(sourceRefreshOptions{
		RefreshMFL:   true,
		Command:      "mfl-mcp",
		LiveDraft:    true,
		IncludeDraft: false,
		Timeout:      time.Minute,
	})
	if err == nil || !strings.Contains(err.Error(), "-live-draft requires -draft=true") {
		t.Fatalf("error = %v, want incompatible draft options", err)
	}
}

func TestLoadSourceRejectsSnapshotExportWithoutRefresh(t *testing.T) {
	_, err := loadSource(sourceRefreshOptions{ExportPath: "live.json"})
	if err == nil || !strings.Contains(err.Error(), "require -refresh-mfl") {
		t.Fatalf("error = %v, want export refresh requirement", err)
	}
}

func TestLoadSourceRejectsProjectionsWithoutRefresh(t *testing.T) {
	_, err := loadSource(sourceRefreshOptions{ProjectionPath: "projections.json"})
	if err == nil || !strings.Contains(err.Error(), "require -refresh-mfl") {
		t.Fatalf("error = %v, want projection refresh requirement", err)
	}
}
