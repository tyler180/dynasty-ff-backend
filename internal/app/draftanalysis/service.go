package draftanalysis

import (
	"context"
	"fmt"

	"github.com/tyler180/dynasty-ff-backend/internal/analysis/source"
)

type Request struct {
	Load               LoadOptions
	CapReliefTarget    float64
	ProjectionFallback string
}

type Result struct {
	Snapshot source.Snapshot
	Analysis source.Analysis
	Refresh  *RefreshSummary
}

type Service struct {
	Loader Loader
}

func NewService() Service {
	return Service{Loader: NewLoader()}
}

func (s Service) Run(ctx context.Context, request Request) (Result, error) {
	loaded, err := s.Loader.Load(ctx, request.Load)
	if err != nil {
		return Result{}, err
	}
	fallback := request.ProjectionFallback
	if fallback == "" || fallback == "auto" {
		fallback = "none"
		if len(loaded.Snapshot.Projections.ByPlayerID) == 0 && len(loaded.Snapshot.HistoricalPoints.Seasons) > 0 {
			fallback = "historical"
		}
	}
	if fallback != "none" && fallback != "historical" {
		return Result{}, fmt.Errorf("unknown projection fallback %q; use auto, none, or historical", fallback)
	}
	analysis := source.AnalyzeWithOptions(loaded.Snapshot, source.AnalysisOptions{
		CapReliefTarget:    request.CapReliefTarget,
		ProjectionFallback: fallback,
	})
	return Result{Snapshot: loaded.Snapshot, Analysis: analysis, Refresh: loaded.Refresh}, nil
}
