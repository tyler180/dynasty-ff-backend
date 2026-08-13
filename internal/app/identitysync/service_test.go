package identitysync

import (
	"context"
	"testing"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/identity"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
	"github.com/tyler180/dynasty-ff-backend/internal/provider/dynastyprocess"
)

type fakeSource struct{ players []dynastyprocess.Player }

func (f fakeSource) Players(context.Context) ([]dynastyprocess.Player, error) { return f.players, nil }

type fakeRelevantPlayers map[string]struct{}

func (f fakeRelevantPlayers) PlayerIDs(context.Context, int, string) (map[string]struct{}, error) {
	return f, nil
}

type fakeRepository struct {
	profiles map[player.ID]player.Profile
	aliases  map[player.ExternalID]player.ID
}

func (f *fakeRepository) GetPlayer(_ context.Context, id player.ID) (player.Profile, error) {
	return f.profiles[id], nil
}
func (f *fakeRepository) ResolvePlayer(_ context.Context, id player.ExternalID) (player.Profile, error) {
	return f.profiles[f.aliases[id]], nil
}
func (f *fakeRepository) ResolvePlayers(_ context.Context, ids []player.ExternalID) (map[player.ExternalID]player.Profile, error) {
	result := map[player.ExternalID]player.Profile{}
	for _, id := range ids {
		if playerID, ok := f.aliases[id]; ok {
			result[id] = f.profiles[playerID]
		}
	}
	return result, nil
}
func (f *fakeRepository) PutPlayer(_ context.Context, profile player.Profile) error {
	f.profiles[profile.ID] = profile
	return nil
}
func (f *fakeRepository) PutAlias(_ context.Context, alias identity.Alias) error {
	if existing, ok := f.aliases[alias.ExternalID]; ok && existing != alias.PlayerID {
		return identity.ErrAliasConflict
	}
	f.aliases[alias.ExternalID] = alias.PlayerID
	return nil
}

func TestSyncPreservesManualCanonicalIdentity(t *testing.T) {
	manualID := player.ID("player-josh-allen-qb")
	mflID := player.ExternalID{Provider: player.ProviderMFL, Value: "13589"}
	repository := &fakeRepository{
		profiles: map[player.ID]player.Profile{manualID: {ID: manualID, DisplayName: "Josh Allen"}},
		aliases:  map[player.ExternalID]player.ID{mflID: manualID},
	}
	service := Service{
		Source: fakeSource{players: []dynastyprocess.Player{{
			Name: "Joshua Allen", MFLID: "13589", GSISID: "00-0034857", Season: 2026,
			BirthDate: "1996-05-21", DraftYear: 2018, DraftRound: 1, DraftPick: 7,
		}}},
		Repository: repository, BulkResolver: repository, RelevantPlayers: fakeRelevantPlayers{"13589": {}},
		Now: func() time.Time { return time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC) },
	}
	result, err := service.Sync(context.Background(), Request{Year: 2026, LeagueID: "79286", Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExistingPlayers != 1 || result.CreatedPlayers != 0 {
		t.Fatalf("result = %+v", result)
	}
	gsisID := player.ExternalID{Provider: player.ProviderGSIS, Value: "00-0034857"}
	if repository.aliases[gsisID] != manualID {
		t.Fatalf("GSIS alias points to %q, want %q", repository.aliases[gsisID], manualID)
	}
	if repository.profiles[manualID].DisplayName != "Josh Allen" {
		t.Fatalf("manual display name was overwritten")
	}
}

func TestCanonicalIDIsStableAndOpaque(t *testing.T) {
	record := dynastyprocess.Player{MFLID: "13589", GSISID: "00-0034857"}
	first := canonicalID(record)
	second := canonicalID(record)
	if first != second || first == "" || first == player.ID(record.GSISID) {
		t.Fatalf("canonical IDs = %q and %q", first, second)
	}
}
