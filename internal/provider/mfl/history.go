package mflsync

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	source "github.com/tyler180/dynasty-ff-models/analysis"
)

const minimumReplacementGames = 8

func loadMFLHistory(
	ctx context.Context,
	caller Caller,
	year int,
	leagueID string,
	catalog map[string]catalogPlayer,
	freeAgentsPayload map[string]any,
	minimumBid float64,
	warnings *[]string,
) (source.HistoricalPoints, source.ReplacementLevels) {
	history := source.HistoricalPoints{
		Source:  fmt.Sprintf("MFL league-scored playerScores YTD and AVG exports for %d-%d", year-4, year-1),
		Seasons: []source.HistoricalSeason{},
	}
	for season := year - 1; season >= year-4; season-- {
		arguments := map[string]any{"year": season, "league_id": leagueID}
		var totalPayload, averagePayload map[string]any
		arguments["week"] = "YTD"
		if !callOptional(ctx, caller, "get_player_scores", arguments, &totalPayload, warnings) {
			continue
		}
		arguments = map[string]any{"year": season, "league_id": leagueID, "week": "AVG"}
		if !callOptional(ctx, caller, "get_player_scores", arguments, &averagePayload, warnings) {
			continue
		}
		totals := playerScores(totalPayload)
		averages := playerScores(averagePayload)
		games := make(map[string]int)
		for id, total := range totals {
			average := averages[id]
			if average == 0 || total == 0 {
				continue
			}
			count := int(math.Round(math.Abs(total / average)))
			if count > 0 && count <= 21 {
				games[id] = count
			}
		}
		if len(totals) > 0 {
			history.Seasons = append(history.Seasons, source.HistoricalSeason{
				Season: season, ByPlayerID: totals, GamesPlayedByPlayerID: games,
			})
		}
	}

	bids := make([]winningBid, 0)
	for season := year - 1; season >= year-3; season-- {
		var transactionsPayload map[string]any
		if callOptional(ctx, caller, "get_transactions", map[string]any{
			"year": season, "league_id": leagueID, "types": "WAIVER", "count": 2000,
		}, &transactionsPayload, warnings) {
			bids = append(bids, winningBids(transactionsPayload)...)
		}
	}
	replacement := replacementLevels(history, catalog, freeAgentsPayload, bids, year, minimumBid)
	return history, replacement
}

func playerScores(payload map[string]any) map[string]float64 {
	result := make(map[string]float64)
	root := envelope(payload, "playerScores")
	for _, item := range values(root["playerScore"]) {
		entry, ok := object(item)
		if !ok {
			continue
		}
		id := textField(entry, "id")
		score, ok := numberField(entry, "score")
		if id != "" && ok {
			result[id] = score
		}
	}
	return result
}

func freeAgentIDs(payload map[string]any) map[string]bool {
	result := make(map[string]bool)
	walkObjects(envelope(payload, "freeAgents"), func(item map[string]any) {
		if id := textField(item, "id"); id != "" {
			result[id] = true
		}
	})
	return result
}

type weightedPlayer struct {
	PPG   float64
	Games int
}

type freeAgentRecord struct {
	Status       string
	ListedSalary float64
}

func freeAgentRecords(payload map[string]any) map[string]freeAgentRecord {
	result := make(map[string]freeAgentRecord)
	walkObjects(envelope(payload, "freeAgents"), func(item map[string]any) {
		if id := textField(item, "id"); id != "" {
			salary, _ := numberField(item, "salary")
			result[id] = freeAgentRecord{Status: textField(item, "status"), ListedSalary: salary}
		}
	})
	return result
}

type winningBid struct {
	PlayerID  string
	Franchise string
	Amount    float64
}

func winningBids(payload map[string]any) []winningBid {
	result := make([]winningBid, 0)
	root := envelope(payload, "transactions")
	for _, value := range values(root["transaction"]) {
		transaction, ok := object(value)
		if !ok || !strings.Contains(strings.ToUpper(textField(transaction, "type")), "WAIVER") {
			continue
		}
		parts := strings.Split(textField(transaction, "transaction"), "|")
		if len(parts) < 2 {
			continue
		}
		playerID := strings.Trim(strings.TrimSpace(parts[0]), ",")
		amount, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if playerID == "" || err != nil || amount < 0 {
			continue
		}
		result = append(result, winningBid{PlayerID: playerID, Franchise: textField(transaction, "franchise"), Amount: amount})
	}
	return result
}

type bidEstimate struct {
	Low, Expected, High     float64
	Observations, Bidders   int
	Confidence, Competition string
}

func bidEstimates(catalog map[string]catalogPlayer, bids []winningBid, minimumBid float64) map[string]bidEstimate {
	byPosition := make(map[string][]winningBid)
	for _, bid := range bids {
		if position := catalog[bid.PlayerID].Position; position != "" {
			byPosition[position] = append(byPosition[position], bid)
		}
	}
	result := make(map[string]bidEstimate)
	for position, observations := range byPosition {
		amounts := make([]float64, 0, len(observations))
		bidders := make(map[string]bool)
		for _, observation := range observations {
			amounts = append(amounts, math.Max(minimumBid, observation.Amount))
			if observation.Franchise != "" {
				bidders[observation.Franchise] = true
			}
		}
		sort.Float64s(amounts)
		estimate := bidEstimate{
			Low: quantile(amounts, 0.50), Expected: quantile(amounts, 0.75), High: quantile(amounts, 0.90),
			Observations: len(amounts), Bidders: len(bidders),
		}
		switch {
		case estimate.Observations >= 30:
			estimate.Confidence = "high"
		case estimate.Observations >= 10:
			estimate.Confidence = "medium"
		default:
			estimate.Confidence = "low"
		}
		switch {
		case estimate.Expected >= 6:
			estimate.Competition = "high"
		case estimate.Expected >= 3:
			estimate.Competition = "medium"
		default:
			estimate.Competition = "low"
		}
		result[position] = estimate
	}
	return result
}

