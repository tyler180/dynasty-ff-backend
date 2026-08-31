// Package nflverse reads public NFL data releases from nflverse.
package nflverse

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const DefaultSnapCountsURLTemplate = "https://github.com/nflverse/nflverse-data/releases/download/snap_counts/snap_counts_%d.csv"

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	httpClient  HTTPClient
	urlTemplate string
}

type SnapCount struct {
	GameID           string
	PFRGameID        string
	Season           int
	GameType         string
	Week             int
	PlayerName       string
	PFRPlayerID      string
	Position         string
	Team             string
	Opponent         string
	OffenseSnaps     int
	OffenseSnapPct   float64
	DefenseSnaps     int
	DefenseSnapPct   float64
	SpecialTeamSnaps int
	SpecialTeamPct   float64
}

func New(httpClient HTTPClient, urlTemplate string) (*Client, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("HTTP client is required")
	}
	if !strings.Contains(urlTemplate, "%d") {
		return nil, fmt.Errorf("nflverse snap-count URL template must contain %%d for the season")
	}
	return &Client{httpClient: httpClient, urlTemplate: urlTemplate}, nil
}

func NewDefault(urlTemplate string) (*Client, error) {
	return New(&http.Client{Timeout: 2 * time.Minute}, urlTemplate)
}

func (c *Client) SnapCounts(ctx context.Context, season int) ([]SnapCount, error) {
	if season < 2012 || season > 2100 {
		return nil, fmt.Errorf("snap-count season must be between 2012 and 2100")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(c.urlTemplate, season), nil)
	if err != nil {
		return nil, fmt.Errorf("create nflverse snap-count request: %w", err)
	}
	request.Header.Set("User-Agent", "dynasty-ff-backend/0.1")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download nflverse snap counts: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download nflverse snap counts: HTTP %s", response.Status)
	}
	records, err := decodeSnapCounts(response.Body)
	if err != nil {
		return nil, fmt.Errorf("decode nflverse snap counts: %w", err)
	}
	return records, nil
}

func decodeSnapCounts(reader io.Reader) ([]SnapCount, error) {
	csvReader := csv.NewReader(reader)
	csvReader.ReuseRecord = true
	header, err := csvReader.Read()
	if err != nil {
		return nil, err
	}
	columns := make(map[string]int, len(header))
	for index, name := range header {
		columns[strings.TrimSpace(name)] = index
	}
	required := []string{"game_id", "season", "game_type", "week", "pfr_player_id", "position", "team", "opponent", "offense_snaps", "offense_pct", "defense_snaps", "defense_pct", "st_snaps", "st_pct"}
	for _, name := range required {
		if _, ok := columns[name]; !ok {
			return nil, fmt.Errorf("required column %q is missing", name)
		}
	}
	var records []SnapCount
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
		var valueErr error
		integer := func(name string) int {
			raw := value(name)
			if raw == "" {
				return 0
			}
			parsed, err := strconv.Atoi(raw)
			if err != nil && valueErr == nil {
				valueErr = fmt.Errorf("line %d column %s: %w", line, name, err)
			}
			return parsed
		}
		decimal := func(name string) float64 {
			raw := value(name)
			if raw == "" {
				return 0
			}
			parsed, err := strconv.ParseFloat(raw, 64)
			if err != nil && valueErr == nil {
				valueErr = fmt.Errorf("line %d column %s: %w", line, name, err)
			}
			return parsed
		}
		record := SnapCount{
			GameID: value("game_id"), PFRGameID: value("pfr_game_id"), Season: integer("season"), GameType: value("game_type"), Week: integer("week"),
			PlayerName: value("player"), PFRPlayerID: value("pfr_player_id"), Position: value("position"), Team: value("team"), Opponent: value("opponent"),
			OffenseSnaps: integer("offense_snaps"), OffenseSnapPct: decimal("offense_pct"), DefenseSnaps: integer("defense_snaps"), DefenseSnapPct: decimal("defense_pct"),
			SpecialTeamSnaps: integer("st_snaps"), SpecialTeamPct: decimal("st_pct"),
		}
		if valueErr != nil {
			return nil, valueErr
		}
		records = append(records, record)
	}
	return records, nil
}
