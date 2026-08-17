// Package mflingest orchestrates read-only MFL acquisition, canonical identity
// resolution, and normalized snapshot persistence.
package mflingest

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/app/draftanalysis"
	"github.com/tyler180/dynasty-ff-backend/internal/domain/league"
	"github.com/tyler180/dynasty-ff-backend/internal/identity"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
	"github.com/tyler180/dynasty-ff-backend/internal/provider/fantasypros"
	mflsync "github.com/tyler180/dynasty-ff-backend/internal/provider/mfl"
	"github.com/tyler180/dynasty-ff-backend/internal/storage/leaguestore"
	source "github.com/tyler180/dynasty-ff-models/analysis"
)

type Credentials interface {
	Environment(context.Context) (map[string]string, error)
}

type Evaluations interface {
	Evaluations(context.Context, int) ([]fantasypros.Evaluation, error)
}

type Request struct {
	Year             int
	LeagueID         string
	FranchiseID      string
	LeagueConfigPath string
	IncludeDraft     bool
	LiveDraft        bool
	SkipEvaluations  bool
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
	Evaluations Evaluations
	Now         func() time.Time
}

func (s Service) Sync(ctx context.Context, request Request) (Result, error) {
	if s.MCPCommand == "" {
		return Result{}, fmt.Errorf("MFL MCP command is required")
	}
	if s.Credentials == nil || s.Identities == nil || s.Snapshots == nil || (!request.SkipEvaluations && s.Evaluations == nil) {
		return Result{}, fmt.Errorf("MFL credentials, identity repository, snapshot repository, and player evaluations are required")
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
	identityWarnings, err := ensureMFLIdentities(ctx, &loaded.Snapshot, loaded.Refresh.SyncedAt, s.Identities)
	if err != nil {
		return Result{}, err
	}
	evaluationWarnings := []string{}
	if request.SkipEvaluations {
		evaluationWarnings = append(evaluationWarnings, "FantasyPros enrichment was skipped; analysis will use MFL data and provider-independent safeguards")
	} else if warnings, evaluationErr := enrichEvaluations(ctx, &loaded.Snapshot, request.Year, loaded.Refresh.SyncedAt, s.Identities, s.Evaluations); evaluationErr != nil {
		evaluationWarnings = append(evaluationWarnings, fmt.Sprintf("FantasyPros enrichment unavailable (%v); continuing with MFL data and provider-independent safeguards", evaluationErr))
	} else {
		evaluationWarnings = append(evaluationWarnings, warnings...)
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
		Warnings: append(append(append([]string(nil), loaded.Refresh.Warnings...), identityWarnings...), evaluationWarnings...),
		SyncedAt: loaded.Refresh.SyncedAt,
	}, nil
}

type mflIdentityObservation struct {
	name       string
	rookieYear int
}

func ensureMFLIdentities(
	ctx context.Context,
	snapshot *source.Snapshot,
	observedAt time.Time,
	repository identity.Repository,
) ([]string, error) {
	bulk, ok := repository.(identity.BulkResolver)
	if !ok {
		return nil, fmt.Errorf("identity repository does not support batch resolution for MFL players")
	}
	observations := make(map[string]mflIdentityObservation)
	observe := func(id, name string, rookieYear int) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		current := observations[id]
		if current.name == "" && strings.TrimSpace(name) != "" {
			current.name = strings.TrimSpace(name)
		}
		if current.rookieYear == 0 && rookieYear != 0 {
			current.rookieYear = rookieYear
		}
		observations[id] = current
	}
	for _, rostered := range snapshot.Roster {
		observe(rostered.ID, rostered.Name, rostered.RookieYear)
	}
	for _, candidates := range snapshot.ReplacementLevels.CandidatesByPosition {
		for _, candidate := range candidates {
			observe(candidate.PlayerID, candidate.Name, candidate.RookieYear)
		}
	}
	for _, candidate := range snapshot.RookieCandidates {
		observe(candidate.ID, candidate.Name, candidate.RookieYear)
	}

	externalIDs := make([]player.ExternalID, 0, len(observations))
	for id := range observations {
		externalIDs = append(externalIDs, player.ExternalID{Provider: player.ProviderMFL, Value: id})
	}
	resolved, err := bulk.ResolvePlayers(ctx, externalIDs)
	if err != nil {
		return nil, fmt.Errorf("resolve MFL identities before snapshot normalization: %w", err)
	}
	missing := make([]string, 0)
	for id := range observations {
		externalID := player.ExternalID{Provider: player.ProviderMFL, Value: id}
		if _, found := resolved[externalID]; !found {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	for _, id := range missing {
		externalID := player.ExternalID{Provider: player.ProviderMFL, Value: id}
		canonicalID, err := player.DeterministicID(externalID)
		if err != nil {
			return nil, err
		}
		observation := observations[id]
		name := observation.name
		if name == "" {
			name = "MFL player " + id
		}
		profile := player.Profile{ID: canonicalID, DisplayName: name, RookieYear: observation.rookieYear}
		if timestamp := snapshot.BirthdatesUnix[id]; timestamp > 0 {
			birthDate := time.Unix(timestamp, 0).UTC()
			profile.BirthDate = &birthDate
		}
		if err := repository.PutPlayer(ctx, profile); err != nil {
			return nil, fmt.Errorf("bootstrap MFL player %s: %w", id, err)
		}
		if err := repository.PutAlias(ctx, identity.Alias{
			ExternalID: externalID, PlayerID: canonicalID, Source: "mfl",
			ResolutionMethod: "provider_native_bootstrap", Confidence: 1, IngestedAt: observedAt.UTC(),
		}); err != nil {
			return nil, fmt.Errorf("bootstrap MFL alias %s: %w", id, err)
		}
	}
	if len(missing) == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("bootstrapped %d newly observed MFL player identities: %s", len(missing), strings.Join(missing, ", "))}, nil
}

func enrichEvaluations(
	ctx context.Context,
	snapshot *source.Snapshot,
	season int,
	observedAt time.Time,
	identities identity.Repository,
	provider Evaluations,
) ([]string, error) {
	values, err := provider.Evaluations(ctx, season)
	if err != nil {
		return nil, err
	}
	bulk, ok := identities.(identity.BulkResolver)
	if !ok {
		return nil, fmt.Errorf("identity repository does not support batch resolution for FantasyPros values")
	}
	externalIDs := make([]player.ExternalID, 0, len(values))
	byExternalID := make(map[player.ExternalID]fantasypros.Evaluation, len(values))
	for _, value := range values {
		if value.FantasyProsID == "" {
			continue
		}
		externalID := player.ExternalID{Provider: player.ProviderFantasyPros, Value: value.FantasyProsID}
		externalIDs = append(externalIDs, externalID)
		byExternalID[externalID] = value
	}
	resolved, err := bulk.ResolvePlayers(ctx, externalIDs)
	if err != nil {
		return nil, fmt.Errorf("resolve FantasyPros evaluations: %w", err)
	}
	byCanonicalID := make(map[player.ID]fantasypros.Evaluation, len(resolved))
	for externalID, profile := range resolved {
		byCanonicalID[profile.ID] = byExternalID[externalID]
	}
	const sourceName = "FantasyPros rookie/dynasty ECR and PPR preseason projections"
	for index := range snapshot.RookieCandidates {
		candidate := &snapshot.RookieCandidates[index]
		profile, err := identities.ResolvePlayer(ctx, player.ExternalID{Provider: player.ProviderMFL, Value: candidate.ID})
		if err != nil {
			return nil, fmt.Errorf("resolve available rookie MFL player %s: %w", candidate.ID, err)
		}
		value, found := byCanonicalID[profile.ID]
		if !found {
			continue
		}
		candidate.RookieRank = value.RookieRank
		candidate.DynastyRank = value.DynastyRank
		candidate.MarketValue = value.MarketValue
		candidate.ProjectedPoints = map[int]float64{season: value.ProjectedPoints}
		candidate.Source = appendObservationSource(candidate.Source, sourceName)
		candidate.UpdatedAt = observedAt.UTC().Format(time.RFC3339)
	}
	for position, candidates := range snapshot.ReplacementLevels.CandidatesByPosition {
		for index := range candidates {
			candidate := &candidates[index]
			profile, err := identities.ResolvePlayer(ctx, player.ExternalID{Provider: player.ProviderMFL, Value: candidate.PlayerID})
			if err != nil {
				return nil, fmt.Errorf("resolve replacement candidate MFL player %s: %w", candidate.PlayerID, err)
			}
			value, found := byCanonicalID[profile.ID]
			if !found {
				continue
			}
			candidate.DynastyRank = value.DynastyRank
			candidate.MarketValue = value.MarketValue
			candidate.ProjectedPoints = value.ProjectedPoints
			candidate.Source = appendObservationSource(candidate.Source, sourceName)
		}
		snapshot.ReplacementLevels.CandidatesByPosition[position] = candidates
	}
	if snapshot.Projections.ByPlayerID == nil {
		snapshot.Projections.ByPlayerID = map[string]float64{}
	}
	for index := range snapshot.Roster {
		rostered := &snapshot.Roster[index]
		profile, err := identities.ResolvePlayer(ctx, player.ExternalID{Provider: player.ProviderMFL, Value: rostered.ID})
		if err != nil {
			return nil, fmt.Errorf("resolve roster projection MFL player %s: %w", rostered.ID, err)
		}
		if value, found := byCanonicalID[profile.ID]; found {
			rostered.DynastyRank = value.DynastyRank
			rostered.MarketValue = value.MarketValue
			rostered.MarketSource = sourceName
			if value.ProjectedPoints > 0 {
				snapshot.Projections.ByPlayerID[rostered.ID] = value.ProjectedPoints
			}
		}
	}
	if len(snapshot.Projections.ByPlayerID) > 0 {
		snapshot.Projections.Season = season
		snapshot.Projections.Source = sourceName
	}
	warnings := make([]string, 0, 2)
	valued := 0
	for _, candidate := range snapshot.RookieCandidates {
		if candidate.MarketValue > 0 || candidate.RookieRank > 0 || candidate.RookieADP > 0 || candidate.ProjectedPoints[season] > 0 {
			valued++
		}
	}
	if valued < len(snapshot.RookieCandidates) {
		warnings = append(warnings, fmt.Sprintf("MFL rookie ADP and FantasyPros valued %d of %d currently available MFL rookies; unranked players remain visible but are not ranked", valued, len(snapshot.RookieCandidates)))
	}
	rosterValued := 0
	for _, rostered := range snapshot.Roster {
		if rostered.MarketValue > 0 || rostered.DynastyRank > 0 || snapshot.Projections.ByPlayerID[rostered.ID] > 0 {
			rosterValued++
		}
	}
	if rosterValued < len(snapshot.Roster) {
		warnings = append(warnings, fmt.Sprintf("FantasyPros valued %d of %d rostered players; missing dynasty values weaken cut protection and should be investigated", rosterValued, len(snapshot.Roster)))
	}
	return warnings, nil
}

func appendObservationSource(existing, added string) string {
	existing = strings.TrimSpace(existing)
	if existing == "" {
		return added
	}
	if strings.Contains(existing, added) {
		return existing
	}
	return existing + "; " + added
}
