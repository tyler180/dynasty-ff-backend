// Package dynamodbidentity implements canonical player identity storage in
// DynamoDB. The table uses string partition and sort keys named pk and sk.
package dynamodbidentity

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/tyler180/dynasty-ff-backend/internal/identity"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
)

const profileSortKey = "PROFILE"

type Client interface {
	GetItem(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	PutItem(context.Context, *dynamodb.PutItemInput, ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
}

type BatchClient interface {
	BatchGetItem(context.Context, *dynamodb.BatchGetItemInput, ...func(*dynamodb.Options)) (*dynamodb.BatchGetItemOutput, error)
}

type Repository struct {
	client    Client
	tableName string
	now       func() time.Time
}

func New(client Client, tableName string) (*Repository, error) {
	if client == nil {
		return nil, fmt.Errorf("DynamoDB client is required")
	}
	if strings.TrimSpace(tableName) == "" {
		return nil, fmt.Errorf("DynamoDB identity table name is required")
	}
	return &Repository{client: client, tableName: tableName, now: time.Now}, nil
}

func NewFromConfig(config aws.Config, tableName string) (*Repository, error) {
	return New(dynamodb.NewFromConfig(config), tableName)
}

func (r *Repository) GetPlayer(ctx context.Context, id player.ID) (player.Profile, error) {
	if strings.TrimSpace(string(id)) == "" {
		return player.Profile{}, fmt.Errorf("canonical player ID is required")
	}
	output, err := r.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(r.tableName),
		ConsistentRead: aws.Bool(true),
		Key:            key(playerPartitionKey(id), profileSortKey),
	})
	if err != nil {
		return player.Profile{}, fmt.Errorf("get player %s from DynamoDB: %w", id, err)
	}
	if len(output.Item) == 0 {
		return player.Profile{}, fmt.Errorf("%w: %s", identity.ErrPlayerNotFound, id)
	}
	profile, err := decodeProfile(output.Item)
	if err != nil {
		return player.Profile{}, fmt.Errorf("decode player %s from DynamoDB: %w", id, err)
	}
	return profile, nil
}

func (r *Repository) ResolvePlayer(ctx context.Context, externalID player.ExternalID) (player.Profile, error) {
	if err := externalID.Validate(); err != nil {
		return player.Profile{}, err
	}
	output, err := r.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(r.tableName),
		ConsistentRead: aws.Bool(true),
		Key:            key(aliasPartitionKey(externalID), profileSortKey),
	})
	if err != nil {
		return player.Profile{}, fmt.Errorf("resolve %s player %s from DynamoDB: %w", externalID.Provider, externalID.Value, err)
	}
	if len(output.Item) == 0 {
		return player.Profile{}, fmt.Errorf("%w: %s#%s", identity.ErrPlayerNotFound, externalID.Provider, externalID.Value)
	}
	playerID, err := requiredString(output.Item, "player_id")
	if err != nil {
		return player.Profile{}, fmt.Errorf("decode alias %s#%s: %w", externalID.Provider, externalID.Value, err)
	}
	return r.GetPlayer(ctx, player.ID(playerID))
}

