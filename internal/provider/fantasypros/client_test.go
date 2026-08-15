package fantasypros

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type staticKey string

func (k staticKey) APIKey(context.Context) (string, error) { return string(k), nil }

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestClientCombinesRankingsAndProjections(t *testing.T) {
	requested := map[string]bool{}
	httpClient := &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("x-api-key") != "test-key" {
			t.Fatal("FantasyPros API key header is missing")
		}
		requested[request.URL.Query().Get("type")+":"+request.URL.Query().Get("position")] = true
		body := `{"players":[]}`
		switch request.URL.Query().Get("type") {
		case "ROOKIES":
			body = `{"players":[{"player_id":28138,"fpid":99901,"player_name":"Rookie WR","player_position_id":"WR","player_team_id":"SF","rank_ecr":2.5}]}`
		case "DYNASTY":
			body = `{"players":[{"player_id":"28138","fpid":"99901","player_name":"Rookie WR","player_position_id":"WR","player_team_id":"SF","rank_ecr":"80"}]}`
		default:
			body = `{"players":[{"fpid":28138,"name":"Rookie WR","position_id":"WR","team_id":"SF","stats":[{"points":120,"points_ppr":150.5}]}]}`
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	client, err := New(httpClient, staticKey("test-key"), "https://example.test")
	if err != nil {
		t.Fatal(err)
	}
	values, err := client.Evaluations(context.Background(), 2026)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 {
		t.Fatalf("evaluations = %+v", values)
	}
	got := values[0]
	if got.FantasyProsID != "28138" || got.RookieRank != 2.5 || got.DynastyRank != 80 || got.ProjectedPoints != 150.5 || got.MarketValue == 0 {
		t.Fatalf("evaluation = %+v", got)
	}
	for _, request := range []string{
		"ROOKIES:ALL", "ROOKIES:IDP",
		"DYNASTY:ALL", "DYNASTY:IDP", "DYNASTY:QB", "DYNASTY:RB", "DYNASTY:WR", "DYNASTY:TE",
		"DYNASTY:DL", "DYNASTY:LB", "DYNASTY:DB",
		":ALL", ":IDP",
	} {
		if !requested[request] {
			t.Errorf("missing FantasyPros request %q; got %v", request, requested)
		}
	}
}

func TestClientUsesPositionalDynastyRankWhenOverallBoardOmitsPlayer(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		body := `{"players":[]}`
		if request.URL.Query().Get("type") == "DYNASTY" {
			switch request.URL.Query().Get("position") {
			case "ALL":
				body = `{"players":[{"player_id":100,"player_name":"Overall Player","player_position_id":"WR","rank_ecr":20}]}`
			case "WR":
				body = `{"players":[{"player_id":100,"player_name":"Overall Player","player_position_id":"WR","rank_ecr":2},{"player_id":200,"player_name":"Positional Player","player_position_id":"WR","rank_ecr":8}]}`
			}
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	client, err := New(httpClient, staticKey("test-key"), "https://example.test")
	if err != nil {
		t.Fatal(err)
	}
	values, err := client.Evaluations(context.Background(), 2026)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]Evaluation, len(values))
	for _, value := range values {
		byID[value.FantasyProsID] = value
	}
	if got := byID["100"].DynastyRank; got != 20 {
		t.Fatalf("overall dynasty rank = %v, want 20", got)
	}
	if got := byID["200"].DynastyRank; got != 8 {
		t.Fatalf("positional dynasty fallback = %v, want 8", got)
	}
}
