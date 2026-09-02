package snapstore

import (
	"context"
	"testing"

	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
)

type memoryStore struct {
	records []history.PlayerGameSnaps
	writes  int
}

type migrationState map[int]string

func (s migrationState) SnapDatasetState(_ context.Context, season int) (history.SnapDatasetState, error) {
	return history.SnapDatasetState{Season: season, Version: s[season]}, nil
}

func (s migrationState) PutSnapDatasetState(context.Context, history.SnapDatasetState) error {
	return nil
}

func (s *memoryStore) PutPlayerGameSnaps(_ context.Context, records []history.PlayerGameSnaps) error {
	s.writes++
	s.records = append([]history.PlayerGameSnaps(nil), records...)
	return nil
}

func TestReaderUsesStateToAvoidPartiallyMigratedSeason(t *testing.T) {
	primary := &memoryStore{records: []history.PlayerGameSnaps{{PlayerID: "p1", GameID: "partial", Season: 2024, Week: 1, Source: "test"}}}
	archive := &memoryStore{records: []history.PlayerGameSnaps{{PlayerID: "p1", GameID: "complete", Season: 2024, Week: 1, Source: "test"}}}
	got, err := (Reader{Primary: primary, Fallback: archive, State: migrationState{}}).PlayerGameSnaps(context.Background(), history.SnapQuery{
		PlayerIDs: []player.ID{"p1"}, Seasons: []int{2024},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].GameID != "complete" {
		t.Fatalf("records = %+v", got)
	}
}

func (s *memoryStore) PlayerGameSnaps(_ context.Context, query history.SnapQuery) ([]history.PlayerGameSnaps, error) {
	var result []history.PlayerGameSnaps
	for _, record := range s.records {
		for _, season := range query.Seasons {
			if record.Season == season {
				result = append(result, record)
			}
		}
	}
	return result, nil
}

func TestWriterArchivesAndIndexes(t *testing.T) {
	archive, primary := &memoryStore{}, &memoryStore{}
	records := []history.PlayerGameSnaps{{PlayerID: "p1", GameID: "g1", Season: 2025, Week: 1, Source: "test"}}
	if err := (Writer{Archive: archive, Primary: primary}).PutPlayerGameSnaps(context.Background(), records); err != nil {
		t.Fatal(err)
	}
	if archive.writes != 1 || primary.writes != 1 {
		t.Fatalf("archive writes = %d, primary writes = %d", archive.writes, primary.writes)
	}
}

func TestReaderFallsBackOnlyForUnmigratedSeasons(t *testing.T) {
	primary := &memoryStore{records: []history.PlayerGameSnaps{{PlayerID: "p1", GameID: "new", Season: 2025, Week: 1, Source: "test"}}}
	archive := &memoryStore{records: []history.PlayerGameSnaps{
		{PlayerID: "p1", GameID: "old", Season: 2024, Week: 1, Source: "test"},
		{PlayerID: "p1", GameID: "duplicate", Season: 2025, Week: 1, Source: "test"},
	}}
	got, err := (Reader{Primary: primary, Fallback: archive}).PlayerGameSnaps(context.Background(), history.SnapQuery{
		PlayerIDs: []player.ID{"p1"}, Seasons: []int{2024, 2025},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].GameID != "old" || got[1].GameID != "new" {
		t.Fatalf("records = %+v", got)
	}
}
