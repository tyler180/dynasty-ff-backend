package mflsync

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	source "github.com/tyler180/dynasty-ff-models/analysis"
)

type Options struct {
	Year         int
	LeagueID     string
	FranchiseID  string
	SnapshotDate time.Time
	Projections  *source.Projections
	IncludeDraft bool
	LiveDraft    bool
}

func Sync(ctx context.Context, caller Caller, base BaseDocument, options Options) (Result, error) {
	if caller == nil {
		return Result{}, fmt.Errorf("MCP caller is required")
	}
	if options.Year == 0 {
		year, _ := strconv.Atoi(base.Snapshot.SnapshotDate[:4])
		options.Year = year
	}
	if options.LeagueID == "" {
		options.LeagueID = base.Snapshot.League.ID
	}
	if options.FranchiseID == "" {
		options.FranchiseID = base.Snapshot.Franchise.ID
	}
	if options.SnapshotDate.IsZero() {
		options.SnapshotDate = time.Now()
	}
	if options.Year < 2000 || options.Year > 2100 || options.LeagueID == "" || options.FranchiseID == "" {
		return Result{}, fmt.Errorf("year, league ID, and franchise ID are required")
	}

	snapshot, err := cloneSnapshot(base.Snapshot)
	if err != nil {
		return Result{}, err
	}
	common := map[string]any{"year": options.Year, "league_id": options.LeagueID}
	var leaguePayload, rosterPayload, playersPayload map[string]any
	if err := caller.Call(ctx, "get_league", common, &leaguePayload); err != nil {
		return Result{}, err
	}
	if err := caller.Call(ctx, "get_rosters", map[string]any{
		"year": options.Year, "league_id": options.LeagueID, "franchise": options.FranchiseID,
	}, &rosterPayload); err != nil {
		return Result{}, err
	}
	if err := caller.Call(ctx, "get_players", map[string]any{"year": options.Year, "details": true}, &playersPayload); err != nil {
		return Result{}, err
	}

	catalog := parseCatalog(playersPayload)
	if len(catalog) == 0 {
		return Result{}, fmt.Errorf("get_players returned no player records")
	}
	league, franchiseName := normalizeLeague(leaguePayload, snapshot.League, options.FranchiseID)
	roster, err := normalizeRoster(rosterPayload, options.FranchiseID, catalog, snapshot.Roster, base.SalaryMultipliers)
	if err != nil {
		return Result{}, err
	}
	snapshot.SnapshotDate = options.SnapshotDate.Format("2006-01-02")
	snapshot.League = league
	snapshot.Franchise.ID = options.FranchiseID
	if franchiseName != "" {
		snapshot.Franchise.Name = franchiseName
	}
	snapshot.Roster = roster
	if snapshot.BirthdatesUnix == nil {
		snapshot.BirthdatesUnix = make(map[string]int64)
	}
	for _, player := range roster {
		if metadata := catalog[player.ID]; metadata.Birthdate > 0 {
			snapshot.BirthdatesUnix[player.ID] = metadata.Birthdate
		}
	}

	warnings := make([]string, 0)
	extra := make(map[string]any)
	if base.Bootstrapped && !base.HasSalaryMultipliers {
		warnings = append(warnings, "no league salary policy was supplied; assumed IR salaries count 50% and taxi salaries count 0% toward the cap")
	}
	if base.Bootstrapped && !base.HasRookieSalarySchedule {
		warnings = append(warnings, "MFL does not expose this league's custom rookie salary schedule; synchronized picks may have salary 0 until a schedule is supplied")
	}
	var rulesPayload, allRulesPayload map[string]any
	if callOptional(ctx, caller, "get_rules", common, &rulesPayload, &warnings) {
		extra["mfl_rules"] = rulesPayload
	}
	if callOptional(ctx, caller, "get_all_rules", map[string]any{"year": options.Year}, &allRulesPayload, &warnings) {
		extra["mfl_all_rules"] = allRulesPayload
	}
	var salaryPayload map[string]any
	adjustment := 0.0
	if callOptional(ctx, caller, "get_salary_adjustments", common, &salaryPayload, &warnings) {
		adjustment = salaryAdjustment(salaryPayload, options.FranchiseID)
	}
	snapshot.Franchise = summarizeFranchise(snapshot.Franchise, snapshot.League, roster, adjustment)

	playerIDs := make([]string, 0, len(roster))
	for _, player := range roster {
		playerIDs = append(playerIDs, player.ID)
	}
	var profilePayload map[string]any
	if callOptional(ctx, caller, "get_player_profiles", map[string]any{
		"year": options.Year, "player_ids": playerIDs,
	}, &profilePayload, &warnings) {
		for id, timestamp := range birthdates(profilePayload) {
			snapshot.BirthdatesUnix[id] = timestamp
		}
	}

	var freeAgentsPayload map[string]any
	haveFreeAgents := callOptional(ctx, caller, "get_free_agents", common, &freeAgentsPayload, &warnings)
	if len(snapshot.HistoricalPoints.Seasons) == 0 && len(snapshot.HistoricalPoints.ByPlayerID) == 0 {
		history, replacement := loadMFLHistory(ctx, caller, options.Year, options.LeagueID, catalog, freeAgentsPayload, &warnings)
		if len(history.Seasons) > 0 {
			snapshot.HistoricalPoints = history
			snapshot.ReplacementLevels = replacement
		}
	}
	draftedPlayerIDs := make(map[string]bool)

	franchiseNames := franchiseNameMap(leaguePayload)
	if options.IncludeDraft {
		var assetsPayload map[string]any
		if callOptional(ctx, caller, "get_assets", common, &assetsPayload, &warnings) {
			fallbackTeamCount := 0
			if leagueRaw, ok := object(base.Raw["league"]); ok {
				fallbackTeamCount = intField(leagueRaw, "team_count")
			}
			if picks, ok := ownedCurrentPicks(assetsPayload, options.FranchiseID, base, franchiseNames, teamCount(leaguePayload, fallbackTeamCount)); ok {
				snapshot.Draft.CurrentYearPicks = picks
			}
		}
		var futurePayload map[string]any
		if callOptional(ctx, caller, "get_future_draft_picks", common, &futurePayload, &warnings) {
			if inventory, ok := futurePickInventory(futurePayload, options.FranchiseID, franchiseNames); ok {
				extra["draft"] = map[string]any{"future_pick_inventory": inventory}
			}
		}
		var draftPayload map[string]any
		if callOptional(ctx, caller, "get_draft_results", common, &draftPayload, &warnings) {
			if picks, ok := ownedPicksFromDraftResults(draftPayload, options.FranchiseID, base, franchiseNames); ok {
				if len(picks) > 0 {
					snapshot.Draft.CurrentYearPicks = picks
				} else {
					warnings = append(warnings, "get_draft_results shows no remaining picks for the selected franchise; preserved any configured base picks")
				}
			}
			snapshot.Draft.Status, snapshot.Draft.StatusMessage = draftStatus(draftPayload, snapshot.Draft.Status)
		}
		if options.LiveDraft {
			var livePayload map[string]any
			if callOptional(ctx, caller, "get_live_draft_results", common, &livePayload, &warnings) {
				live, err := parseLiveDraft(livePayload)
				if err != nil {
					warnings = append(warnings, "get_live_draft_results could not be normalized: "+err.Error())
				} else {
					if live.Status != "" {
						snapshot.Draft.Status = live.Status
					}
					if live.Message != "" {
						snapshot.Draft.StatusMessage = live.Message
					}
					for _, id := range live.DraftedPlayerIDs {
						draftedPlayerIDs[id] = true
					}
					if len(live.CompletedPicks) > 0 {
						completed := make(map[string]bool, len(live.CompletedPicks))
						for _, pick := range live.CompletedPicks {
							completed[pick] = true
						}
						remaining := snapshot.Draft.CurrentYearPicks[:0]
						for _, pick := range snapshot.Draft.CurrentYearPicks {
							if !completed[pick.Pick] {
								remaining = append(remaining, pick)
							}
						}
						if len(remaining) > 0 {
							snapshot.Draft.CurrentYearPicks = remaining
						}
					}
					extra["draft"] = mergeExtraObject(extra["draft"], map[string]any{"live": live})
				}
			}
		}
	}
	if haveFreeAgents {
		snapshot.RookieCandidates = availableRookies(freeAgentsPayload, catalog, options.Year, draftedPlayerIDs)
		extra["available_rookie_pool"] = availableRookieSummary(snapshot.RookieCandidates)
	}

	if options.Projections != nil {
		snapshot.Projections = *options.Projections
	}
	snapshot.SourceReconciliation = append(snapshot.SourceReconciliation,
		fmt.Sprintf("dynasty-sync refreshed MFL-authoritative league, roster, player, salary, and draft data on %s.", snapshot.SnapshotDate))
	for _, warning := range warnings {
		snapshot.SourceReconciliation = append(snapshot.SourceReconciliation, "Sync warning: "+warning)
	}
	if err := Validate(snapshot); err != nil {
		return Result{}, err
	}
	return Result{Snapshot: snapshot, Extra: extra, Warnings: warnings, SyncedAt: options.SnapshotDate}, nil
}

func mergeExtraObject(existing any, overlay map[string]any) map[string]any {
	result, ok := object(existing)
	if !ok {
		result = make(map[string]any)
	}
	deepMerge(result, overlay)
	return result
}

func callOptional(ctx context.Context, caller Caller, tool string, arguments any, destination any, warnings *[]string) bool {
	if err := caller.Call(ctx, tool, arguments, destination); err != nil {
		*warnings = append(*warnings, fmt.Sprintf("%s unavailable: %v", tool, err))
		return false
	}
	return true
}

func cloneSnapshot(snapshot source.Snapshot) (source.Snapshot, error) {
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return source.Snapshot{}, fmt.Errorf("clone base snapshot: %w", err)
	}
	var cloned source.Snapshot
	if err := json.Unmarshal(payload, &cloned); err != nil {
		return source.Snapshot{}, fmt.Errorf("clone base snapshot: %w", err)
	}
	return cloned, nil
}
