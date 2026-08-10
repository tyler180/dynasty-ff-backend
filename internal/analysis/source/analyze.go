package source

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

func Analyze(snapshot Snapshot) Analysis {
	return AnalyzeWithOptions(snapshot, AnalysisOptions{})
}

func AnalyzeWithOptions(snapshot Snapshot, options AnalysisOptions) Analysis {
	year, _ := strconv.Atoi(snapshot.SnapshotDate[:4])
	active := playersWithStatus(snapshot.Roster, "ROSTER")
	ir := playersWithStatus(snapshot.Roster, "INJURED_RESERVE")
	taxi := playersWithStatus(snapshot.Roster, "TAXI_SQUAD")
	capSpace := snapshot.League.SalaryCap - snapshot.Franchise.TotalCapHit

	analysis := Analysis{
		SnapshotDate: snapshot.SnapshotDate,
		League:       snapshot.League.Name,
		Team:         snapshot.Franchise.Name,
		Cap: CapSummary{
			Used:  round(snapshot.Franchise.TotalCapHit, 2),
			Limit: round(snapshot.League.SalaryCap, 2),
			Space: round(capSpace, 2),
		},
		Roster: RosterSummary{
			Active:         slotSummary(len(active), snapshot.League.ActiveRosterLimit),
			InjuredReserve: slotSummary(len(ir), snapshot.League.InjuredReserveLimit),
			Taxi:           slotSummary(len(taxi), snapshot.League.TaxiSquadLimit),
		},
		Draft: DraftSummary{
			Status:             snapshot.Draft.Status,
			AvailabilityWindow: snapshot.Draft.AvailabilityPollWindow,
			PickCount:          len(snapshot.Draft.CurrentYearPicks),
		},
		ComplianceScenarios: []ComplianceScenario{},
		Warnings:            []string{},
	}
	analysis.TaxiCompliance.MustLeaveTaxi = []PlayerSummary{}
	analysis.TaxiCompliance.UnknownEligibility = []PlayerSummary{}

	for _, pick := range snapshot.Draft.CurrentYearPicks {
		shortfall := math.Max(0, pick.Salary-capSpace)
		analysis.Draft.Picks = append(analysis.Draft.Picks, PickAssessment{
			Pick:               pick.Pick,
			Overall:            pick.Overall,
			Salary:             pick.Salary,
			FitsActiveNow:      len(active) < snapshot.League.ActiveRosterLimit && shortfall == 0,
			ActiveCapShortfall: round(shortfall, 2),
			FitsTaxiNow:        len(taxi) < snapshot.League.TaxiSquadLimit,
		})
		analysis.Draft.TotalSalaryIfActive += pick.Salary
	}
	analysis.Draft.TotalSalaryIfActive = round(analysis.Draft.TotalSalaryIfActive, 2)

	for _, player := range taxi {
		summary := summarizePlayer(player)
		if player.RookieYear == 0 {
			analysis.TaxiCompliance.UnknownEligibility = append(analysis.TaxiCompliance.UnknownEligibility, summary)
			continue
		}
		if year-player.RookieYear >= 2 {
			analysis.TaxiCompliance.MustLeaveTaxi = append(analysis.TaxiCompliance.MustLeaveTaxi, summary)
		}
	}
	analysis.TaxiCompliance.CurrentYear = year
	analysis.TaxiCompliance.SlotsOpenedAfterMove = len(analysis.TaxiCompliance.MustLeaveTaxi)
	sort.Slice(analysis.TaxiCompliance.MustLeaveTaxi, func(i, j int) bool {
		return analysis.TaxiCompliance.MustLeaveTaxi[i].Salary > analysis.TaxiCompliance.MustLeaveTaxi[j].Salary
	})

	analysis.ComplianceScenarios = complianceScenarios(snapshot, active, taxi, analysis.TaxiCompliance.MustLeaveTaxi)
	analysis.HistoricalEfficiency = historicalEfficiency(snapshot, active)
	analysis.DropEvaluation = evaluateDrops(snapshot, options)
	analysis.Warnings = []string{
		"No rookie or multi-year veteran projections are present, so this report cannot rank draft selections.",
		"Historical fallback uses up to four prior seasons; replacement value and early-career protection improve cut analysis, but explicit dynasty market values are still absent.",
		"The rookie draft is voluntary; total pick salary is an upper bound only if every pick is used on an active player.",
		"Roster status and player eligibility must be refreshed again when the draft is scheduled.",
	}
	for _, note := range snapshot.SourceReconciliation {
		if strings.HasPrefix(note, "Sync warning: ") {
			analysis.Warnings = append(analysis.Warnings, strings.TrimPrefix(note, "Sync warning: "))
		}
	}
	return analysis
}

