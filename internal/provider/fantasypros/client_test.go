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
	httpClient := &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("x-api-key") != "test-key" {
			t.Fatal("FantasyPros API key header is missing")
		}
		body := `{"players":[]}`
		switch request.URL.Query().Get("type") {
		case "ROOKIES":
			body = `{"players":[{"player_id":28138,"player_name":"Rookie WR","player_position_id":"WR","player_team_id":"SF","rank_ecr":2.5}]}`
		case "DYNASTY":
			body = `{"players":[{"player_id":"28138","player_name":"Rookie WR","player_position_id":"WR","player_team_id":"SF","rank_ecr":"80"}]}`
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
}
