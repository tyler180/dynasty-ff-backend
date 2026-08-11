package mflsync

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/analysis/source"
	"github.com/tyler180/dynasty-ff-backend/internal/domain/league"
	"github.com/tyler180/dynasty-ff-backend/internal/identity"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
)

// PlayerResolver is the identity capability needed to translate provider IDs
// before league facts are persisted. The DynamoDB identity repository can
// implement this without coupling MFL ingestion to DynamoDB itself.
type PlayerResolver interface {
	ResolvePlayer(context.Context, player.ExternalID) (player.Profile, error)
}

type NormalizedRecords struct {
	LeagueSnapshot league.Snapshot
	Players        []player.Profile
	Aliases        []identity.Alias
}

// NormalizeRecords converts the legacy draft snapshot into the shared domain
// records. It is the compatibility bridge while the existing analyzer still
// consumes source.Snapshot directly.
func NormalizeRecords(ctx context.Context, snapshot source.Snapshot, resolver PlayerResolver, observedAt time.Time) (NormalizedRecords, error) {
	if resolver == nil {
		return NormalizedRecords{}, fmt.Errorf("player resolver is required")
	}
	if observedAt.IsZero() {
		parsed, err := time.Parse("2006-01-02", snapshot.SnapshotDate)
		if err != nil {
			return NormalizedRecords{}, fmt.Errorf("parse snapshot date: %w", err)
		}
		observedAt = parsed
	}
	season := observedAt.Year()
	records := NormalizedRecords{
		LeagueSnapshot: league.Snapshot{
			League: league.League{
				ID:                  league.ID(snapshot.League.ID),
				Name:                snapshot.League.Name,
				Season:              season,
				SalaryCap:           snapshot.League.SalaryCap,
				ActiveRosterLimit:   snapshot.League.ActiveRosterLimit,
				InjuredReserveLimit: snapshot.League.InjuredReserveLimit,
				TaxiSquadLimit:      snapshot.League.TaxiSquadLimit,
			},
			Franchise: league.Franchise{
				ID:   league.FranchiseID(snapshot.Franchise.ID),
				Name: snapshot.Franchise.Name,
			},
			ObservedAt: observedAt,
			Source:     "mfl",
		},
	}

	for _, rostered := range snapshot.Roster {
		externalID := player.ExternalID{Provider: player.ProviderMFL, Value: rostered.ID}
		profile, err := resolver.ResolvePlayer(ctx, externalID)
		if err != nil {
			return NormalizedRecords{}, fmt.Errorf("resolve MFL player %s: %w", rostered.ID, err)
		}
		if profile.ID == "" {
			return NormalizedRecords{}, fmt.Errorf("resolve MFL player %s: canonical player ID is empty", rostered.ID)
		}
		if profile.DisplayName == "" {
			profile.DisplayName = rostered.Name
		}
		if profile.RookieYear == 0 {
			profile.RookieYear = rostered.RookieYear
		}
		if profile.BirthDate == nil {
			if unix, ok := snapshot.BirthdatesUnix[rostered.ID]; ok && unix > 0 {
				birthDate := time.Unix(unix, 0).UTC()
				profile.BirthDate = &birthDate
			}
		}
		records.Players = append(records.Players, profile)
		records.Aliases = append(records.Aliases, identity.Alias{
			ExternalID:       externalID,
			PlayerID:         profile.ID,
			Source:           "mfl",
			ResolutionMethod: "identity_repository",
			IngestedAt:       observedAt,
		})
		records.LeagueSnapshot.Roster = append(records.LeagueSnapshot.Roster, league.RosterAssignment{
			PlayerID:      profile.ID,
			Status:        normalizeRosterStatus(rostered.Status),
			Salary:        rostered.Salary,
			CurrentCapHit: rostered.CurrentCapHit,
		})
	}

	for _, pick := range snapshot.Draft.CurrentYearPicks {
		round, number := parsePickLabel(pick.Pick)
		records.LeagueSnapshot.DraftAssets = append(records.LeagueSnapshot.DraftAssets, league.DraftAsset{
			Season:             season,
			Round:              round,
			Pick:               number,
			Overall:            pick.Overall,
			CurrentFranchiseID: league.FranchiseID(snapshot.Franchise.ID),
			Salary:             pick.Salary,
		})
	}
	if err := records.LeagueSnapshot.Validate(); err != nil {
		return NormalizedRecords{}, err
	}
	return records, nil
}

func normalizeRosterStatus(status string) league.RosterStatus {
	switch status {
	case "ROSTER":
		return league.RosterActive
	case "INJURED_RESERVE":
		return league.RosterInjuredReserve
	case "TAXI_SQUAD":
		return league.RosterTaxi
	default:
		return league.RosterStatus(strings.ToLower(status))
	}
}

func parsePickLabel(label string) (int, int) {
	parts := strings.SplitN(label, ".", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	round, _ := strconv.Atoi(parts[0])
	pick, _ := strconv.Atoi(parts[1])
	return round, pick
}