func evaluateDrops(snapshot Snapshot, options AnalysisOptions) DropEvaluation {
	evaluation := DropEvaluation{
		CapReliefTarget:  round(math.Max(0, options.CapReliefTarget), 2),
		Candidates:       []DropCandidate{},
		MissingPlayerIDs: []string{},
		Method:           "Classify players before ranking: protect early-career assets, trade players clearly above replacement, then rank actual drop candidates by cap relief per marginal dynasty value lost.",
		Caution:          "Replacement value and a conservative development guard are modeled, but external dynasty market value, future role projections, and trade liquidity are not yet available.",
	}
	points := snapshot.Projections.ByPlayerID
	historyByPlayer := map[string]historicalValue{}
	evaluation.ProjectionSeason = snapshot.Projections.Season
	evaluation.ProjectionSource = snapshot.Projections.Source
	evaluation.ProductionMetric = "projected season points"
	if len(points) == 0 && options.ProjectionFallback == "historical" {
		historyByPlayer, _ = weightedHistorical(snapshot.HistoricalPoints)
		points = make(map[string]float64, len(historyByPlayer))
		allSeasons := historicalSeasonList(historyByPlayer)
		for id, value := range historyByPlayer {
			points[id] = value.PointsPerGame
		}
		evaluation.ProjectionSeason = 0
		evaluation.ProjectionSource = fmt.Sprintf("%s recency-weighted historical points-per-game fallback", formatSeasons(allSeasons))
		evaluation.ProductionMetric = "historical points per game"
		evaluation.ReplacementSource = snapshot.ReplacementLevels.Source
		evaluation.Caution = "Historical PPG fallback is enabled for exploratory use. Current free-agent replacement levels and early-career protection are included, but external dynasty market values and future role projections are not."
	}
	if len(points) == 0 {
		evaluation.ProjectionSource = "unavailable"
		for _, player := range snapshot.Roster {
			if capRelief(player) > 0 {
				evaluation.MissingPlayerIDs = append(evaluation.MissingPlayerIDs, player.ID)
			}
		}
		return evaluation
	}

	snapshotDate, _ := time.Parse("2006-01-02", snapshot.SnapshotDate)
	for _, player := range snapshot.Roster {
		relief := capRelief(player)
		if relief <= 0 {
			continue
		}
		projected, ok := points[player.ID]
		birthdate, hasBirthdate := snapshot.BirthdatesUnix[player.ID]
		if !ok || !hasBirthdate {
			evaluation.MissingPlayerIDs = append(evaluation.MissingPlayerIDs, player.ID)
			continue
		}
		age := ageOn(time.Unix(birthdate, 0).UTC(), snapshotDate)
		factor := ageFactor(player.Position, age)
		development := 1.0
		adjusted := math.Max(1, projected*factor)
		candidate := DropCandidate{
			PlayerID:              player.ID,
			Name:                  player.Name,
			Position:              player.Position,
			Age:                   age,
			SalaryCapRelief:       round(relief, 2),
			ProductionValue:       round(projected, 2),
			AgeFactor:             round(factor, 3),
			DevelopmentFactor:     1,
			AgeAdjustedProduction: round(adjusted, 2),
			DropScore:             round(100*relief/adjusted, 3),
			Disposition:           "cap_efficiency_only",
			DispositionReason:     "No compatible replacement-level input is available for this production metric.",
		}
		if historical, ok := historyByPlayer[player.ID]; ok {
			candidate.ProductionSeasons = append([]int(nil), historical.Seasons...)
			candidate.HistoricalGames = historical.Games
			candidate.CareerSeasons = len(historical.Seasons)
			if replacement, exists := snapshot.ReplacementLevels.PointsPerGameByPosition[player.Position]; exists {
				development = developmentFactor(age, candidate.CareerSeasons)
				vorp := projected - replacement
				adjustedVORP := math.Max(0, vorp) * factor * development
				candidate.DevelopmentFactor = round(development, 3)
				candidate.ReplacementPointsPerGame = round(replacement, 2)
				candidate.ValueOverReplacement = round(vorp, 2)
				candidate.DynastyAdjustedVORP = round(adjustedVORP, 2)
				candidate.DropScore = round(100*relief/(1+adjustedVORP), 3)
				candidate.Disposition, candidate.DispositionReason = disposition(candidate, vorp)
			}
		}
		evaluation.Candidates = append(evaluation.Candidates, candidate)
	}
	sort.Strings(evaluation.MissingPlayerIDs)
	sort.Slice(evaluation.Candidates, func(i, j int) bool {
		leftPriority := dispositionPriority(evaluation.Candidates[i].Disposition)
		rightPriority := dispositionPriority(evaluation.Candidates[j].Disposition)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		if evaluation.Candidates[i].DropScore != evaluation.Candidates[j].DropScore {
			return evaluation.Candidates[i].DropScore > evaluation.Candidates[j].DropScore
		}
		if evaluation.Candidates[i].ValueOverReplacement != evaluation.Candidates[j].ValueOverReplacement {
			return evaluation.Candidates[i].ValueOverReplacement < evaluation.Candidates[j].ValueOverReplacement
		}
		if evaluation.Candidates[i].SalaryCapRelief != evaluation.Candidates[j].SalaryCapRelief {
			return evaluation.Candidates[i].SalaryCapRelief > evaluation.Candidates[j].SalaryCapRelief
		}
		return evaluation.Candidates[i].Name < evaluation.Candidates[j].Name
	})
	for i := range evaluation.Candidates {
		evaluation.Candidates[i].Rank = i + 1
		switch evaluation.Candidates[i].Disposition {
		case "drop_candidate", "cap_efficiency_only":
			evaluation.DropCandidates = append(evaluation.DropCandidates, evaluation.Candidates[i])
		case "trade_first":
			evaluation.TradeFirst = append(evaluation.TradeFirst, evaluation.Candidates[i])
		case "hold_develop":
			evaluation.HoldDevelop = append(evaluation.HoldDevelop, evaluation.Candidates[i])
		}
	}
	evaluation.Available = len(evaluation.Candidates) > 0
	for i := range evaluation.DropCandidates {
		if evaluation.DropCandidates[i].SalaryCapRelief >= evaluation.CapReliefTarget {
			best := evaluation.DropCandidates[i]
			evaluation.BestForTarget = &best
			break
		}
	}
	return evaluation
}

