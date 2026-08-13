// Package identitysync imports a provider crosswalk into canonical identities.
package identitysync

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"sync"
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
	SourcePlayers         int      `json:"source_players"`
	MFLPlayers            int      `json:"mfl_players"`
	EligiblePlayers       int      `json:"eligible_players"`
	UnmatchedMFLPlayers   int      `json:"unmatched_mfl_players"`
	UnmatchedMFLPlayerIDs []string `json:"unmatched_mfl_player_ids,omitempty"`
	ExistingPlayers       int      `json:"existing_players"`
	CreatedPlayers        int      `json:"created_players"`
	WrittenProfiles       int      `json:"written_profiles"`
	WrittenAliases        int      `json:"written_aliases"`
	ExistingAliases       int      `json:"existing_aliases"`
}

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
	result := Result{SourcePlayers: len(sourcePlayers)}
	relevantMFLIDs, err := s.RelevantPlayers.PlayerIDs(ctx, request.Year, request.LeagueID)
	if err != nil {
		return Result{}, err
	}
	var records []dynastyprocess.Player
	allExternalIDs := make([]player.ExternalID, 0, len(relevantMFLIDs))
	for mflID := range relevantMFLIDs {
		allExternalIDs = append(allExternalIDs, player.ExternalID{Provider: player.ProviderMFL, Value: mflID})
	}
	matchedMFLIDs := make(map[string]struct{}, len(relevantMFLIDs))
	for _, record := range sourcePlayers {
		if _, relevant := relevantMFLIDs[record.MFLID]; !relevant || record.MFLID == "" || strings.TrimSpace(record.Name) == "" {
			continue
		}
		records = append(records, record)
		matchedMFLIDs[record.MFLID] = struct{}{}
		allExternalIDs = append(allExternalIDs, aliases(record)...)
	}
	result.MFLPlayers = len(relevantMFLIDs)
	result.EligiblePlayers = len(records)
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
		externalIDs := aliases(record)
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
	if err := run(ctx, request.Workers, profileTasks); err != nil {
		return Result{}, err
	}
	result.WrittenProfiles = len(profileTasks)

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
	if err := run(ctx, request.Workers, aliasTasks); err != nil {
		return Result{}, err
	}
	result.WrittenAliases = len(aliasTasks)
	return result, nil
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
	anchor := "mfl:" + record.MFLID
	if record.GSISID != "" {
		anchor = "gsis:" + record.GSISID
	}
	digest := sha256.Sum256([]byte("dynasty-ff-player-v1:" + anchor))
	digest[6] = (digest[6] & 0x0f) | 0x50
	digest[8] = (digest[8] & 0x3f) | 0x80
	return player.ID(fmt.Sprintf("player-%x-%x-%x-%x-%x", digest[0:4], digest[4:6], digest[6:8], digest[8:10], digest[10:16]))
}

func run(ctx context.Context, workers int, tasks []func(context.Context) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan func(context.Context) error)
	var group sync.WaitGroup
	var firstErr error
	var once sync.Once
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for task := range jobs {
				if err := task(ctx); err != nil {
					once.Do(func() { firstErr = err; cancel() })
				}
			}
		}()
	}
	for _, task := range tasks {
		select {
		case jobs <- task:
		case <-ctx.Done():
			close(jobs)
			group.Wait()
			if firstErr != nil {
				return firstErr
			}
			return ctx.Err()
		}
	}
	close(jobs)
	group.Wait()
	return firstErr
}
