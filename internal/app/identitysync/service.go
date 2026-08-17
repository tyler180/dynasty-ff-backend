// Package identitysync imports a provider crosswalk into canonical identities.
package identitysync

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/identity"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
	"github.com/tyler180/dynasty-ff-backend/internal/provider/dynastyprocess"
)

type Source interface {
	Players(context.Context) ([]dynastyprocess.Player, error)
}

type RelevantPlayers interface {
	PlayerIDs(context.Context, int, string) (map[string]struct{}, error)
}

type Request struct {
	Year     int
	LeagueID string
	Workers  int
}

type Result struct {
	Complete              bool             `json:"complete"`
	PartialReason         string           `json:"partial_reason,omitempty"`
	SourcePlayers         int              `json:"source_players"`
	MFLPlayers            int              `json:"mfl_players"`
	EligiblePlayers       int              `json:"eligible_players"`
	UnmatchedMFLPlayers   int              `json:"unmatched_mfl_players"`
	UnmatchedMFLPlayerIDs []string         `json:"unmatched_mfl_player_ids,omitempty"`
	ExistingPlayers       int              `json:"existing_players"`
	CreatedPlayers        int              `json:"created_players"`
	WrittenProfiles       int              `json:"written_profiles"`
	WrittenAliases        int              `json:"written_aliases"`
	ExistingAliases       int              `json:"existing_aliases"`
	AmbiguousAliases      []AmbiguousAlias `json:"ambiguous_aliases,omitempty"`
}

type AmbiguousAlias struct {
	Provider     player.Provider `json:"provider"`
	ExternalID   string          `json:"external_id"`
	MFLPlayerIDs []string        `json:"mfl_player_ids"`
	PlayerNames  []string        `json:"player_names"`
}

const deadlineReserve = 20 * time.Second

type Service struct {
	Source          Source
	Repository      identity.Repository
	BulkResolver    identity.BulkResolver
	RelevantPlayers RelevantPlayers
	Now             func() time.Time
}

type candidate struct {
	profile player.Profile
	aliases []player.ExternalID
}

