package s3snapstore

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
)

type fakeS3 struct{ objects map[string][]byte }

func (f *fakeS3) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	payload, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	f.objects[aws.ToString(input.Key)] = payload
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(f.objects[aws.ToString(input.Key)]))}, nil
}

func TestRepositoryWritesAndQueriesCanonicalSnapFacts(t *testing.T) {
	client := &fakeS3{objects: map[string][]byte{}}
	repository, err := New(client, "league-data")
	if err != nil {
		t.Fatal(err)
	}
	records := []history.PlayerGameSnaps{
		{PlayerID: "player-2", GameID: "2025_02_PHI_KC", Season: 2025, Week: 2, PositionGroup: "DB", DefenseSnaps: 40, DefenseSnapPct: 0.65, Source: "nflverse-pfr"},
		{PlayerID: "player-1", GameID: "2025_01_DAL_PHI", Season: 2025, Week: 1, PositionGroup: "LB", DefenseSnaps: 52, DefenseSnapPct: 0.83, Source: "nflverse-pfr"},
	}
	if err := repository.PutPlayerGameSnaps(context.Background(), records); err != nil {
		t.Fatal(err)
	}
	if _, ok := client.objects["snap-counts/2025/latest.json"]; !ok {
		t.Fatalf("stored keys = %v", client.objects)
	}
	got, err := repository.PlayerGameSnaps(context.Background(), history.SnapQuery{
		PlayerIDs: []player.ID{"player-1"}, Seasons: []int{2025}, PositionGroups: []string{"lb"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PlayerID != "player-1" || got[0].DefenseSnapPct != 0.83 {
		t.Fatalf("snap facts = %+v", got)
	}
}
