// Package snapshotanalysis analyzes normalized league snapshots from durable storage.
package snapshotanalysis

import (
	"context"
	"fmt"
	"strings"
	"time"

	source "github.com/tyler180/dynasty-ff-models/analysis"
	"github.com/tyler180/dynasty-ff-backend/internal/domain/league"
	"github.com/tyler180/dynasty-ff-backend/internal/identity"
	"github.com/tyler180/dynasty-ff-backend/internal/storage/leaguestore"
)

type Request struct {
	Year               int
	LeagueID           league.ID
	FranchiseID        league.FranchiseID
	CapReliefTarget    float64
	ProjectionFallback string
}

type Result struct {
	SnapshotObservedAt time.Time       `json:"snapshot_observed_at"`
	ProjectionFallback string          `json:"projection_fallback"`
	Analysis           source.Analysis `json:"analysis"`
}

type Service struct {
	Snapshots leaguestore.Reader
	Players   identity.Reader
}

func (s Service) Analyze(ctx context.Context, request Request) (Result, error) {
	if s.Snapshots == nil || s.Players == nil {
		return Result{}, fmt.Errorf("snapshot and player repositories are required")
	}
	if request.Year < 2000 || request.Year > 2100 || strings.TrimSpace(string(request.LeagueID)) == "" || strings.TrimSpace(string(request.FranchiseID)) == "" {
		return Result{}, fmt.Errorf("analysis year, league ID, and franchise ID are required")
	}
	snapshot, err := s.Snapshots.LatestSnapshot(ctx, request.LeagueID, request.FranchiseID, request.Year)
	if err != nil {
		return Result{}, err
	}
	input, err := s.sourceSnapshot(ctx, snapshot)
	if err != nil {
		return Result{}, err
	}
	fallback := strings.ToLower(strings.TrimSpace(request.ProjectionFallback))
	if fallback == "" || fallback == "auto" {
		fallback = "none"
		if len(input.Projections.ByPlayerID) == 0 && len(input.HistoricalPoints.Seasons) > 0 {
			fallback = "historical"
		}
	}
	if fallback != "none" && fallback != "historical" {
		return Result{}, fmt.Errorf("unknown projection fallback %q; use auto, none, or historical", request.ProjectionFallback)
	}
	analysis := source.AnalyzeWithOptions(input, source.AnalysisOptions{
		CapReliefTarget: request.CapReliefTarget, ProjectionFallback: fallback,
	})
	return Result{SnapshotObservedAt: snapshot.ObservedAt, ProjectionFallback: fallback, Analysis: analysis}, nil
}

func (s Service) sourceSnapshot(ctx context.Context, snapshot league.Snapshot) (source.Snapshot, error) {
	result := source.Snapshot{
		SnapshotDate: snapshot.ObservedAt.UTC().Format("2006-01-02"),
		League: source.League{
			ID: string(snapshot.League.ID), Name: snapshot.League.Name, SalaryCap: snapshot.League.SalaryCap,
			ActiveRosterLimit: snapshot.League.ActiveRosterLimit, InjuredReserveLimit: snapshot.League.InjuredReserveLimit,
			TaxiSquadLimit: snapshot.League.TaxiSquadLimit,
		},
		Franchise:        source.Franchise{ID: string(snapshot.Franchise.ID), Name: snapshot.Franchise.Name},
		BirthdatesUnix:   map[string]int64{},
		HistoricalPoints: source.HistoricalPoints{Source: snapshot.HistoricalPoints.Source},
		ReplacementLevels: source.ReplacementLevels{
			Source: snapshot.ReplacementLevels.Source, Method: snapshot.ReplacementLevels.Method,
			MinimumHistoricalGames:  snapshot.ReplacementLevels.MinimumHistoricalGames,
			PointsPerGameByPosition: cloneFloatMap(snapshot.ReplacementLevels.PointsPerGameByPosition),
		},
		Projections: source.Projections{
			Season: snapshot.Projections.Season, Source: snapshot.Projections.Source,
			ByPlayerID: cloneFloatMap(snapshot.Projections.ByPlayerID),
		},
		Draft: source.Draft{
			Status: snapshot.DraftStatus,
			AvailabilityPollWindow: source.AvailabilityPollWindow{
				Start: snapshot.DraftAvailabilityWindow.Start, End: snapshot.DraftAvailabilityWindow.End,
			},
		},
	}
	for _, assignment := range snapshot.Roster {
		profile, err := s.Players.GetPlayer(ctx, assignment.PlayerID)
		if err != nil {
			return source.Snapshot{}, fmt.Errorf("load roster profile %s: %w", assignment.PlayerID, err)
		}
		result.Roster = append(result.Roster, source.Player{
			ID: string(assignment.PlayerID), Name: profile.DisplayName, Position: assignment.Position,
			NFLTeam: assignment.NFLTeam, Salary: assignment.Salary, Status: sourceRosterStatus(assignment.Status),
			CurrentCapHit: assignment.CurrentCapHit, RookieYear: profile.RookieYear,
		})
		if profile.BirthDate != nil {
			result.BirthdatesUnix[string(assignment.PlayerID)] = profile.BirthDate.Unix()
		}
		switch assignment.Status {
		case league.RosterActive:
			result.Franchise.ActivePlayers++
			result.Franchise.ActiveSalary += assignment.Salary
		case league.RosterInjuredReserve:
			result.Franchise.InjuredReservePlayers++
			result.Franchise.InjuredReserveCapHit += assignment.CurrentCapHit
		case league.RosterTaxi:
			result.Franchise.TaxiSquadPlayers++
			result.Franchise.TaxiSquadCapHit += assignment.CurrentCapHit
		}
		result.Franchise.TotalCapHit += assignment.CurrentCapHit
	}
	result.Franchise.CurrentCapSpace = snapshot.League.SalaryCap - result.Franchise.TotalCapHit
	for _, asset := range snapshot.DraftAssets {
		if asset.Season != snapshot.League.Season {
			continue
		}
		result.Draft.CurrentYearPicks = append(result.Draft.CurrentYearPicks, source.Pick{
			Pick: fmt.Sprintf("%d.%02d", asset.Round, asset.Pick), Overall: asset.Overall,
			Salary: asset.Salary, OriginalOwner: string(asset.OriginalFranchiseID),
		})
	}
	for _, season := range snapshot.HistoricalPoints.Seasons {
		result.HistoricalPoints.Seasons = append(result.HistoricalPoints.Seasons, source.HistoricalSeason{
			Season: season.Season, ByPlayerID: cloneFloatMap(season.ByPlayerID),
			GamesPlayedByPlayerID: cloneIntMap(season.GamesPlayedByPlayerID),
		})
	}
	return result, nil
}

func sourceRosterStatus(status league.RosterStatus) string {
	switch status {
	case league.RosterActive:
		return "ROSTER"
	case league.RosterInjuredReserve:
		return "INJURED_RESERVE"
	case league.RosterTaxi:
		return "TAXI_SQUAD"
	default:
		return strings.ToUpper(string(status))
	}
}

func cloneFloatMap(values map[string]float64) map[string]float64 {
	if len(values) == 0 {
		return map[string]float64{}
	}
	result := make(map[string]float64, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func cloneIntMap(values map[string]int) map[string]int {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]int, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
