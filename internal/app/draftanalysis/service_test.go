package draftanalysis

import (
	"context"
	"path/filepath"
	"testing"
)

func TestServiceLoadsAndAnalyzesLegacySnapshot(t *testing.T) {
	service := NewService()
	result, err := service.Run(context.Background(), Request{
		Load: LoadOptions{
			SourcePath: filepath.Join("..", "..", "..", "data", "team-mclean-2026-source.json"),
		},
		CapReliefTarget:    6,
		ProjectionFallback: "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Refresh != nil {
		t.Fatal("file-backed analysis was marked as an MFL refresh")
	}
	if result.Analysis.Draft.PickCount != 11 {
		t.Fatalf("draft pick count = %d, want 11", result.Analysis.Draft.PickCount)
	}
	if !result.Analysis.DropEvaluation.Available {
		t.Fatal("auto fallback did not use available historical data")
	}
}

func TestServiceRejectsUnknownProjectionFallback(t *testing.T) {
	service := NewService()
	_, err := service.Run(context.Background(), Request{
		Load: LoadOptions{
			SourcePath: filepath.Join("..", "..", "..", "data", "team-mclean-2026-source.json"),
		},
		ProjectionFallback: "guess",
	})
	if err == nil {
		t.Fatal("unknown projection fallback was accepted")
	}
}
