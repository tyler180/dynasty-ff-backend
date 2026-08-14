// Package fantasypros reads licensed rankings and projections from the official FantasyPros API.
package fantasypros

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.fantasypros.com/public/v2/json"

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type APIKeyProvider interface {
	APIKey(context.Context) (string, error)
}

type Client struct {
	httpClient HTTPClient
	keys       APIKeyProvider
	baseURL    string
}

type Evaluation struct {
	FantasyProsID   string
	Name            string
	Position        string
	NFLTeam         string
	RookieRank      float64
	DynastyRank     float64
	MarketValue     float64
	ProjectedPoints float64
}

func New(httpClient HTTPClient, keys APIKeyProvider, baseURL string) (*Client, error) {
	if httpClient == nil || keys == nil {
		return nil, fmt.Errorf("FantasyPros HTTP client and API key provider are required")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{httpClient: httpClient, keys: keys, baseURL: strings.TrimRight(baseURL, "/")}, nil
}

func NewDefault(keys APIKeyProvider) (*Client, error) {
	return New(&http.Client{Timeout: 90 * time.Second}, keys, DefaultBaseURL)
}

func (c *Client) Evaluations(ctx context.Context, season int) ([]Evaluation, error) {
	if season < 2000 || season > 2100 {
		return nil, fmt.Errorf("FantasyPros season is invalid")
	}
	apiKey, err := c.keys.APIKey(ctx)
	if err != nil {
		return nil, err
	}
	values := make(map[string]*Evaluation)
	for _, rankingType := range []string{"ROOKIES", "DYNASTY"} {
		payload, err := c.get(ctx, apiKey, fmt.Sprintf("nfl/%d/consensus-rankings", season), url.Values{
			"position": {"ALL"}, "type": {rankingType}, "scoring": {"PPR"}, "include_idp": {"true"},
		})
		if err != nil {
			return nil, err
		}
		var response rankingResponse
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, fmt.Errorf("decode FantasyPros %s rankings: %w", strings.ToLower(rankingType), err)
		}
		for _, ranked := range response.Players {
			id := ranked.id()
			if id == "" {
				continue
			}
			item := evaluation(values, id)
			item.mergeIdentity(ranked.Name, ranked.Position, ranked.Team)
			if rankingType == "ROOKIES" {
				item.RookieRank = ranked.Rank.Float64()
			} else {
				item.DynastyRank = ranked.Rank.Float64()
			}
		}
	}

	payload, err := c.get(ctx, apiKey, fmt.Sprintf("nfl/%d/projections", season), url.Values{
		"position": {"ALL"}, "positions": {"QB:RB:WR:TE:K:DL:LB:DB"}, "week": {"0"},
	})
	if err != nil {
		return nil, err
	}
	var projections projectionResponse
	if err := json.Unmarshal(payload, &projections); err != nil {
		return nil, fmt.Errorf("decode FantasyPros projections: %w", err)
	}
	for _, projected := range projections.Players {
		id := projected.id()
		if id == "" {
			continue
		}
		item := evaluation(values, id)
		item.mergeIdentity(projected.Name, projected.Position, projected.Team)
		item.ProjectedPoints = projected.points()
	}

	result := make([]Evaluation, 0, len(values))
	for _, item := range values {
		rank := item.DynastyRank
		if rank == 0 {
			rank = item.RookieRank
		}
		if rank > 0 {
			item.MarketValue = math.Round(10500 * math.Exp(-0.0235*rank))
		}
		result = append(result, *item)
	}
	return result, nil
}

func (c *Client) get(ctx context.Context, apiKey, endpoint string, query url.Values) ([]byte, error) {
	requestURL := c.baseURL + "/" + endpoint + "?" + query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create FantasyPros request: %w", err)
	}
	request.Header.Set("x-api-key", apiKey)
	request.Header.Set("User-Agent", "dynasty-ff-backend/0.1")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request FantasyPros %s: %w", endpoint, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request FantasyPros %s: HTTP %s", endpoint, response.Status)
	}
	var body json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("read FantasyPros %s: %w", endpoint, err)
	}
	return body, nil
}

type scalar string

func (s *scalar) UnmarshalJSON(payload []byte) error {
	value := strings.TrimSpace(string(payload))
	if value == "null" {
		*s = ""
		return nil
	}
	if unquoted, err := strconv.Unquote(value); err == nil {
		*s = scalar(unquoted)
		return nil
	}
	*s = scalar(value)
	return nil
}

func (s scalar) String() string { return strings.TrimSpace(string(s)) }
func (s scalar) Float64() float64 {
	value, _ := strconv.ParseFloat(s.String(), 64)
	return value
}

type rankingResponse struct {
	Players []rankingPlayer `json:"players"`
}
type rankingPlayer struct {
	PlayerID scalar `json:"player_id"`
	FPID     scalar `json:"fpid"`
	Name     string `json:"player_name"`
	Position string `json:"player_position_id"`
	Team     string `json:"player_team_id"`
	Rank     scalar `json:"rank_ecr"`
}

func (p rankingPlayer) id() string {
	if p.PlayerID.String() != "" {
		return p.PlayerID.String()
	}
	return p.FPID.String()
}

type projectionResponse struct {
	Players []projectionPlayer `json:"players"`
}
type projectionPlayer struct {
	FPID     scalar          `json:"fpid"`
	PlayerID scalar          `json:"player_id"`
	Name     string          `json:"name"`
	Position string          `json:"position_id"`
	Team     string          `json:"team_id"`
	Stats    json.RawMessage `json:"stats"`
}

func (p projectionPlayer) id() string {
	if p.FPID.String() != "" {
		return p.FPID.String()
	}
	return p.PlayerID.String()
}
func (p projectionPlayer) points() float64 {
	var rows []map[string]scalar
	if err := json.Unmarshal(p.Stats, &rows); err != nil {
		var row map[string]scalar
		if json.Unmarshal(p.Stats, &row) == nil {
			rows = []map[string]scalar{row}
		}
	}
	if len(rows) == 0 {
		return 0
	}
	if value := rows[0]["points_ppr"].Float64(); value != 0 {
		return value
	}
	return rows[0]["points"].Float64()
}

func evaluation(values map[string]*Evaluation, id string) *Evaluation {
	if values[id] == nil {
		values[id] = &Evaluation{FantasyProsID: id}
	}
	return values[id]
}
func (e *Evaluation) mergeIdentity(name, position, team string) {
	if e.Name == "" {
		e.Name = strings.TrimSpace(name)
	}
	if e.Position == "" {
		e.Position = strings.ToUpper(strings.TrimSpace(position))
	}
	if e.NFLTeam == "" {
		e.NFLTeam = strings.ToUpper(strings.TrimSpace(team))
	}
}
