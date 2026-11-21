package s3snapstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
)

type fakeS3 struct {
	objects map[string][]byte
	// failures makes GetObject fail for a key with a non-"not found" error.
	failures map[string]error
}

func (f *fakeS3) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	payload, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	f.objects[aws.ToString(input.Key)] = payload
	return &s3.PutObjectOutput{}, nil
}

// GetObject behaves like S3 for a caller with s3:ListBucket: a missing key is
// a NoSuchKey error rather than an empty body.
func (f *fakeS3) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	key := aws.ToString(input.Key)
	if err := f.failures[key]; err != nil {
		return nil, err
	}
	payload, ok := f.objects[key]
	if !ok {
		return nil, &types.NoSuchKey{Message: aws.String("The specified key does not exist.")}
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(payload))}, nil
}

func TestRepositoryWritesAndQueriesCanonicalSnapFacts(t *testing.T) {
	client := &fakeS3{objects: map[string][]byte{}}
	repository, err := New(client, "league-data")
	if err != nil {
		t.Fatal(err)
	}
	records := []history.PlayerGameSnaps{
		{PlayerID: "player-2", GameID: "2025_02_PHI_KC", Season: 2025, Week: 2, PositionGroup: "DB", DefenseSnaps: 40, DefenseSnapPct: 0.65, Source: "nflverse-pfr"},
		{PlayerID: "player-1", GameID: "2025_01_DAL_PHI", Season: 2025, Week: 1, PositionGroup: "LB", DefenseSnaps: 52, TeamDefenseSnaps: 63, DefenseSnapPct: 0.83, Source: "nflverse-pfr"},
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

func TestRepositoryArchivesRawSourceByContentVersion(t *testing.T) {
	client := &fakeS3{objects: map[string][]byte{}}
	repository, _ := New(client, "league-data")
	if err := repository.PutSourceFile(context.Background(), history.SourceFile{
		Dataset: "nflverse-snap-counts", Season: 2025, Version: "sha256:abc123",
		SourceURL: "https://example.test/snap.csv", ContentType: "text/csv", Payload: []byte("header\nvalue\n"),
	}); err != nil {
		t.Fatal(err)
	}
	key := "source-data/nflverse-snap-counts/2025/abc123.csv"
	if string(client.objects[key]) != "header\nvalue\n" {
		t.Fatalf("stored objects = %+v", client.objects)
	}
}

func TestRepositoryTreatsMissingSeasonAsEmpty(t *testing.T) {
	stored := []history.PlayerGameSnaps{
		{PlayerID: "player-1", GameID: "2025_18_DAL_PHI", Season: 2025, Week: 18, PositionGroup: "LB", DefenseSnaps: 52, TeamDefenseSnaps: 63, DefenseSnapPct: 0.83, Source: "nflverse-pfr"},
	}
	query := history.SnapQuery{PlayerIDs: []player.ID{"player-1"}, Seasons: []int{2025, 2026}}

	tests := []struct {
		name      string
		failures  map[string]error
		wantFacts int
		wantErr   string
	}{
		{name: "season not stored yet contributes nothing", wantFacts: 1},
		{
			name:     "other S3 errors still fail",
			failures: map[string]error{"snap-counts/2026/latest.json": errors.New("AccessDenied: Access Denied")},
			wantErr:  "get 2026 snap counts from S3",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeS3{objects: map[string][]byte{}, failures: test.failures}
			repository, err := New(client, "league-data")
			if err != nil {
				t.Fatal(err)
			}
			if err := repository.PutPlayerGameSnaps(context.Background(), stored); err != nil {
				t.Fatal(err)
			}
			got, err := repository.PlayerGameSnaps(context.Background(), query)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error = %v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != test.wantFacts {
				t.Fatalf("facts = %+v", got)
			}
		})
	}
}