func (r *Repository) ResolvePlayers(ctx context.Context, externalIDs []player.ExternalID) (map[player.ExternalID]player.Profile, error) {
	client, ok := r.client.(BatchClient)
	if !ok {
		return nil, fmt.Errorf("DynamoDB client does not support batch reads")
	}
	result := make(map[player.ExternalID]player.Profile)
	if len(externalIDs) == 0 {
		return result, nil
	}

	unique := make(map[player.ExternalID]struct{}, len(externalIDs))
	for _, externalID := range externalIDs {
		if err := externalID.Validate(); err != nil {
			return nil, err
		}
		unique[externalID] = struct{}{}
	}
	aliases := make([]player.ExternalID, 0, len(unique))
	for externalID := range unique {
		aliases = append(aliases, externalID)
	}

	aliasToPlayer := make(map[player.ExternalID]player.ID)
	for start := 0; start < len(aliases); start += 100 {
		end := min(start+100, len(aliases))
		keys := make([]map[string]types.AttributeValue, 0, end-start)
		for _, externalID := range aliases[start:end] {
			keys = append(keys, key(aliasPartitionKey(externalID), profileSortKey))
		}
		items, err := batchGet(ctx, client, r.tableName, keys)
		if err != nil {
			return nil, fmt.Errorf("batch resolve player aliases: %w", err)
		}
		for _, item := range items {
			providerName := optionalString(item, "provider")
			externalValue := optionalString(item, "external_id")
			playerID := optionalString(item, "player_id")
			if providerName != "" && externalValue != "" && playerID != "" {
				aliasToPlayer[player.ExternalID{Provider: player.Provider(providerName), Value: externalValue}] = player.ID(playerID)
			}
		}
	}

	playerIDs := make(map[player.ID]struct{}, len(aliasToPlayer))
	for _, playerID := range aliasToPlayer {
		playerIDs[playerID] = struct{}{}
	}
	profiles := make(map[player.ID]player.Profile, len(playerIDs))
	ids := make([]player.ID, 0, len(playerIDs))
	for playerID := range playerIDs {
		ids = append(ids, playerID)
	}
	for start := 0; start < len(ids); start += 100 {
		end := min(start+100, len(ids))
		keys := make([]map[string]types.AttributeValue, 0, end-start)
		for _, playerID := range ids[start:end] {
			keys = append(keys, key(playerPartitionKey(playerID), profileSortKey))
		}
		items, err := batchGet(ctx, client, r.tableName, keys)
		if err != nil {
			return nil, fmt.Errorf("batch get player profiles: %w", err)
		}
		for _, item := range items {
			profile, err := decodeProfile(item)
			if err != nil {
				return nil, err
			}
			profiles[profile.ID] = profile
		}
	}
	for externalID, playerID := range aliasToPlayer {
		profile, ok := profiles[playerID]
		if !ok {
			return nil, fmt.Errorf("alias %s#%s references missing player %s", externalID.Provider, externalID.Value, playerID)
		}
		result[externalID] = profile
	}
	return result, nil
}

func batchGet(ctx context.Context, client BatchClient, tableName string, keys []map[string]types.AttributeValue) ([]map[string]types.AttributeValue, error) {
	requestItems := map[string]types.KeysAndAttributes{tableName: {Keys: keys, ConsistentRead: aws.Bool(true)}}
	var items []map[string]types.AttributeValue
	for attempts := 0; len(requestItems) > 0; attempts++ {
		if attempts == 8 {
			return nil, fmt.Errorf("DynamoDB left batch keys unprocessed after retries")
		}
		output, err := client.BatchGetItem(ctx, &dynamodb.BatchGetItemInput{RequestItems: requestItems})
		if err != nil {
			return nil, err
		}
		items = append(items, output.Responses[tableName]...)
		requestItems = output.UnprocessedKeys
		if len(requestItems) > 0 {
			time.Sleep(time.Duration(1<<attempts) * 10 * time.Millisecond)
		}
	}
	return items, nil
}

func (r *Repository) PutPlayer(ctx context.Context, profile player.Profile) error {
	if err := profile.Validate(); err != nil {
		return err
	}
	item := map[string]types.AttributeValue{
		"pk":           stringValue(playerPartitionKey(profile.ID)),
		"sk":           stringValue(profileSortKey),
		"entity_type":  stringValue("player_profile"),
		"player_id":    stringValue(string(profile.ID)),
		"display_name": stringValue(profile.DisplayName),
	}
	if profile.BirthDate != nil {
		item["birth_date"] = stringValue(profile.BirthDate.UTC().Format("2006-01-02"))
	}
	if profile.RookieYear != 0 {
		item["rookie_year"] = numberValue(profile.RookieYear)
	}
	if profile.Draft != nil {
		item["draft_year"] = numberValue(profile.Draft.Year)
		if profile.Draft.Round != 0 {
			item["draft_round"] = numberValue(profile.Draft.Round)
		}
		if profile.Draft.Pick != 0 {
			item["draft_pick"] = numberValue(profile.Draft.Pick)
		}
		if profile.Draft.NFLTeam != "" {
			item["draft_nfl_team"] = stringValue(profile.Draft.NFLTeam)
		}
	}
	_, err := r.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(r.tableName),
		Item:      item,
	})
	if err != nil {
		return fmt.Errorf("put player %s in DynamoDB: %w", profile.ID, err)
	}
	return nil
}