func (s Service) Sync(ctx context.Context, request Request) (Result, error) {
	if s.Source == nil || s.Repository == nil || s.BulkResolver == nil || s.RelevantPlayers == nil {
		return Result{}, fmt.Errorf("identity source, MFL catalog, repository, and bulk resolver are required")
	}
	if request.Year < 2000 || request.Year > 2100 || strings.TrimSpace(request.LeagueID) == "" {
		return Result{}, fmt.Errorf("year and league ID are required")
	}
	if request.Workers == 0 {
		request.Workers = 16
	}
	if request.Workers < 1 || request.Workers > 64 {
		return Result{}, fmt.Errorf("workers must be between 1 and 64")
	}

	sourcePlayers, err := s.Source.Players(ctx)
	if err != nil {
		return Result{}, err
	}
	result := Result{Complete: true, SourcePlayers: len(sourcePlayers)}
	relevantMFLIDs, err := s.RelevantPlayers.PlayerIDs(ctx, request.Year, request.LeagueID)
	if err != nil {
		return Result{}, err
	}
	var records []dynastyprocess.Player
	matchedMFLIDs := make(map[string]struct{}, len(relevantMFLIDs))
	for _, record := range sourcePlayers {
		if _, relevant := relevantMFLIDs[record.MFLID]; !relevant || record.MFLID == "" || strings.TrimSpace(record.Name) == "" {
			continue
		}
		records = append(records, record)
		matchedMFLIDs[record.MFLID] = struct{}{}
	}
	result.MFLPlayers = len(relevantMFLIDs)
	result.EligiblePlayers = len(records)
	ambiguous := ambiguousAliases(records)
	result.AmbiguousAliases = describeAmbiguousAliases(ambiguous)
	allExternalIDs := make([]player.ExternalID, 0, len(relevantMFLIDs))
	for mflID := range relevantMFLIDs {
		allExternalIDs = append(allExternalIDs, player.ExternalID{Provider: player.ProviderMFL, Value: mflID})
	}
	for _, record := range records {
		for _, externalID := range aliases(record) {
			if _, skip := ambiguous[externalID]; !skip {
				allExternalIDs = append(allExternalIDs, externalID)
			}
		}
	}
	existing, err := s.BulkResolver.ResolvePlayers(ctx, allExternalIDs)
	if err != nil {
		return Result{}, err
	}
	for mflID := range relevantMFLIDs {
		if _, matched := matchedMFLIDs[mflID]; matched {
			continue
		}
		if _, alreadyResolved := existing[player.ExternalID{Provider: player.ProviderMFL, Value: mflID}]; alreadyResolved {
			continue
		}
		result.UnmatchedMFLPlayerIDs = append(result.UnmatchedMFLPlayerIDs, mflID)
	}
	sort.Strings(result.UnmatchedMFLPlayerIDs)
	result.UnmatchedMFLPlayers = len(result.UnmatchedMFLPlayerIDs)

	now := s.Now
	if now == nil {
		now = time.Now
	}
	ingestedAt := now().UTC()
	candidates := make([]candidate, 0, len(records))
	seenCanonical := make(map[player.ID]struct{})
	for _, record := range records {
		externalIDs := unambiguousAliases(record, ambiguous)
		var existingProfile *player.Profile
		for _, externalID := range externalIDs {
			profile, ok := existing[externalID]
			if !ok {
				continue
			}
			if existingProfile != nil && existingProfile.ID != profile.ID {
				return Result{}, fmt.Errorf("%w: source row %q resolves to both %s and %s", identity.ErrAliasConflict, record.Name, existingProfile.ID, profile.ID)
			}
			copy := profile
			existingProfile = &copy
		}
		profile := profileFromRecord(record, canonicalID(record))
		isExisting := existingProfile != nil
		if isExisting {
			profile = mergeProfile(*existingProfile, profile)
			result.ExistingPlayers++
		} else {
			result.CreatedPlayers++
		}
		if _, duplicate := seenCanonical[profile.ID]; duplicate {
			return Result{}, fmt.Errorf("duplicate canonical player %s in identity source", profile.ID)
		}
		seenCanonical[profile.ID] = struct{}{}
		candidates = append(candidates, candidate{profile: profile, aliases: externalIDs})
	}

	profileTasks := make([]func(context.Context) error, 0, len(candidates))
	for _, item := range candidates {
		profile := item.profile
		profileTasks = append(profileTasks, func(ctx context.Context) error { return s.Repository.PutPlayer(ctx, profile) })
	}
	completed, stopped, err := run(ctx, request.Workers, deadlineReserve, profileTasks)
	result.WrittenProfiles = completed
	if err != nil {
		return Result{}, err
	}
	if stopped {
		result.Complete = false
		result.PartialReason = "stopped before the Lambda deadline while writing player profiles; invoke sync_identities again to continue"
		return result, nil
	}

	var aliasTasks []func(context.Context) error
	for _, item := range candidates {
		for _, externalID := range item.aliases {
			if profile, ok := existing[externalID]; ok {
				if profile.ID != item.profile.ID {
					return Result{}, fmt.Errorf("%w: %s#%s", identity.ErrAliasConflict, externalID.Provider, externalID.Value)
				}
				result.ExistingAliases++
				continue
			}
			alias := identity.Alias{
				ExternalID: externalID, PlayerID: item.profile.ID, Source: "dynastyprocess",
				ResolutionMethod: "provider_crosswalk", Confidence: 1, IngestedAt: ingestedAt,
			}
			aliasTasks = append(aliasTasks, func(ctx context.Context) error { return s.Repository.PutAlias(ctx, alias) })
		}
	}
	completed, stopped, err = run(ctx, request.Workers, deadlineReserve, aliasTasks)
	result.WrittenAliases = completed
	if err != nil {
		return Result{}, err
	}
	if stopped {
		result.Complete = false
		result.PartialReason = "stopped before the Lambda deadline while writing player aliases; invoke sync_identities again to continue"
	}
	return result, nil
}

type aliasOccurrence struct {
	mflID string
	name  string
}

func ambiguousAliases(records []dynastyprocess.Player) map[player.ExternalID][]aliasOccurrence {
	occurrences := make(map[player.ExternalID][]aliasOccurrence)
	for _, record := range records {
		for _, externalID := range aliases(record) {
			occurrences[externalID] = append(occurrences[externalID], aliasOccurrence{mflID: record.MFLID, name: record.Name})
		}
	}
	ambiguous := make(map[player.ExternalID][]aliasOccurrence)
	for externalID, items := range occurrences {
		uniqueMFLIDs := make(map[string]struct{})
		for _, item := range items {
			uniqueMFLIDs[item.mflID] = struct{}{}
		}
		if len(uniqueMFLIDs) > 1 {
			ambiguous[externalID] = items
		}
	}
	return ambiguous
}