func quantile(sorted []float64, fraction float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	index := int(math.Ceil(fraction*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	return round(sorted[index], 2)
}

func replacementLevels(history source.HistoricalPoints, catalog map[string]catalogPlayer, freeAgentsPayload map[string]any, bids []winningBid, year int, minimumBid float64) source.ReplacementLevels {
	values := weightedHistory(history)
	freeAgents := freeAgentRecords(freeAgentsPayload)
	estimates := bidEstimates(catalog, bids, minimumBid)
	byPosition := make(map[string][]source.ReplacementCandidate)
	for id, freeAgent := range freeAgents {
		metadata, exists := catalog[id]
		if !exists || metadata.Position == "" || metadata.NFLTeam == "" || metadata.NFLTeam == "FA" {
			continue
		}
		value := values[id]
		estimate := estimates[metadata.Position]
		if estimate.Observations == 0 {
			estimate.Low, estimate.Expected, estimate.High = minimumBid, minimumBid, minimumBid
			estimate.Confidence, estimate.Competition = "low", "unknown"
		}
		byPosition[metadata.Position] = append(byPosition[metadata.Position], source.ReplacementCandidate{
			PlayerID: id, Name: metadata.Name, Position: metadata.Position, NFLTeam: metadata.NFLTeam,
			RookieYear: metadata.RookieYear, AvailabilityStatus: freeAgent.Status, ListedSalary: freeAgent.ListedSalary,
			HistoricalPointsPerGame: round(value.PPG, 2), HistoricalGames: value.Games,
			EstimatedWinningBid: estimate.Expected, BidLow: estimate.Low, BidHigh: estimate.High,
			BidObservations: estimate.Observations, HistoricalWinningFranchises: estimate.Bidders,
			BidConfidence: estimate.Confidence, Competition: estimate.Competition,
			Source: "Current MFL free-agent pool with historical MFL winning BBID estimate",
		})
	}
	levels := make(map[string]float64)
	for position, candidates := range byPosition {
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].HistoricalGames != candidates[j].HistoricalGames {
				leftEligible := candidates[i].HistoricalGames >= minimumReplacementGames
				rightEligible := candidates[j].HistoricalGames >= minimumReplacementGames
				if leftEligible != rightEligible {
					return leftEligible
				}
			}
			if candidates[i].HistoricalPointsPerGame != candidates[j].HistoricalPointsPerGame {
				return candidates[i].HistoricalPointsPerGame > candidates[j].HistoricalPointsPerGame
			}
			return candidates[i].Name < candidates[j].Name
		})
		eligible := make([]source.ReplacementCandidate, 0, len(candidates))
		selected := make([]source.ReplacementCandidate, 0, 20)
		for _, candidate := range candidates {
			if candidate.HistoricalGames >= minimumReplacementGames {
				eligible = append(eligible, candidate)
				if len(selected) < 10 {
					selected = append(selected, candidate)
				}
			}
		}
		for _, candidate := range candidates {
			if len(selected) >= 20 {
				break
			}
			if candidate.HistoricalGames < minimumReplacementGames && (candidate.HistoricalGames > 0 || candidate.RookieYear >= year-2) {
				selected = append(selected, candidate)
			}
		}
		byPosition[position] = selected
		if len(eligible) >= 3 {
			levels[position] = round(eligible[2].HistoricalPointsPerGame, 2)
		}
	}
	return source.ReplacementLevels{
		Source:                  "Current MFL free agents with four-season league-scored PPG",
		Method:                  "Third-highest recency-weighted PPG by exact position",
		MinimumHistoricalGames:  minimumReplacementGames,
		PointsPerGameByPosition: levels,
		CandidatesByPosition:    byPosition,
		BidSource:               "Winning BBID_WAIVER transactions from the prior three MFL seasons",
		BidMethod:               "Median/P75/P90 winning bid by exact position; expected acquisition salary uses P75",
	}
}

func weightedHistory(history source.HistoricalPoints) map[string]weightedPlayer {
	series := append([]source.HistoricalSeason(nil), history.Seasons...)
	sort.Slice(series, func(i, j int) bool { return series[i].Season > series[j].Season })
	if len(series) > 4 {
		series = series[:4]
	}
	weightedPoints := make(map[string]float64)
	weightedGames := make(map[string]float64)
	totalGames := make(map[string]int)
	for index, season := range series {
		weight := float64(4 - index)
		for id, points := range season.ByPlayerID {
			games := season.GamesPlayedByPlayerID[id]
			if games <= 0 {
				continue
			}
			weightedPoints[id] += points * weight
			weightedGames[id] += float64(games) * weight
			totalGames[id] += games
		}
	}
	result := make(map[string]weightedPlayer)
	for id, points := range weightedPoints {
		result[id] = weightedPlayer{PPG: points / weightedGames[id], Games: totalGames[id]}
	}
	return result
}
