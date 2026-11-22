package dynamodbplayerstats

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
)

// failingPutClient rejects writes, as a throttled or oversized PutItem would.
type failingPutClient struct{ fakeClient }

func (f *failingPutClient) PutItem(context.Context, *dynamodb.PutItemInput, ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	return nil, errors.New("throttled")
}

func TestSeasonScheduleRoundTripsAsOneItem(t *testing.T) {
	games := []history.ScheduledGame{
		{Season: 2025, Week: 1, GameID: "2025_01_DAL_PHI", GameType: "REG", Teams: []string{"PHI", "DAL"}},
		{Season: 2025, Week: 19, GameID: "2025_19_BUF_DEN", GameType: "WC", Teams: []string{"DEN", "BUF"}},
	}
	client := &fakeClient{}
	repository, err := New(client, "player-game")
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.PutSeasonSchedule(context.Background(), 2025, games); err != nil {
		t.Fatal(err)
	}
	if len(client.writes) != 0 {
		t.Fatalf("schedule must be one atomic PutItem, not %d batch writes", len(client.writes))
	}
	if optionalString(client.state, "pk") != "SCHEDULE" || optionalString(client.state, "sk") != "SEASON#2025" ||
		optionalString(client.state, "entity_type") != "season_schedule" {
		t.Fatalf("item key = %s / %s (%s)", optionalString(client.state, "pk"), optionalString(client.state, "sk"), optionalString(client.state, "entity_type"))
	}
	got, err := repository.SeasonSchedules(context.Background(), []int{2025})
	if err != nil {
		t.Fatal(err)
	}
	want := []history.ScheduledGame{
		{Season: 2025, Week: 1, GameID: "2025_01_DAL_PHI", GameType: "REG", Teams: []string{"DAL", "PHI"}},
		{Season: 2025, Week: 19, GameID: "2025_19_BUF_DEN", GameType: "WC", Teams: []string{"BUF", "DEN"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("games = %+v\nwant %+v", got, want)
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
			client := &fakeClient{}
			repository, _ := New(client, "player-game")
			err := repository.PutSeasonSchedule(context.Background(), test.season, test.games)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("err = %v, want %q", err, test.wantErr)
			}
			if client.state != nil {
				t.Fatalf("bad input was written: %+v", client.state)
			}
		})
	}
}

func TestPutSeasonScheduleReportsWriteFailure(t *testing.T) {
	repository, _ := New(&failingPutClient{}, "player-game")
	err := repository.PutSeasonSchedule(context.Background(), 2025, []history.ScheduledGame{
		{Season: 2025, Week: 1, GameID: "g1", Teams: []string{"PHI"}},
	})
	if err == nil || !strings.Contains(err.Error(), "put 2025 schedule in DynamoDB: throttled") {
		t.Fatalf("err = %v", err)
	}
}

func TestSeasonSchedulesMissingSeasonIsEmpty(t *testing.T) {
	repository, _ := New(&fakeClient{}, "player-game")
	got, err := repository.SeasonSchedules(context.Background(), []int{2026})
	if err != nil || len(got) != 0 {
		t.Fatalf("games = %+v, err = %v", got, err)
	}
}

func TestSeasonSchedulesRejectsMalformedItems(t *testing.T) {
	game := func(season int) types.AttributeValue {
		return encodeScheduledGame(history.ScheduledGame{Season: season, Week: 1, GameID: "g1", Teams: []string{"PHI"}})
	}
	tests := []struct {
		name    string
		item    map[string]types.AttributeValue
		wantErr string
	}{
		{"wrong entity", map[string]types.AttributeValue{"entity_type": stringValue("dataset_state")}, "unexpected entity type"},
		{"no games list", map[string]types.AttributeValue{"entity_type": stringValue("season_schedule")}, "no games list"},
		{"game from another season", map[string]types.AttributeValue{
			"entity_type": stringValue("season_schedule"), "games": &types.AttributeValueMemberL{Value: []types.AttributeValue{game(2025)}},
		}, "holds a 2025 game"},
		{"game is not a map", map[string]types.AttributeValue{
			"entity_type": stringValue("season_schedule"), "games": &types.AttributeValueMemberL{Value: []types.AttributeValue{stringValue("g1")}},
		}, "not a map"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, _ := New(&fakeClient{state: test.item}, "player-game")
			_, err := repository.SeasonSchedules(context.Background(), []int{2026})
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("err = %v, want %q", err, test.wantErr)
			}
		})
	}
}
