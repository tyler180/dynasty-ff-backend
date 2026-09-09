package nflverse

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const DefaultDepthChartsURLTemplate = "https://github.com/nflverse/nflverse-data/releases/download/depth_charts/depth_charts_%d.csv"

type DepthChartsClient struct {
	httpClient  HTTPClient
	urlTemplate string
}

type DepthChartRecord struct {
	GSISPlayerID    string
	ESPNPlayerID    string
	PlayerName      string
	Season          int
	Week            int
	GameType        string
	ObservedAt      time.Time
	LastSeenAt      time.Time
	Team            string
	PositionGroupID int
	PositionGroup   string
	PositionID      int
	Position        string
	PositionName    string
	PositionSlot    int
	DepthRank       int
}

type DepthChartsDataset struct {
	Records       []DepthChartRecord
	SourceRows    int
	Payload       []byte
	SourceURL     string
	SourceVersion string
}

func NewDepthCharts(httpClient HTTPClient, urlTemplate string) (*DepthChartsClient, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("HTTP client is required")
	}
	if !strings.Contains(urlTemplate, "%d") {
		return nil, fmt.Errorf("nflverse depth-chart URL template must contain %%d")
	}
	return &DepthChartsClient{httpClient: httpClient, urlTemplate: urlTemplate}, nil
}

func NewDefaultDepthCharts(urlTemplate string) (*DepthChartsClient, error) {
	if strings.TrimSpace(urlTemplate) == "" {
		urlTemplate = DefaultDepthChartsURLTemplate
	}
	return NewDepthCharts(&http.Client{Timeout: 3 * time.Minute}, urlTemplate)
}

func (c *DepthChartsClient) DepthChartsDataset(ctx context.Context, season int) (DepthChartsDataset, error) {
	if season < 2001 || season > 2100 {
		return DepthChartsDataset{}, fmt.Errorf("depth-chart season must be between 2001 and 2100")
	}
	sourceURL := fmt.Sprintf(c.urlTemplate, season)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return DepthChartsDataset{}, fmt.Errorf("create nflverse depth-chart request: %w", err)
	}
	request.Header.Set("User-Agent", "dynasty-ff-backend/0.1")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return DepthChartsDataset{}, fmt.Errorf("download nflverse depth charts: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return DepthChartsDataset{}, fmt.Errorf("download nflverse depth charts: HTTP %s", response.Status)
	}
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return DepthChartsDataset{}, fmt.Errorf("read nflverse depth charts: %w", err)
	}
	records, sourceRows, err := decodeDepthCharts(bytes.NewReader(payload), season)
	if err != nil {
		return DepthChartsDataset{}, fmt.Errorf("decode nflverse depth charts: %w", err)
	}
	digest := sha256.Sum256(payload)
	return DepthChartsDataset{
		Records: records, SourceRows: sourceRows, Payload: payload, SourceURL: sourceURL,
		SourceVersion: "sha256:" + hex.EncodeToString(digest[:]),
	}, nil
}

func decodeDepthCharts(reader io.Reader, requestedSeason int) ([]DepthChartRecord, int, error) {
	csvReader := csv.NewReader(reader)
	csvReader.ReuseRecord = true
	header, err := csvReader.Read()
	if err != nil {
		return nil, 0, err
	}
	header = append([]string(nil), header...)
	columns := make(map[string]int, len(header))
	for index, name := range header {
		columns[strings.TrimSpace(name)] = index
	}
	if _, modern := columns["dt"]; modern {
		return decodeCurrentDepthCharts(csvReader, columns, requestedSeason)
	}
	return decodeLegacyDepthCharts(csvReader, columns, requestedSeason)
}

func decodeLegacyDepthCharts(csvReader *csv.Reader, columns map[string]int, requestedSeason int) ([]DepthChartRecord, int, error) {
	if err := requireColumns(columns, "season", "club_code", "week", "game_type", "depth_team", "formation", "gsis_id", "position", "depth_position", "full_name"); err != nil {
		return nil, 0, err
	}
	var records []DepthChartRecord
	seen := make(map[string]int)
	sourceRows := 0
	for line := 2; ; line++ {
		row, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, sourceRows, fmt.Errorf("line %d: %w", line, err)
		}
		sourceRows++
		value := depthChartValue(row, columns)
		season, err := requiredDepthChartInt(value("season"), line, "season")
		if err != nil {
			return nil, sourceRows, err
		}
		if season != requestedSeason || value("week") == "" {
			// nflverse includes post-Super-Bowl SBBYE snapshots without a
			// football week. They are not weekly depth-chart observations.
			continue
		}
		week, err := requiredDepthChartInt(value("week"), line, "week")
		if err != nil {
			return nil, sourceRows, err
		}
		rank, err := requiredDepthChartInt(value("depth_team"), line, "depth_team")
		if err != nil {
			return nil, sourceRows, err
		}
		if value("gsis_id") == "" {
			continue
		}
		record := DepthChartRecord{
			GSISPlayerID: value("gsis_id"), PlayerName: value("full_name"), Season: season, Week: week,
			GameType: value("game_type"), Team: value("club_code"), PositionGroup: value("formation"),
			Position: value("position"), PositionName: value("depth_position"), DepthRank: rank,
		}
		key := strings.Join([]string{
			record.GSISPlayerID, strconv.Itoa(record.Week), record.GameType, record.Team,
			record.PositionGroup, record.Position, record.PositionName,
		}, "\x00")
		if index, ok := seen[key]; ok {
			records[index] = record
			continue
		}
		seen[key] = len(records)
		records = append(records, record)
	}
	return records, sourceRows, nil
}

