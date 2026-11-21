package freeagenttrends

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
	"github.com/tyler180/dynasty-ff-backend/internal/storage/s3snapstore"
)

// objectStore stands in for S3, including NoSuchKey for objects never written.
type objectStore map[string][]byte

func (o objectStore) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	payload, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	o[aws.ToString(input.Key)] = payload
	return &s3.PutObjectOutput{}, nil
}

func (o objectStore) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	payload, ok := o[aws.ToString(input.Key)]
	if !ok {
		return nil, &types.NoSuchKey{Message: aws.String("The specified key does not exist.")}
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(payload))}, nil
}

// Between seasons the league season has no snap data stored yet. The default
// query includes it, so it must fall back to the seasons that are stored.
func TestAnalyzeDefaultSeasonsWhenLeagueSeasonNotStored(t *testing.T) {
	repository, err := s3snapstore.New(objectStore{}, "league-data")
	if err != nil {
		t.Fatal(err)
	}
	// Each put replaces a season's document, so store the whole season at once.
	var season2025 []history.PlayerGameSnaps
	for week, count := range []int{10, 12, 14, 50, 52, 54} {
		fact := snapFact("player-rising", week+1, count)
		fact.Source = "nflverse-pfr"
		season2025 = append(season2025, fact)
	}
	if err := repository.PutPlayerGameSnaps(context.Background(), season2025); err != nil {
		t.Fatal(err)
	}
	available := fakeFreeAgents{{MFLID: "100", Name: "Rising Linebacker", Position: "LB", PositionGroup: "LB"}}
	identities := fakeIdentities{
		{Provider: player.ProviderMFL, Value: "100"}: {ID: "player-rising", DisplayName: "Rising Linebacker"},
	}

	// League season 2027 before any 2027 games: defaults to 2025-2027, only 2025 is stored.
	result, err := (Service{FreeAgents: available, Identities: identities, Snaps: repository}).Analyze(context.Background(), Request{
		Year: 2027, LeagueID: "79286",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trends) != 1 || result.Trends[0].LatestSeason != 2025 {
		t.Fatalf("trends = %+v", result.Trends)
	}
}
