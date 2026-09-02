package dynamodbplayerstats

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
)

type fakeClient struct {
	mu      sync.Mutex
	queries []*dynamodb.QueryInput
	writes  []types.WriteRequest
	state   map[string]types.AttributeValue
}

func (f *fakeClient) BatchWriteItem(_ context.Context, input *dynamodb.BatchWriteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error) {
	for _, requests := range input.RequestItems {
		f.writes = append(f.writes, requests...)
	}
	return &dynamodb.BatchWriteItemOutput{}, nil
}

func (f *fakeClient) GetItem(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	return &dynamodb.GetItemOutput{Item: f.state}, nil
}

func (f *fakeClient) PutItem(_ context.Context, input *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	f.state = input.Item
	return &dynamodb.PutItemOutput{}, nil
}

func (f *fakeClient) Query(_ context.Context, input *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, input)
	if input.IndexName != nil {
		return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{
			{"pk": stringValue("PLAYER#stale"), "sk": stringValue("GAME#2025#01#old")},
		}}, nil
	}
	return &dynamodb.QueryOutput{Items: []map[string]types.AttributeValue{
		encode(history.PlayerGameSnaps{
			PlayerID: "player-1", GameID: "game-1", Season: 2025, Week: 1, PositionGroup: "LB",
			DefenseSnaps: 52, TeamDefenseSnaps: 63, DefenseSnapPct: 0.83, Source: "nflverse-pfr",
		}),
		encode(history.PlayerGameSnaps{
			PlayerID: "player-1", GameID: "game-2", Season: 2024, Week: 2, PositionGroup: "DB",
			DefenseSnaps: 40, TeamDefenseSnaps: 60, DefenseSnapPct: 0.67, Source: "nflverse-pfr",
		}),
	}}, nil
}

func TestRepositoryReplacesSeasonAndQueriesPlayerHistory(t *testing.T) {
	client := &fakeClient{}
	repository, err := New(client, "player-game-stats")
	if err != nil {
		t.Fatal(err)
	}
	record := history.PlayerGameSnaps{
		PlayerID: "player-1", GameID: "game-1", Season: 2025, Week: 1, PositionGroup: "LB",
		DefenseSnaps: 52, TeamDefenseSnaps: 63, DefenseSnapPct: 0.83, Source: "nflverse-pfr",
	}
	if err := repository.PutPlayerGameSnaps(context.Background(), []history.PlayerGameSnaps{record}); err != nil {
		t.Fatal(err)
	}
	if len(client.writes) != 2 || client.writes[0].PutRequest == nil || client.writes[1].DeleteRequest == nil {
		t.Fatalf("writes = %+v", client.writes)
	}
	got, err := repository.PlayerGameSnaps(context.Background(), history.SnapQuery{
		PlayerIDs: []player.ID{"player-1"}, Seasons: []int{2025}, PositionGroups: []string{"LB"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].GameID != "game-1" || got[0].DefenseSnapPct != 0.83 {
		t.Fatalf("records = %+v", got)
	}
}

func TestRepositoryStoresDatasetState(t *testing.T) {
	client := &fakeClient{}
	repository, _ := New(client, "player-game-stats")
	want := history.SnapDatasetState{
		Season: 2025, SourceVersion: "sha256:source", Version: "sha256:abc", RecordCount: 123,
		ImportedAt: time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC),
	}
	if err := repository.PutSnapDatasetState(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	got, err := repository.SnapDatasetState(context.Background(), 2025)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("state = %+v, want %+v", got, want)
	}
}