func developmentFactor(age, careerSeasons int) float64 {
	if careerSeasons <= 2 {
		switch {
		case age <= 23:
			return 1.75
		case age <= 26:
			return 1.50
		}
	}
	if careerSeasons <= 3 && age <= 26 {
		return 1.25
	}
	return 1
}

func disposition(candidate DropCandidate, vorp float64) (string, string) {
	if candidate.CareerSeasons <= 2 && candidate.Age <= 26 {
		return "hold_develop", "Early-career player is protected because historical production does not capture development or market value reliably."
	}
	if vorp >= 1 {
		return "trade_first", "Produces at least 1.0 PPG above the current positional replacement level."
	}
	return "drop_candidate", "At or near the current positional replacement level and not covered by the early-career protection rule."
}

func dispositionPriority(value string) int {
	switch value {
	case "drop_candidate":
		return 0
	case "cap_efficiency_only":
		return 1
	case "trade_first":
		return 2
	case "hold_develop":
		return 3
	default:
		return 4
	}
}

func capRelief(player Player) float64 {
	switch player.Status {
	case "ROSTER":
		return player.Salary
	case "INJURED_RESERVE":
		if player.CurrentCapHit > 0 {
			return player.CurrentCapHit
		}
		return player.Salary * 0.5
	default:
		return 0
	}
}

