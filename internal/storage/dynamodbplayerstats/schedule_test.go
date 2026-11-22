package dynamodbplayerstats

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
)

func TestPutSeasonScheduleReplacesSeasonGames(t *testing.T) {
	kept := history.ScheduledGame{Season: 2025, Week: 1, GameID: "2025_01_DAL_PHI", GameType: "REG", Teams: []string{"PHI", "DAL"}}
	stale := encodeScheduledGame(history.ScheduledGame{Season: 2025, Week: 1, GameID: "2025_01_OLD", GameType: "REG", Teams: []string{"NE"}})
	client := &fakeClient{queryItems: []map[string]types.AttributeValue{encodeScheduledGame(kept), stale}}
	repository, err := New(client, "player-game")
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.PutSeasonSchedule(context.Background(), 2025, []history.ScheduledGame{kept}); err != nil {
		t.Fatal(err)
	}
	if len(client.writes) != 2 || client.writes[0].PutRequest == nil || client.writes[1].DeleteRequest == nil {
		t.Fatalf("writes = %+v", client.writes)
	}
	put := client.writes[0].PutRequest.Item
	if optionalString(put, "pk") != "SCHEDULE#SEASON#2025" || optionalString(put, "sk") != "GAME#01#2025_01_DAL_PHI" {
		t.Fatalf("put key = %s / %s", optionalString(put, "pk"), optionalString(put, "sk"))
	}
	if optionalString(client.writes[1].DeleteRequest.Key, "sk") != "GAME#01#2025_01_OLD" {
		t.Fatalf("deleted %+v, want the stale game", client.writes[1].DeleteRequest.Key)
	}
	if query := client.queries[0]; query.IndexName != nil || *query.KeyConditionExpression != "pk = :pk AND begins_with(sk, :prefix)" {
		t.Fatalf("schedule should be read from the base table: %+v", query)
	}
}

func TestPutSeasonScheduleRejectsBadInput(t *testing.T) {
	game := history.ScheduledGame{Season: 2025, Week: 1, GameID: "g1", Teams: []string{"PHI"}}
	tests := []struct {
		name    string
		season  int
		games   []history.ScheduledGame
		wantErr string
	}{
		{"no games", 2025, nil, "at least one scheduled game"},
		{"wrong season", 2024, []history.ScheduledGame{game}, "must belong to season 2024"},
		{"duplicate game", 2025, []history.ScheduledGame{game, game}, "duplicate scheduled game"},
		{"no teams", 2025, []history.ScheduledGame{{Season: 2025, Week: 1, GameID: "g2"}}, "needs at least one team"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeClient{queryItems: []map[string]types.AttributeValue{}}
			repository, _ := New(client, "player-game")
			err := repository.PutSeasonSchedule(context.Background(), test.season, test.games)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("err = %v, want %q", err, test.wantErr)
			}
			if len(client.writes) != 0 {
				t.Fatalf("bad input wrote %d items", len(client.writes))
			}
		})
	}
}

func TestSeasonSchedulesDecodesOnlyScheduledGames(t *testing.T) {
	game := history.ScheduledGame{Season: 2026, Week: 11, GameID: "2026_11_BUF_MIA", GameType: "REG", Teams: []string{"MIA", "BUF"}}
	other := map[string]types.AttributeValue{"pk": stringValue("SCHEDULE#SEASON#2026"), "sk": stringValue("GAME#00#x"), "entity_type": stringValue("something_else")}
	client := &fakeClient{queryItems: []map[string]types.AttributeValue{encodeScheduledGame(game), other}}
	repository, _ := New(client, "player-game")
	got, err := repository.SeasonSchedules(context.Background(), []int{2026})
	if err != nil {
		t.Fatal(err)
	}
	want := []history.ScheduledGame{{Season: 2026, Week: 11, GameID: "2026_11_BUF_MIA", GameType: "REG", Teams: []string{"BUF", "MIA"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("games = %+v", got)
	}
}
