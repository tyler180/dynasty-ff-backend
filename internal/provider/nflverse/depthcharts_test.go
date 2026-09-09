package nflverse

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDepthChartsParsesLegacyWeeklySchema(t *testing.T) {
	payload := "season,club_code,week,game_type,depth_team,last_name,first_name,football_name,formation,gsis_id,jersey_number,position,elias_id,depth_position,full_name\n" +
		"2024,ATL,1,REG,2,London,Drake,Drake,Offense,00-0037238,5,WR,,LWR,Drake London\n" +
		"2024,ATL,,SBBYE,1,London,Drake,Drake,Offense,00-0037238,5,WR,,LWR,Drake London\n"
	client, err := NewDepthCharts(roundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(payload))}, nil
	}), "https://example.test/depth_charts_%d.csv")
	if err != nil {
		t.Fatal(err)
	}
	dataset, err := client.DepthChartsDataset(context.Background(), 2024)
	if err != nil {
		t.Fatal(err)
	}
	if dataset.SourceRows != 2 || len(dataset.Records) != 1 {
		t.Fatalf("dataset counts = %d/%d", dataset.SourceRows, len(dataset.Records))
	}
	record := dataset.Records[0]
	if record.GSISPlayerID != "00-0037238" || record.Week != 1 || record.DepthRank != 2 || record.PositionName != "LWR" {
		t.Fatalf("record = %+v", record)
	}
}

func TestDepthChartsCollapsesUnchangedCurrentSnapshots(t *testing.T) {
	header := "dt,team,player_name,espn_id,gsis_id,pos_grp_id,pos_grp,pos_id,pos_name,pos_abb,pos_slot,pos_rank\n"
	rows := "2026-09-09T12:00:00Z,ATL,Drake London,4362921,00-0037238,1,11 Personnel,4,Wide Receiver,LWR,2,1\n" +
		"2026-09-08T12:00:00Z,ATL,Drake London,4362921,00-0037238,1,11 Personnel,4,Wide Receiver,LWR,2,1\n" +
		"2026-09-07T12:00:00Z,ATL,Drake London,4362921,00-0037238,1,11 Personnel,4,Wide Receiver,LWR,2,2\n" +
		"2026-09-06T12:00:00Z,ATL,No GSIS,12345,,1,11 Personnel,4,Wide Receiver,RWR,3,3\n"
	records, sourceRows, err := decodeDepthCharts(strings.NewReader(header+rows), 2026)
	if err != nil {
		t.Fatal(err)
	}
	if sourceRows != 4 || len(records) != 3 {
		t.Fatalf("counts = %d/%d, want 4/3", sourceRows, len(records))
	}
	if records[0].DepthRank != 1 || !records[0].ObservedAt.Equal(time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)) || !records[0].LastSeenAt.Equal(time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("collapsed interval = %+v", records[0])
	}
	if records[2].GSISPlayerID != "" || records[2].ESPNPlayerID != "12345" {
		t.Fatalf("ESPN fallback record = %+v", records[2])
	}
}

func TestDepthChartsRejectsOldestFirstCurrentData(t *testing.T) {
	header := "dt,team,player_name,espn_id,gsis_id,pos_grp_id,pos_grp,pos_id,pos_name,pos_abb,pos_slot,pos_rank\n"
	rows := "2026-09-08T12:00:00Z,ATL,A,1,00-1,1,G,1,P,P,1,1\n2026-09-09T12:00:00Z,ATL,A,1,00-1,1,G,1,P,P,1,1\n"
	if _, _, err := decodeDepthCharts(strings.NewReader(header+rows), 2026); err == nil {
		t.Fatal("expected ordering error")
	}
}
