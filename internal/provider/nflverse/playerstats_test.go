package nflverse

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPlayerStatsFallsBackAndPreservesAllMetrics(t *testing.T) {
	header := "player_id,player_name,player_display_name,position,position_group,headshot_url,season,week,season_type,game_id,team,opponent_team,passing_yards,def_sacks,fg_made_list\n"
	row := "00-001,A.Player,A Player,LB,LB,https://example.test/a.png,2025,1,REG,game-1,PHI,DAL,0,1.5,42;51\n"
	client, err := NewPlayerStats(roundTripper(func(request *http.Request) (*http.Response, error) {
		if strings.Contains(request.URL.String(), "legacy") {
			return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(header + row))}, nil
	}), []string{"https://example.test/legacy_%d.csv", "https://example.test/current_%d.csv"})
	if err != nil {
		t.Fatal(err)
	}
	dataset, err := client.PlayerStatsDataset(context.Background(), 2025)
	if err != nil {
		t.Fatal(err)
	}
	if len(dataset.Records) != 1 || dataset.Records[0].Metrics["def_sacks"] != 1.5 || dataset.Records[0].Attributes["fg_made_list"] != "42;51" || dataset.Records[0].Attributes["headshot_url"] == "" {
		t.Fatalf("dataset = %+v", dataset)
	}
	if !strings.Contains(dataset.SourceURL, "current_2025.csv") || dataset.SourceVersion == "" {
		t.Fatalf("metadata = %+v", dataset)
	}
}

func TestPlayerStatsSkipsAnonymousRowsAndDoesNotStoreNonFiniteNumbers(t *testing.T) {
	header := "player_id,season,week,season_type,game_id,team,opponent_team,receiving_yards,target_share\n"
	rows := ",2025,1,REG,game-1,PHI,DAL,12,0.5\n00-001,2025,1,REG,game-1,PHI,DAL,NaN,0.5\n"
	client, err := NewPlayerStats(roundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(header + rows))}, nil
	}), []string{"https://example.test/current_%d.csv"})
	if err != nil {
		t.Fatal(err)
	}
	dataset, err := client.PlayerStatsDataset(context.Background(), 2025)
	if err != nil {
		t.Fatal(err)
	}
	if len(dataset.Records) != 1 {
		t.Fatalf("records = %d, want 1", len(dataset.Records))
	}
	if dataset.Records[0].Metrics["target_share"] != 0.5 || dataset.Records[0].Attributes["receiving_yards"] != "NaN" {
		t.Fatalf("record = %+v", dataset.Records[0])
	}
}
