// Package dynastyprocess reads the public DynastyProcess player-ID crosswalk.
package dynastyprocess

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

const DefaultURL = "https://raw.githubusercontent.com/DynastyProcess/data/master/files/db_playerids.csv"

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	httpClient HTTPClient
	url        string
}

type Player struct {
	Name          string
	BirthDate     string
	Season        int
	DraftYear     int
	DraftRound    int
	DraftPick     int
	MFLID         string
	GSISID        string
	PFRID         string
	PFFID         string
	ESPNID        string
	SleeperID     string
	FantasyProsID string
	NFLID         string
	YahooID       string
	CBSID         string
	FleaflickerID string
	RotowireID    string
	KTCID         string
	FantasyDataID string
	SportradarID  string
	CFBRefID      string
}

func New(httpClient HTTPClient, url string) (*Client, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("HTTP client is required")
	}
	if strings.TrimSpace(url) == "" {
		return nil, fmt.Errorf("DynastyProcess player IDs URL is required")
	}
	return &Client{httpClient: httpClient, url: url}, nil
}

func NewDefault(url string) (*Client, error) {
	return New(&http.Client{Timeout: 2 * time.Minute}, url)
}

func (c *Client) Players(ctx context.Context) ([]Player, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, fmt.Errorf("create DynastyProcess request: %w", err)
	}
	request.Header.Set("User-Agent", "dynasty-ff-backend/0.1")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download DynastyProcess player IDs: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download DynastyProcess player IDs: HTTP %s", response.Status)
	}
	players, err := decode(response.Body)
	if err != nil {
		return nil, fmt.Errorf("decode DynastyProcess player IDs: %w", err)
	}
	return players, nil
}

func decode(reader io.Reader) ([]Player, error) {
	csvReader := csv.NewReader(reader)
	csvReader.ReuseRecord = true
	header, err := csvReader.Read()
	if err != nil {
		return nil, err
	}
	columns := make(map[string]int, len(header))
	for index, name := range header {
		columns[name] = index
	}
	for _, required := range []string{"mfl_id", "name", "db_season"} {
		if _, ok := columns[required]; !ok {
			return nil, fmt.Errorf("required column %q is missing", required)
		}
	}

	var players []Player
	for line := 2; ; line++ {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		value := func(name string) string {
			index, ok := columns[name]
			if !ok || index >= len(record) {
				return ""
			}
			value := strings.TrimSpace(record[index])
			if strings.EqualFold(value, "NA") {
				return ""
			}
			return value
		}
		players = append(players, Player{
			Name: value("name"), BirthDate: value("birthdate"), Season: integer(value("db_season")),
			DraftYear: integer(value("draft_year")), DraftRound: integer(value("draft_round")), DraftPick: integer(value("draft_pick")),
			MFLID: value("mfl_id"), GSISID: value("gsis_id"), PFRID: value("pfr_id"), PFFID: value("pff_id"),
			ESPNID: value("espn_id"), SleeperID: value("sleeper_id"), FantasyProsID: value("fantasypros_id"),
			NFLID: value("nfl_id"), YahooID: value("yahoo_id"), CBSID: value("cbs_id"), FleaflickerID: value("fleaflicker_id"),
			RotowireID: value("rotowire_id"), KTCID: value("ktc_id"), FantasyDataID: value("fantasy_data_id"),
			SportradarID: value("sportradar_id"), CFBRefID: value("cfbref_id"),
		})
	}
	return players, nil
}

func integer(value string) int {
	parsed, _ := strconv.Atoi(value)
	return parsed
}
