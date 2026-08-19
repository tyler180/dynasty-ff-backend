// Package s3leaguestore stores immutable, normalized league snapshots in S3.
package s3leaguestore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/tyler180/dynasty-ff-backend/internal/domain/league"
	"github.com/tyler180/dynasty-ff-backend/internal/storage/leaguestore"
)

type Client interface {
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

type Repository struct {
	client Client
	bucket string
}

func New(client Client, bucket string) (*Repository, error) {
	if client == nil {
		return nil, fmt.Errorf("S3 client is required")
	}
	if strings.TrimSpace(bucket) == "" {
		return nil, fmt.Errorf("S3 league data bucket is required")
	}
	return &Repository{client: client, bucket: bucket}, nil
}

func NewFromConfig(config aws.Config, bucket string) (*Repository, error) {
	return New(s3.NewFromConfig(config), bucket)
}

func (r *Repository) PutSnapshot(ctx context.Context, snapshot league.Snapshot) error {
	if err := snapshot.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode league snapshot: %w", err)
	}
	key, err := snapshotKey(snapshot.League.ID, snapshot.Franchise.ID, snapshot.League.Season, snapshot.ObservedAt)
	if err != nil {
		return err
	}
	_, err = r.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(r.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(payload),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("put league snapshot in S3: %w", err)
	}
	return nil
}

func (r *Repository) LatestSnapshot(ctx context.Context, leagueID league.ID, franchiseID league.FranchiseID, season int) (league.Snapshot, error) {
	keys, err := r.snapshotKeys(ctx, leagueID, franchiseID, season)
	if err != nil {
		return league.Snapshot{}, err
	}
	return r.get(ctx, keys[len(keys)-1])
}

func (r *Repository) LatestEnrichedSnapshot(ctx context.Context, leagueID league.ID, franchiseID league.FranchiseID, season int) (league.Snapshot, error) {
	keys, err := r.snapshotKeys(ctx, leagueID, franchiseID, season)
	if err != nil {
		return league.Snapshot{}, err
	}
	for index := len(keys) - 1; index >= 0; index-- {
		snapshot, err := r.get(ctx, keys[index])
		if err != nil {
			return league.Snapshot{}, err
		}
		if len(snapshot.HistoricalPoints.Seasons) > 0 && len(snapshot.ReplacementLevels.CandidatesByPosition) > 0 {
			return snapshot, nil
		}
	}
	return league.Snapshot{}, fmt.Errorf("%w: no enriched snapshot for %s/%s/%d", leaguestore.ErrSnapshotNotFound, leagueID, franchiseID, season)
}

func (r *Repository) snapshotKeys(ctx context.Context, leagueID league.ID, franchiseID league.FranchiseID, season int) ([]string, error) {
	prefix, err := snapshotPrefix(leagueID, franchiseID, season)
	if err != nil {
		return nil, err
	}

	var keys []string
	var continuationToken *string
	for {
		output, err := r.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(r.bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, fmt.Errorf("list league snapshots in S3: %w", err)
		}
		for _, object := range output.Contents {
			if key := aws.ToString(object.Key); key != "" {
				keys = append(keys, key)
			}
		}
		if !aws.ToBool(output.IsTruncated) {
			break
		}
		if output.NextContinuationToken == nil {
			return nil, fmt.Errorf("list league snapshots in S3: truncated response has no continuation token")
		}
		continuationToken = output.NextContinuationToken
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: %s/%s/%d", leaguestore.ErrSnapshotNotFound, leagueID, franchiseID, season)
	}
	sort.Strings(keys)
	return keys, nil
}

func (r *Repository) SnapshotAt(ctx context.Context, leagueID league.ID, franchiseID league.FranchiseID, season int, observedAt time.Time) (league.Snapshot, error) {
	if observedAt.IsZero() {
		return league.Snapshot{}, fmt.Errorf("snapshot observed_at is required")
	}
	prefix, err := snapshotPrefix(leagueID, franchiseID, season)
	if err != nil {
		return league.Snapshot{}, err
	}
	return r.get(ctx, prefix+timestampName(observedAt)+".json")
}

func (r *Repository) get(ctx context.Context, key string) (league.Snapshot, error) {
	output, err := r.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(r.bucket), Key: aws.String(key)})
	if err != nil {
		var notFound *types.NoSuchKey
		if errors.As(err, &notFound) {
			return league.Snapshot{}, fmt.Errorf("%w: %s", leaguestore.ErrSnapshotNotFound, key)
		}
		return league.Snapshot{}, fmt.Errorf("get league snapshot from S3: %w", err)
	}
	defer output.Body.Close()
	payload, err := io.ReadAll(output.Body)
	if err != nil {
		return league.Snapshot{}, fmt.Errorf("read league snapshot from S3: %w", err)
	}
	var snapshot league.Snapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return league.Snapshot{}, fmt.Errorf("decode league snapshot from S3: %w", err)
	}
	if err := snapshot.Validate(); err != nil {
		return league.Snapshot{}, fmt.Errorf("validate league snapshot from S3: %w", err)
	}
	return snapshot, nil
}

func snapshotPrefix(leagueID league.ID, franchiseID league.FranchiseID, season int) (string, error) {
	leagueSegment := strings.TrimSpace(string(leagueID))
	franchiseSegment := strings.TrimSpace(string(franchiseID))
	if leagueSegment == "" || franchiseSegment == "" {
		return "", fmt.Errorf("league and franchise IDs are required")
	}
	if !safeSegment(leagueSegment) || !safeSegment(franchiseSegment) {
		return "", fmt.Errorf("league and franchise IDs cannot contain path separators")
	}
	if season < 2000 || season > 2100 {
		return "", fmt.Errorf("league season is invalid")
	}
	return path.Join("snapshots", fmt.Sprintf("%d", season), leagueSegment, franchiseSegment) + "/", nil
}

func snapshotKey(leagueID league.ID, franchiseID league.FranchiseID, season int, observedAt time.Time) (string, error) {
	prefix, err := snapshotPrefix(leagueID, franchiseID, season)
	if err != nil {
		return "", err
	}
	return prefix + timestampName(observedAt) + ".json", nil
}

func timestampName(value time.Time) string {
	return value.UTC().Format("20060102T150405.000000000Z")
}

func safeSegment(value string) bool {
	return value != "." && value != ".." && !strings.ContainsAny(value, `/\`)
}
