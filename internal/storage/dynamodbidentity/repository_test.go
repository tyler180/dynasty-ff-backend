package dynamodbidentity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/tyler180/dynasty-ff-backend/internal/identity"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
)

type fakeDynamoClient struct {
	items    map[string]map[string]types.AttributeValue
	puts     []*dynamodb.PutItemInput
	putError error
}

func newFakeDynamoClient() *fakeDynamoClient {
	return &fakeDynamoClient{items: make(map[string]map[string]types.AttributeValue)}
}

func (f *fakeDynamoClient) GetItem(_ context.Context, input *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	return &dynamodb.GetItemOutput{Item: f.items[itemKey(input.Key)]}, nil
}

func (f *fakeDynamoClient) PutItem(_ context.Context, input *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	f.puts = append(f.puts, input)
	if f.putError != nil {
		return nil, f.putError
	}
	f.items[itemKey(input.Item)] = input.Item
	return &dynamodb.PutItemOutput{}, nil
}

func itemKey(item map[string]types.AttributeValue) string {
	return optionalString(item, "pk") + "|" + optionalString(item, "sk")
}

func TestRepositoryStoresAndResolvesCanonicalPlayer(t *testing.T) {
	client := newFakeDynamoClient()
	repository, err := New(client, "player-identity")
	if err != nil {
		t.Fatal(err)
	}
	birthDate := time.Date(2001, 7, 24, 0, 0, 0, 0, time.UTC)
	profile := player.Profile{
		ID: "player-123", DisplayName: "Drake London", BirthDate: &birthDate, RookieYear: 2022,
		Draft: &player.DraftRecord{Year: 2022, Round: 1, Pick: 8, NFLTeam: "ATL"},
	}
	if err := repository.PutPlayer(context.Background(), profile); err != nil {
		t.Fatal(err)
	}
	alias := identity.Alias{
		ExternalID:       player.ExternalID{Provider: player.ProviderMFL, Value: "15751"},
		PlayerID:         profile.ID,
		Source:           "nflverse",
		ResolutionMethod: "provider_crosswalk",
		Confidence:       1,
	}
	if err := repository.PutAlias(context.Background(), alias); err != nil {
		t.Fatal(err)
	}

	resolved, err := repository.ResolvePlayer(context.Background(), alias.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != profile.ID || resolved.DisplayName != profile.DisplayName {
		t.Fatalf("resolved profile = %+v", resolved)
	}
	if resolved.BirthDate == nil || !resolved.BirthDate.Equal(birthDate) {
		t.Fatalf("resolved birth date = %v", resolved.BirthDate)
	}
	if resolved.Draft == nil || resolved.Draft.Pick != 8 {
		t.Fatalf("resolved draft record = %+v", resolved.Draft)
	}
	if client.puts[1].ConditionExpression == nil {
		t.Fatal("automatic alias write did not protect an existing resolution")
	}
}

func TestManualAliasCanReplaceAnAutomaticResolution(t *testing.T) {
	client := newFakeDynamoClient()
	repository, err := New(client, "player-identity")
	if err != nil {
		t.Fatal(err)
	}
	err = repository.PutAlias(context.Background(), identity.Alias{
		ExternalID:       player.ExternalID{Provider: player.ProviderPFR, Value: "LondDr00"},
		PlayerID:         "player-123",
		Source:           "manual",
		ResolutionMethod: "manual_review",
		ManualOverride:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.puts[0].ConditionExpression != nil {
		t.Fatal("manual resolution was written with an automatic conflict condition")
	}
}

func TestAliasConflictReturnsDomainError(t *testing.T) {
	client := newFakeDynamoClient()
	client.putError = &types.ConditionalCheckFailedException{}
	repository, err := New(client, "player-identity")
	if err != nil {
		t.Fatal(err)
	}
	err = repository.PutAlias(context.Background(), identity.Alias{
		ExternalID:       player.ExternalID{Provider: player.ProviderMFL, Value: "15751"},
		PlayerID:         "player-456",
		Source:           "nflverse",
		ResolutionMethod: "provider_crosswalk",
	})
	if !errors.Is(err, identity.ErrAliasConflict) {
		t.Fatalf("error = %v, want ErrAliasConflict", err)
	}
}

func TestMissingAliasReturnsPlayerNotFound(t *testing.T) {
	repository, err := New(newFakeDynamoClient(), "player-identity")
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.ResolvePlayer(context.Background(), player.ExternalID{Provider: player.ProviderMFL, Value: "missing"})
	if !errors.Is(err, identity.ErrPlayerNotFound) {
		t.Fatalf("error = %v, want ErrPlayerNotFound", err)
	}
}
