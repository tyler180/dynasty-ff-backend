// Package snapstore composes the queryable player-game store with the
// versioned S3 archive used for recovery and migrations.
package snapstore

import (
	"context"
	"fmt"
	"sort"

	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
)

type Writer struct {
	Archive history.SnapWriter
	Primary history.SnapWriter
}

func (w Writer) PutPlayerGameSnaps(ctx context.Context, records []history.PlayerGameSnaps) error {
	if w.Archive == nil || w.Primary == nil {
		return fmt.Errorf("snap archive and primary store are required")
	}
	if err := w.Archive.PutPlayerGameSnaps(ctx, records); err != nil {
		return fmt.Errorf("archive player-game stats: %w", err)
	}
	if err := w.Primary.PutPlayerGameSnaps(ctx, records); err != nil {
		return fmt.Errorf("index player-game stats: %w", err)
	}
	return nil
}

// Reader uses DynamoDB first and fills seasons not yet migrated from S3. This
// keeps existing data readable while historical seasons are resynchronized.
type Reader struct {
	Primary  history.SnapReader
	Fallback history.SnapReader
	State    history.SnapDatasetStateStore
}

func (r Reader) PlayerGameSnaps(ctx context.Context, query history.SnapQuery) ([]history.PlayerGameSnaps, error) {
	if r.Primary == nil || r.Fallback == nil {
		return nil, fmt.Errorf("primary and fallback snap readers are required")
	}
	if r.State != nil {
		return r.readByMigrationState(ctx, query)
	}
	indexed, err := r.Primary.PlayerGameSnaps(ctx, query)
	if err != nil {
		return nil, err
	}
	present := make(map[int]struct{})
	for _, record := range indexed {
		present[record.Season] = struct{}{}
	}
	missing := make([]int, 0, len(query.Seasons))
	for _, season := range query.Seasons {
		if _, ok := present[season]; !ok {
			missing = append(missing, season)
		}
	}
	if len(missing) > 0 {
		fallbackQuery := query
		fallbackQuery.Seasons = missing
		archived, err := r.Fallback.PlayerGameSnaps(ctx, fallbackQuery)
		if err != nil {
			return nil, err
		}
		indexed = append(indexed, archived...)
	}
	sortRecords(indexed)
	return indexed, nil
}

func (r Reader) readByMigrationState(ctx context.Context, query history.SnapQuery) ([]history.PlayerGameSnaps, error) {
	if err := query.Validate(); err != nil {
		return nil, err
	}
	var migrated, archived []int
	for _, season := range query.Seasons {
		state, err := r.State.SnapDatasetState(ctx, season)
		if err != nil {
			return nil, err
		}
		if state.Version == "" {
			archived = append(archived, season)
		} else {
			migrated = append(migrated, season)
		}
	}
	var result []history.PlayerGameSnaps
	if len(migrated) > 0 {
		primaryQuery := query
		primaryQuery.Seasons = migrated
		records, err := r.Primary.PlayerGameSnaps(ctx, primaryQuery)
		if err != nil {
			return nil, err
		}
		result = append(result, records...)
	}
	if len(archived) > 0 {
		fallbackQuery := query
		fallbackQuery.Seasons = archived
		records, err := r.Fallback.PlayerGameSnaps(ctx, fallbackQuery)
		if err != nil {
			return nil, err
		}
		result = append(result, records...)
	}
	sortRecords(result)
	return result, nil
}

func sortRecords(records []history.PlayerGameSnaps) {
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

var _ history.SnapWriter = Writer{}
var _ history.SnapReader = Reader{}