func (r *Repository) PutAlias(ctx context.Context, alias identity.Alias) error {
	if err := alias.Validate(); err != nil {
		return err
	}
	if alias.IngestedAt.IsZero() {
		alias.IngestedAt = r.now().UTC()
	}
	item := map[string]types.AttributeValue{
		"pk":                stringValue(aliasPartitionKey(alias.ExternalID)),
		"sk":                stringValue(profileSortKey),
		"entity_type":       stringValue("player_alias"),
		"provider":          stringValue(string(alias.ExternalID.Provider)),
		"external_id":       stringValue(alias.ExternalID.Value),
		"player_id":         stringValue(string(alias.PlayerID)),
		"source":            stringValue(alias.Source),
		"resolution_method": stringValue(alias.ResolutionMethod),
		"manual_override":   boolValue(alias.ManualOverride),
		"ingested_at":       stringValue(alias.IngestedAt.UTC().Format(time.RFC3339Nano)),
	}
	if alias.Confidence != 0 {
		item["confidence"] = &types.AttributeValueMemberN{Value: strconv.FormatFloat(alias.Confidence, 'f', -1, 64)}
	}
	if alias.SourceUpdatedAt != nil {
		item["source_updated_at"] = stringValue(alias.SourceUpdatedAt.UTC().Format(time.RFC3339Nano))
	}

	input := &dynamodb.PutItemInput{TableName: aws.String(r.tableName), Item: item}
	if !alias.ManualOverride {
		input.ConditionExpression = aws.String("attribute_not_exists(pk) OR (player_id = :player_id AND (attribute_not_exists(manual_override) OR manual_override = :false))")
		input.ExpressionAttributeValues = map[string]types.AttributeValue{
			":player_id": stringValue(string(alias.PlayerID)),
			":false":     boolValue(false),
		}
	}
	_, err := r.client.PutItem(ctx, input)
	if err != nil {
		var conflict *types.ConditionalCheckFailedException
		if errors.As(err, &conflict) {
			return fmt.Errorf("%w: %s#%s", identity.ErrAliasConflict, alias.ExternalID.Provider, alias.ExternalID.Value)
		}
		return fmt.Errorf("put %s player alias %s in DynamoDB: %w", alias.ExternalID.Provider, alias.ExternalID.Value, err)
	}
	return nil
}

func playerPartitionKey(id player.ID) string {
	return "PLAYER#" + string(id)
}

func aliasPartitionKey(id player.ExternalID) string {
	return "ALIAS#" + strings.ToUpper(string(id.Provider)) + "#" + id.Value
}

func key(partitionKey, sortKey string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"pk": stringValue(partitionKey),
		"sk": stringValue(sortKey),
	}
}

func stringValue(value string) types.AttributeValue {
	return &types.AttributeValueMemberS{Value: value}
}

func numberValue(value int) types.AttributeValue {
	return &types.AttributeValueMemberN{Value: strconv.Itoa(value)}
}

func boolValue(value bool) types.AttributeValue {
	return &types.AttributeValueMemberBOOL{Value: value}
}

func decodeProfile(item map[string]types.AttributeValue) (player.Profile, error) {
	id, err := requiredString(item, "player_id")
	if err != nil {
		return player.Profile{}, err
	}
	name, err := requiredString(item, "display_name")
	if err != nil {
		return player.Profile{}, err
	}
	profile := player.Profile{ID: player.ID(id), DisplayName: name}
	if value := optionalString(item, "birth_date"); value != "" {
		birthDate, err := time.Parse("2006-01-02", value)
		if err != nil {
			return player.Profile{}, fmt.Errorf("birth_date: %w", err)
		}
		profile.BirthDate = &birthDate
	}
	profile.RookieYear, err = optionalInt(item, "rookie_year")
	if err != nil {
		return player.Profile{}, err
	}
	draftYear, err := optionalInt(item, "draft_year")
	if err != nil {
		return player.Profile{}, err
	}
	if draftYear != 0 {
		round, err := optionalInt(item, "draft_round")
		if err != nil {
			return player.Profile{}, err
		}
		pick, err := optionalInt(item, "draft_pick")
		if err != nil {
			return player.Profile{}, err
		}
		profile.Draft = &player.DraftRecord{
			Year: draftYear, Round: round, Pick: pick, NFLTeam: optionalString(item, "draft_nfl_team"),
		}
	}
	return profile, profile.Validate()
}

func requiredString(item map[string]types.AttributeValue, name string) (string, error) {
	value := optionalString(item, name)
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func optionalString(item map[string]types.AttributeValue, name string) string {
	value, ok := item[name].(*types.AttributeValueMemberS)
	if !ok {
		return ""
	}
	return value.Value
}

func optionalInt(item map[string]types.AttributeValue, name string) (int, error) {
	value, ok := item[name].(*types.AttributeValueMemberN)
	if !ok || value.Value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value.Value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return parsed, nil
}

var _ identity.Repository = (*Repository)(nil)
