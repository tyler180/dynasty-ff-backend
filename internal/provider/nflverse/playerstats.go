package nflverse

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var DefaultPlayerStatsURLTemplates = []string{
	"https://github.com/nflverse/nflverse-data/releases/download/player_stats/stats_player_week_%d.csv",
	"https://github.com/nflverse/nflverse-data/releases/download/stats_player/stats_player_week_%d.csv",
}

type PlayerStatsClient struct {
	httpClient   HTTPClient
	urlTemplates []string
}

type PlayerStat struct {
	GSISPlayerID  string
	PlayerName    string
	DisplayName   string
	Position      string
	PositionGroup string
	Season        int
	Week          int
	GameType      string
	GameID        string
	Team          string
	Opponent      string
	Metrics       map[string]float64
	Attributes    map[string]string
}

type PlayerStatsDataset struct {
	Records       []PlayerStat
	Payload       []byte
	SourceURL     string
	SourceVersion string
}

func NewPlayerStats(httpClient HTTPClient, urlTemplates []string) (*PlayerStatsClient, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("HTTP client is required")
	}
	if len(urlTemplates) == 0 {
		return nil, fmt.Errorf("at least one player-stat URL template is required")
	}
	for _, template := range urlTemplates {
		if !strings.Contains(template, "%d") {
			return nil, fmt.Errorf("nflverse player-stat URL template must contain %%d")
		}
	}
	return &PlayerStatsClient{httpClient: httpClient, urlTemplates: append([]string(nil), urlTemplates...)}, nil
}

func NewDefaultPlayerStats(urlTemplates []string) (*PlayerStatsClient, error) {
	if len(urlTemplates) == 0 {
		urlTemplates = DefaultPlayerStatsURLTemplates
	}
	return NewPlayerStats(&http.Client{Timeout: 2 * time.Minute}, urlTemplates)
}

func (c *PlayerStatsClient) PlayerStatsDataset(ctx context.Context, season int) (PlayerStatsDataset, error) {
	if season < 1999 || season > 2100 {
		return PlayerStatsDataset{}, fmt.Errorf("player-stat season must be between 1999 and 2100")
	}
	var notFound []string
	for _, template := range c.urlTemplates {
		sourceURL := fmt.Sprintf(template, season)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
		if err != nil {
			return PlayerStatsDataset{}, fmt.Errorf("create nflverse player-stat request: %w", err)
		}
		request.Header.Set("User-Agent", "dynasty-ff-backend/0.1")
		response, err := c.httpClient.Do(request)
		if err != nil {
			return PlayerStatsDataset{}, fmt.Errorf("download nflverse player stats: %w", err)
		}
		if response.StatusCode == http.StatusNotFound {
			response.Body.Close()
			notFound = append(notFound, sourceURL)
			continue
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return PlayerStatsDataset{}, fmt.Errorf("download nflverse player stats: HTTP %s", response.Status)
		}
		payload, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			return PlayerStatsDataset{}, fmt.Errorf("read nflverse player stats: %w", err)
		}
		records, err := decodePlayerStats(bytes.NewReader(payload))
		if err != nil {
			return PlayerStatsDataset{}, fmt.Errorf("decode nflverse player stats: %w", err)
		}
		digest := sha256.Sum256(payload)
		return PlayerStatsDataset{
			Records: records, Payload: payload, SourceURL: sourceURL,
			SourceVersion: "sha256:" + hex.EncodeToString(digest[:]),
		}, nil
	}
	return PlayerStatsDataset{}, fmt.Errorf("nflverse player stats were not found for %d at %s", season, strings.Join(notFound, ", "))
}

func decodePlayerStats(reader io.Reader) ([]PlayerStat, error) {
	csvReader := csv.NewReader(reader)
	csvReader.ReuseRecord = true
	header, err := csvReader.Read()
	if err != nil {
		return nil, err
	}
	header = append([]string(nil), header...)
	columns := make(map[string]int, len(header))
	for index, name := range header {
		columns[strings.TrimSpace(name)] = index
	}
	required := []string{"player_id", "season", "week", "season_type", "game_id", "team", "opponent_team"}
	for _, name := range required {
		if _, ok := columns[name]; !ok {
			return nil, fmt.Errorf("required column %q is missing", name)
		}
	}
	core := map[string]struct{}{
		"player_id": {}, "player_name": {}, "player_display_name": {}, "position": {}, "position_group": {},
		"season": {}, "week": {}, "season_type": {}, "game_id": {}, "team": {}, "opponent_team": {},
	}
	var records []PlayerStat
	for line := 2; ; line++ {
		row, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		value := func(name string) string {
			index, ok := columns[name]
			if !ok || index >= len(row) {
				return ""
			}
			value := strings.TrimSpace(row[index])
			if strings.EqualFold(value, "NA") {
				return ""
			}
			return value
		}
		season, err := strconv.Atoi(value("season"))
		if err != nil {
			return nil, fmt.Errorf("line %d column season: %w", line, err)
		}
		week, err := strconv.Atoi(value("week"))
		if err != nil {
			return nil, fmt.Errorf("line %d column week: %w", line, err)
		}
		record := PlayerStat{
			GSISPlayerID: value("player_id"), PlayerName: value("player_name"), DisplayName: value("player_display_name"),
			Position: value("position"), PositionGroup: value("position_group"), Season: season, Week: week,
			GameType: value("season_type"), GameID: value("game_id"), Team: value("team"), Opponent: value("opponent_team"),
			Metrics: make(map[string]float64), Attributes: make(map[string]string),
		}
		for _, name := range header {
			name = strings.TrimSpace(name)
			if _, ok := core[name]; ok {
				continue
			}
			raw := value(name)
			if raw == "" {
				continue
			}
			if number, err := strconv.ParseFloat(raw, 64); err == nil && !math.IsNaN(number) && !math.IsInf(number, 0) {
				record.Metrics[name] = number
			} else {
				record.Attributes[name] = raw
			}
		}
		// nflverse includes a small number of team-level recovery rows without
		// a player identity. They are not player-game observations.
		if record.GSISPlayerID == "" {
			continue
		}
		if record.GameID == "" {
			return nil, fmt.Errorf("line %d requires game_id", line)
		}
		records = append(records, record)
	}
	return records, nil
}
