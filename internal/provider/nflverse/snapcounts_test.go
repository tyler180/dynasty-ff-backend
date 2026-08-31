package nflverse

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) Do(request *http.Request) (*http.Response, error) { return f(request) }

func TestClientReadsSnapCounts(t *testing.T) {
	header := "game_id,pfr_game_id,season,game_type,week,player,pfr_player_id,position,team,opponent,offense_snaps,offense_pct,defense_snaps,defense_pct,st_snaps,st_pct\n"
	row := "2025_01_DAL_PHI,202509040phi,2025,REG,1,Nakobe Dean,DeanNa00,LB,PHI,DAL,0,0,52,0.83,8,0.31\n"
	client, err := New(roundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://example.test/snaps_2025.csv" {
			t.Fatalf("URL = %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(header + row))}, nil
	}), "https://example.test/snaps_%d.csv")
	if err != nil {
		t.Fatal(err)
	}
	records, err := client.SnapCounts(context.Background(), 2025)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].PFRPlayerID != "DeanNa00" || records[0].DefenseSnaps != 52 || records[0].DefenseSnapPct != 0.83 {
		t.Fatalf("records = %+v", records)
	}
}

func TestDecodeSnapCountsRequiresDefensePercentage(t *testing.T) {
	_, err := decodeSnapCounts(strings.NewReader("game_id,season,game_type,week,pfr_player_id,position,team,opponent,offense_snaps,offense_pct,defense_snaps,st_snaps,st_pct\n"))
	if err == nil || !strings.Contains(err.Error(), "defense_pct") {
		t.Fatalf("error = %v", err)
	}
}

func TestDecodeSnapCountsRejectsMalformedPercentage(t *testing.T) {
	header := "game_id,pfr_game_id,season,game_type,week,player,pfr_player_id,position,team,opponent,offense_snaps,offense_pct,defense_snaps,defense_pct,st_snaps,st_pct\n"
	row := "game-1,pfr-game-1,2025,REG,1,Player,PlayEr00,LB,PHI,DAL,0,0,52,not-a-number,8,0.31\n"
	_, err := decodeSnapCounts(strings.NewReader(header + row))
	if err == nil || !strings.Contains(err.Error(), "defense_pct") {
		t.Fatalf("error = %v", err)
	}
}
