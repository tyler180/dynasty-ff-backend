package mflsync

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/tylermclean/dynasty-ff-backend/internal/analysis/source"
)

const minimumReplacementGames = 8

func loadMFLHistory(
	ctx context.Context,
	caller Caller,
	year int,
	leagueID string,
	catalog map[string]catalogPlayer,
	freeAgentsPayload map[string]any,
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

	replacement := replacementLevels(history, catalog, freeAgentIDs(freeAgentsPayload))
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

func replacementLevels(history source.HistoricalPoints, catalog map[string]catalogPlayer, freeAgents map[string]bool) source.ReplacementLevels {
	values := weightedHistory(history)
	byPosition := make(map[string][]float64)
	for id := range freeAgents {
		metadata, exists := catalog[id]
		value, scored := values[id]
		if !exists || !scored || value.Games < minimumReplacementGames || metadata.Position == "" || metadata.NFLTeam == "" || metadata.NFLTeam == "FA" {
			continue
		}
		byPosition[metadata.Position] = append(byPosition[metadata.Position], value.PPG)
	}
	levels := make(map[string]float64)
	for position, candidates := range byPosition {
		sort.Sort(sort.Reverse(sort.Float64Slice(candidates)))
		if len(candidates) >= 3 {
			levels[position] = round(candidates[2], 2)
		}
	}
	return source.ReplacementLevels{
		Source:                  "Current MFL free agents with four-season league-scored PPG",
		Method:                  "Third-highest recency-weighted PPG by exact position",
		MinimumHistoricalGames:  minimumReplacementGames,
		PointsPerGameByPosition: levels,
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