func describeAmbiguousAliases(ambiguous map[player.ExternalID][]aliasOccurrence) []AmbiguousAlias {
	result := make([]AmbiguousAlias, 0, len(ambiguous))
	for externalID, occurrences := range ambiguous {
		item := AmbiguousAlias{Provider: externalID.Provider, ExternalID: externalID.Value}
		seen := make(map[string]struct{})
		for _, occurrence := range occurrences {
			key := occurrence.mflID + "\x00" + occurrence.name
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			item.MFLPlayerIDs = append(item.MFLPlayerIDs, occurrence.mflID)
			item.PlayerNames = append(item.PlayerNames, occurrence.name)
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Provider == result[j].Provider {
			return result[i].ExternalID < result[j].ExternalID
		}
		return result[i].Provider < result[j].Provider
	})
	return result
}

func unambiguousAliases(record dynastyprocess.Player, ambiguous map[player.ExternalID][]aliasOccurrence) []player.ExternalID {
	result := make([]player.ExternalID, 0)
	for _, externalID := range aliases(record) {
		if _, skip := ambiguous[externalID]; !skip {
			result = append(result, externalID)
		}
	}
	return result
}

func aliases(record dynastyprocess.Player) []player.ExternalID {
	values := []struct {
		provider player.Provider
		value    string
	}{
		{player.ProviderMFL, record.MFLID}, {player.ProviderGSIS, record.GSISID}, {player.ProviderPFR, record.PFRID},
		{player.ProviderPFF, record.PFFID}, {player.ProviderESPN, record.ESPNID}, {player.ProviderSleeper, record.SleeperID},
		{player.ProviderFantasyPros, record.FantasyProsID}, {player.ProviderNFL, record.NFLID}, {player.ProviderYahoo, record.YahooID},
		{player.ProviderCBS, record.CBSID}, {player.ProviderFleaflicker, record.FleaflickerID}, {player.ProviderRotowire, record.RotowireID},
		{player.ProviderKTC, record.KTCID}, {player.ProviderFantasyData, record.FantasyDataID}, {player.ProviderSportradar, record.SportradarID},
		{player.ProviderCFBRef, record.CFBRefID},
	}
	aliases := make([]player.ExternalID, 0, len(values))
	for _, item := range values {
		if value := strings.TrimSpace(item.value); value != "" {
			aliases = append(aliases, player.ExternalID{Provider: item.provider, Value: value})
		}
	}
	return aliases
}

func profileFromRecord(record dynastyprocess.Player, id player.ID) player.Profile {
	profile := player.Profile{ID: id, DisplayName: strings.TrimSpace(record.Name), RookieYear: record.DraftYear}
	if record.BirthDate != "" {
		if birthDate, err := time.Parse("2006-01-02", record.BirthDate); err == nil {
			profile.BirthDate = &birthDate
		}
	}
	if record.DraftYear != 0 {
		profile.Draft = &player.DraftRecord{Year: record.DraftYear, Round: record.DraftRound, Pick: record.DraftPick}
	}
	return profile
}

func mergeProfile(existing, imported player.Profile) player.Profile {
	imported.ID = existing.ID
	if existing.DisplayName != "" {
		imported.DisplayName = existing.DisplayName
	}
	if existing.BirthDate != nil {
		imported.BirthDate = existing.BirthDate
	}
	if existing.RookieYear != 0 {
		imported.RookieYear = existing.RookieYear
	}
	if existing.Draft != nil {
		imported.Draft = existing.Draft
	}
	return imported
}

func canonicalID(record dynastyprocess.Player) player.ID {
	externalID := player.ExternalID{Provider: player.ProviderMFL, Value: record.MFLID}
	if record.GSISID != "" {
		externalID = player.ExternalID{Provider: player.ProviderGSIS, Value: record.GSISID}
	}
	id, _ := player.DeterministicID(externalID)
	return id
}

func run(ctx context.Context, workers int, reserve time.Duration, tasks []func(context.Context) error) (int, bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan func(context.Context) error)
	var group sync.WaitGroup
	var firstErr error
	var once sync.Once
	var completed atomic.Int64
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for task := range jobs {
				if err := task(ctx); err != nil {
					once.Do(func() { firstErr = err; cancel() })
				} else {
					completed.Add(1)
				}
			}
		}()
	}
	for _, task := range tasks {
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= reserve {
			close(jobs)
			group.Wait()
			return int(completed.Load()), true, firstErr
		}
		select {
		case jobs <- task:
		case <-ctx.Done():
			close(jobs)
			group.Wait()
			if firstErr != nil {
				return int(completed.Load()), false, firstErr
			}
			return int(completed.Load()), false, ctx.Err()
		}
	}
	close(jobs)
	group.Wait()
	return int(completed.Load()), false, firstErr
}
