// Package s3snapstore persists normalized game-level snap facts in S3.
package s3snapstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
)

type Client interface {
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

type Repository struct {
	client Client
	bucket string
}

type seasonDocument struct {
	SchemaVersion int                       `json:"schema_version"`
	Season        int                       `json:"season"`
	Records       []history.PlayerGameSnaps `json:"records"`
}

func New(client Client, bucket string) (*Repository, error) {
	if client == nil {
		return nil, fmt.Errorf("S3 client is required")
	}
	if strings.TrimSpace(bucket) == "" {
		return nil, fmt.Errorf("S3 snap data bucket is required")
	}
	return &Repository{client: client, bucket: bucket}, nil
}

func NewFromConfig(config aws.Config, bucket string) (*Repository, error) {
	return New(s3.NewFromConfig(config), bucket)
}

func (r *Repository) PutPlayerGameSnaps(ctx context.Context, records []history.PlayerGameSnaps) error {
	if len(records) == 0 {
		return fmt.Errorf("at least one snap record is required")
	}
	season := records[0].Season
	for _, record := range records {
		if err := record.Validate(); err != nil {
			return err
		}
		if record.Season != season {
			return fmt.Errorf("snap records must belong to one season")
		}
	}
	ordered := append([]history.PlayerGameSnaps(nil), records...)
	sortFacts(ordered)
	payload, err := json.Marshal(seasonDocument{SchemaVersion: 1, Season: season, Records: ordered})
	if err != nil {
		return fmt.Errorf("encode snap counts: %w", err)
	}
	_, err = r.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(r.bucket), Key: aws.String(seasonKey(season)), Body: bytes.NewReader(payload), ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("put snap counts in S3: %w", err)
	}
	return nil
}

func (r *Repository) PlayerGameSnaps(ctx context.Context, query history.SnapQuery) ([]history.PlayerGameSnaps, error) {
	if err := query.Validate(); err != nil {
		return nil, err
	}
	players := make(map[player.ID]struct{}, len(query.PlayerIDs))
	for _, id := range query.PlayerIDs {
		players[id] = struct{}{}
	}
	groups := make(map[string]struct{}, len(query.PositionGroups))
	for _, group := range query.PositionGroups {
		groups[strings.ToUpper(strings.TrimSpace(group))] = struct{}{}
	}
	var result []history.PlayerGameSnaps
	for _, season := range query.Seasons {
		document, err := r.getSeason(ctx, season)
		if err != nil {
			return nil, err
		}
		for _, record := range document.Records {
			if _, ok := players[record.PlayerID]; !ok {
				continue
			}
			if len(groups) > 0 {
				if _, ok := groups[strings.ToUpper(record.PositionGroup)]; !ok {
					continue
				}
			}
			result = append(result, record)
		}
	}
	sortFacts(result)
	return result, nil
}

func (r *Repository) getSeason(ctx context.Context, season int) (seasonDocument, error) {
	output, err := r.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(r.bucket), Key: aws.String(seasonKey(season))})
	if err != nil {
		return seasonDocument{}, fmt.Errorf("get %d snap counts from S3: %w", season, err)
	}
	defer output.Body.Close()
	payload, err := io.ReadAll(output.Body)
	if err != nil {
		return seasonDocument{}, fmt.Errorf("read %d snap counts from S3: %w", season, err)
	}
	var document seasonDocument
	if err := json.Unmarshal(payload, &document); err != nil {
		return seasonDocument{}, fmt.Errorf("decode %d snap counts from S3: %w", season, err)
	}
	if document.SchemaVersion != 1 || document.Season != season {
		return seasonDocument{}, fmt.Errorf("snap-count document for %d has an unsupported schema or season", season)
	}
	return document, nil
}

func seasonKey(season int) string {
	return fmt.Sprintf("snap-counts/%d/latest.json", season)
}

func sortFacts(records []history.PlayerGameSnaps) {
	sort.Slice(records, func(i, j int) bool {
		if records[i].Season != records[j].Season {
			return records[i].Season < records[j].Season
		}
		if records[i].Week != records[j].Week {
			return records[i].Week < records[j].Week
		}
		if records[i].GameID != records[j].GameID {
			return records[i].GameID < records[j].GameID
		}
		return records[i].PlayerID < records[j].PlayerID
	})
}