type activeDepthChartState struct {
	fingerprint string
	index       int
}

func decodeCurrentDepthCharts(csvReader *csv.Reader, columns map[string]int, requestedSeason int) ([]DepthChartRecord, int, error) {
	if err := requireColumns(columns, "dt", "team", "player_name", "espn_id", "gsis_id", "pos_grp_id", "pos_grp", "pos_id", "pos_name", "pos_abb", "pos_slot", "pos_rank"); err != nil {
		return nil, 0, err
	}
	var records []DepthChartRecord
	active := make(map[string]activeDepthChartState)
	sourceRows := 0
	var previousTimestamp time.Time
	for line := 2; ; line++ {
		row, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, sourceRows, fmt.Errorf("line %d: %w", line, err)
		}
		sourceRows++
		value := depthChartValue(row, columns)
		observedAt, err := time.Parse(time.RFC3339, value("dt"))
		if err != nil {
			return nil, sourceRows, fmt.Errorf("line %d column dt: %w", line, err)
		}
		if !previousTimestamp.IsZero() && observedAt.After(previousTimestamp) {
			return nil, sourceRows, fmt.Errorf("line %d depth-chart timestamps are not newest-first", line)
		}
		previousTimestamp = observedAt
		gsisID, espnID := value("gsis_id"), value("espn_id")
		if gsisID == "" && espnID == "" {
			continue
		}
		groupID, err := optionalDepthChartInt(value("pos_grp_id"), line, "pos_grp_id")
		if err != nil {
			return nil, sourceRows, err
		}
		positionID, err := optionalDepthChartInt(value("pos_id"), line, "pos_id")
		if err != nil {
			return nil, sourceRows, err
		}
		slot, err := optionalDepthChartInt(value("pos_slot"), line, "pos_slot")
		if err != nil {
			return nil, sourceRows, err
		}
		rank, err := requiredDepthChartInt(value("pos_rank"), line, "pos_rank")
		if err != nil {
			return nil, sourceRows, err
		}
		identity := gsisID
		if identity == "" {
			identity = "espn:" + espnID
		}
		key := strings.Join([]string{value("team"), identity, strconv.Itoa(groupID), strconv.Itoa(slot)}, "\x00")
		fingerprint := strings.Join([]string{strconv.Itoa(positionID), value("pos_grp"), value("pos_name"), value("pos_abb"), strconv.Itoa(rank)}, "\x00")
		if state, ok := active[key]; ok && state.fingerprint == fingerprint {
			records[state.index].ObservedAt = observedAt
			continue
		}
		records = append(records, DepthChartRecord{
			GSISPlayerID: gsisID, ESPNPlayerID: espnID, PlayerName: value("player_name"), Season: requestedSeason,
			ObservedAt: observedAt, LastSeenAt: observedAt, Team: value("team"), PositionGroupID: groupID,
			PositionGroup: value("pos_grp"), PositionID: positionID, Position: value("pos_abb"),
			PositionName: value("pos_name"), PositionSlot: slot, DepthRank: rank,
		})
		active[key] = activeDepthChartState{fingerprint: fingerprint, index: len(records) - 1}
	}
	return records, sourceRows, nil
}

func requireColumns(columns map[string]int, names ...string) error {
	for _, name := range names {
		if _, ok := columns[name]; !ok {
			return fmt.Errorf("required column %q is missing", name)
		}
	}
	return nil
}

func depthChartValue(row []string, columns map[string]int) func(string) string {
	return func(name string) string {
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
}

func requiredDepthChartInt(value string, line int, column string) (int, error) {
	if value == "" {
		return 0, fmt.Errorf("line %d column %s is required", line, column)
	}
	return optionalDepthChartInt(value, line, column)
}

func optionalDepthChartInt(value string, line int, column string) (int, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("line %d column %s: %w", line, column, err)
	}
	return parsed, nil
}