func ageOn(birthdate, date time.Time) int {
	age := date.Year() - birthdate.Year()
	if date.Month() < birthdate.Month() || (date.Month() == birthdate.Month() && date.Day() < birthdate.Day()) {
		age--
	}
	return age
}

func ageFactor(position string, age int) float64 {
	peak := 29
	switch position {
	case "QB":
		peak = 35
	case "RB":
		peak = 26
	case "WR":
		peak = 28
	case "TE":
		peak = 29
	}
	if age < peak {
		return 1 + math.Min(0.24, float64(peak-age)*0.03)
	}
	return 1 - math.Min(0.50, float64(age-peak)*0.08)
}

func complianceScenarios(snapshot Snapshot, active, taxi []Player, mustLeave []PlayerSummary) []ComplianceScenario {
	if len(mustLeave) == 0 {
		return nil
	}
	playerByID := make(map[string]Player, len(taxi))
	for _, player := range taxi {
		playerByID[player.ID] = player
	}
	stashedCount := min(len(mustLeave), len(snapshot.Draft.CurrentYearPicks))
	stashedPicks := make([]string, 0, stashedCount)
	for i := 0; i < stashedCount; i++ {
		stashedPicks = append(stashedPicks, snapshot.Draft.CurrentYearPicks[i].Pick)
	}

	scenarios := make([]ComplianceScenario, 0, 1<<len(mustLeave))
	for mask := 0; mask < 1<<len(mustLeave); mask++ {
		scenario := ComplianceScenario{
			Promote:          []PlayerSummary{},
			RemoveOrTrade:    []PlayerSummary{},
			FirstPicksToTaxi: append([]string(nil), stashedPicks...),
		}
		promotedSalary := 0.0
		for i, player := range mustLeave {
			if mask&(1<<i) != 0 {
				scenario.Promote = append(scenario.Promote, player)
				promotedSalary += playerByID[player.ID].Salary
			} else {
				scenario.RemoveOrTrade = append(scenario.RemoveOrTrade, player)
			}
		}
		scenario.ResultingActive = len(active) + len(scenario.Promote)
		scenario.ResultingTaxi = len(taxi) - len(mustLeave) + stashedCount
		scenario.ResultingCapHit = round(snapshot.Franchise.TotalCapHit+promotedSalary, 2)
		scenario.AdditionalCapRelief = round(math.Max(0, scenario.ResultingCapHit-snapshot.League.SalaryCap), 2)
		scenario.RosterLegal = scenario.ResultingActive <= snapshot.League.ActiveRosterLimit &&
			scenario.ResultingTaxi <= snapshot.League.TaxiSquadLimit && scenario.AdditionalCapRelief == 0
		scenario.Name = scenarioName(scenario)
		scenarios = append(scenarios, scenario)
	}

	sort.Slice(scenarios, func(i, j int) bool {
		if len(scenarios[i].RemoveOrTrade) != len(scenarios[j].RemoveOrTrade) {
			return len(scenarios[i].RemoveOrTrade) < len(scenarios[j].RemoveOrTrade)
		}
		if scenarios[i].AdditionalCapRelief != scenarios[j].AdditionalCapRelief {
			return scenarios[i].AdditionalCapRelief < scenarios[j].AdditionalCapRelief
		}
		return scenarios[i].Name < scenarios[j].Name
	})
	return scenarios
}

func scenarioName(scenario ComplianceScenario) string {
	return fmt.Sprintf("promote %d, remove/trade %d", len(scenario.Promote), len(scenario.RemoveOrTrade))
}

