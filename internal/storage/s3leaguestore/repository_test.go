package s3leaguestore

import (
	"bytes"
	"context"
	"io"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/tyler180/dynasty-ff-backend/internal/domain/league"
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
	payload, ok := f.objects[aws.ToString(input.Key)]
	if !ok {
		return nil, &types.NoSuchKey{}
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(payload))}, nil
}

func (f *fakeS3) ListObjectsV2(_ context.Context, input *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	var keys []string
	for key := range f.objects {
		if strings.HasPrefix(key, aws.ToString(input.Prefix)) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	output := &s3.ListObjectsV2Output{}
	for _, key := range keys {
		output.Contents = append(output.Contents, types.Object{Key: aws.String(key)})
	}
	return output, nil
}

func TestRepositoryPutAndReadSnapshots(t *testing.T) {
	client := &fakeS3{objects: map[string][]byte{}}
	repository, err := New(client, "league-data")
	if err != nil {
		t.Fatal(err)
	}
	first := testSnapshot(time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC))
	latest := testSnapshot(time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC))
	for _, snapshot := range []league.Snapshot{latest, first} {
		if err := repository.PutSnapshot(context.Background(), snapshot); err != nil {
			t.Fatal(err)
		}
	}

	gotLatest, err := repository.LatestSnapshot(context.Background(), "79286", "0005", 2026)
	if err != nil {
		t.Fatal(err)
	}
	if !gotLatest.ObservedAt.Equal(latest.ObservedAt) {
		t.Fatalf("latest observed_at = %s, want %s", gotLatest.ObservedAt, latest.ObservedAt)
	}
	gotFirst, err := repository.SnapshotAt(context.Background(), "79286", "0005", 2026, first.ObservedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !gotFirst.ObservedAt.Equal(first.ObservedAt) {
		t.Fatalf("snapshot observed_at = %s, want %s", gotFirst.ObservedAt, first.ObservedAt)
	}
}

func testSnapshot(observedAt time.Time) league.Snapshot {
	return league.Snapshot{
		League:     league.League{ID: "79286", Name: "Test", Season: 2026},
		Franchise:  league.Franchise{ID: "0005", Name: "Team McLean"},
		ObservedAt: observedAt,
		Source:     "test",
	}
}