func historicalEfficiency(snapshot Snapshot, active []Player) HistoricalEfficiency {
	values, seasons := weightedHistorical(snapshot.HistoricalPoints)
	efficiency := HistoricalEfficiency{
		Seasons: seasons,
		Method:  "game-weighted PPG with 4/3/2/1 season recency weights over up to four seasons",
		Caution: "Historical cap efficiency must not be interpreted as dynasty value or a cut ranking.",
	}
	players := make([]EfficiencyPlayer, 0, len(active))
	for _, player := range active {
		value, ok := values[player.ID]
		if !ok || player.Salary <= 0 {
			continue
		}
		players = append(players, EfficiencyPlayer{
			Name:                    player.Name,
			Position:                player.Position,
			Salary:                  player.Salary,
			HistoricalPointsPerGame: value.PointsPerGame,
			PointsPerGamePerSalary:  round(value.PointsPerGame/player.Salary, 2),
		})
	}
	sort.Slice(players, func(i, j int) bool {
		if players[i].PointsPerGamePerSalary != players[j].PointsPerGamePerSalary {
			return players[i].PointsPerGamePerSalary < players[j].PointsPerGamePerSalary
		}
		return players[i].Name < players[j].Name
	})
	limit := min(5, len(players))
	efficiency.LeastEfficient = append(efficiency.LeastEfficient, players[:limit]...)
	for i := len(players) - 1; i >= 0 && len(efficiency.MostEfficient) < limit; i-- {
		efficiency.MostEfficient = append(efficiency.MostEfficient, players[i])
	}
	return efficiency
}

type historicalValue struct {
	PointsPerGame float64
	Games         int
	Seasons       []int
}

func weightedHistorical(history HistoricalPoints) (map[string]historicalValue, []int) {
	series := append([]HistoricalSeason(nil), history.Seasons...)
	if len(series) == 0 && history.Season != 0 && len(history.ByPlayerID) > 0 {
		series = []HistoricalSeason{{Season: history.Season, ByPlayerID: history.ByPlayerID}}
	}
	sort.Slice(series, func(i, j int) bool { return series[i].Season > series[j].Season })
	if len(series) > 4 {
		series = series[:4]
	}
	weightedPoints := make(map[string]float64)
	weightedGames := make(map[string]float64)
	totalGames := make(map[string]int)
	used := make(map[string][]int)
	seasons := make([]int, 0, len(series))
	for index, season := range series {
		weight := float64(4 - index)
		seasons = append(seasons, season.Season)
		for id, points := range season.ByPlayerID {
			games, ok := season.GamesPlayedByPlayerID[id]
			if !ok || games <= 0 {
				continue
			}
			weightedPoints[id] += points * weight
			weightedGames[id] += float64(games) * weight
			totalGames[id] += games
			used[id] = append(used[id], season.Season)
		}
	}
	result := make(map[string]historicalValue, len(weightedPoints))
	for id, points := range weightedPoints {
		result[id] = historicalValue{
			PointsPerGame: round(points/weightedGames[id], 2),
			Games:         totalGames[id],
			Seasons:       used[id],
		}
	}
	return result, seasons
}

func historicalSeasonList(values map[string]historicalValue) []int {
	seen := make(map[int]bool)
	for _, value := range values {
		for _, season := range value.Seasons {
			seen[season] = true
		}
	}
	seasons := make([]int, 0, len(seen))
	for season := range seen {
		seasons = append(seasons, season)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(seasons)))
	return seasons
}

func formatSeasons(seasons []int) string {
	if len(seasons) == 0 {
		return "no-season"
	}
	if len(seasons) == 1 {
		return strconv.Itoa(seasons[0])
	}
	return fmt.Sprintf("%d-%d", seasons[len(seasons)-1], seasons[0])
}

func playersWithStatus(players []Player, status string) []Player {
	result := make([]Player, 0)
	for _, player := range players {
		if player.Status == status {
			result = append(result, player)
		}
	}
	return result
}

func slotSummary(used, limit int) SlotSummary {
	return SlotSummary{Used: used, Limit: limit, Open: max(0, limit-used)}
}

func summarizePlayer(player Player) PlayerSummary {
	return PlayerSummary{ID: player.ID, Name: player.Name, Position: player.Position, Salary: player.Salary, RookieYear: player.RookieYear}
}

func round(value float64, places int) float64 {
	scale := math.Pow10(places)
	return math.Round(value*scale) / scale
}
