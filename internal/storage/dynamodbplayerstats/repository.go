// Package dynamodbplayerstats persists normalized player-game statistics in a
// queryable DynamoDB table. S3 remains the versioned source archive.
package dynamodbplayerstats

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/tyler180/dynasty-ff-backend/internal/features/history"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
)

const seasonIndex = "season-index"

type Client interface {
	BatchWriteItem(context.Context, *dynamodb.BatchWriteItemInput, ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error)
	GetItem(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	PutItem(context.Context, *dynamodb.PutItemInput, ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	Query(context.Context, *dynamodb.QueryInput, ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
}

type Repository struct {
	client    Client
	tableName string
}

func New(client Client, tableName string) (*Repository, error) {
	if client == nil {
		return nil, fmt.Errorf("DynamoDB client is required")
	}
	if strings.TrimSpace(tableName) == "" {
		return nil, fmt.Errorf("DynamoDB player-game table name is required")
	}
	return &Repository{client: client, tableName: tableName}, nil
}

func NewFromConfig(config aws.Config, tableName string) (*Repository, error) {
	return New(dynamodb.NewFromConfig(config), tableName)
}

func (r *Repository) PutPlayerGameSnaps(ctx context.Context, records []history.PlayerGameSnaps) error {
	if len(records) == 0 {
		return fmt.Errorf("at least one player-game record is required")
	}
	season := records[0].Season
	writes := make([]types.WriteRequest, 0, len(records))
	incoming := make(map[string]struct{}, len(records))
	for _, record := range records {
		if err := record.Validate(); err != nil {
			return err
		}
		if record.Season != season {
			return fmt.Errorf("player-game records must belong to one season")
		}
		item := encode(record)
		writes = append(writes, types.WriteRequest{PutRequest: &types.PutRequest{Item: item}})
		incoming[itemKey(item)] = struct{}{}
	}

	existing, err := r.indexedKeys(ctx, seasonKey(season))
	if err != nil {
		return err
	}
	for _, existingKey := range existing {
		if _, ok := incoming[itemKey(existingKey)]; ok {
			continue
		}
		writes = append(writes, types.WriteRequest{DeleteRequest: &types.DeleteRequest{Key: existingKey}})
	}
	if err := r.batchWrite(ctx, writes); err != nil {
		return fmt.Errorf("replace %d player-game stats in DynamoDB: %w", season, err)
	}
	return nil
}

func (r *Repository) PutPlayerGameStats(ctx context.Context, records []history.PlayerGameStats) error {
	if len(records) == 0 {
		return fmt.Errorf("at least one weekly player-stat record is required")
	}
	season := records[0].Season
	incoming := make(map[string]struct{}, len(records))
	for _, record := range records {
		if err := record.Validate(); err != nil {
			return err
		}
		if record.Season != season {
			return fmt.Errorf("weekly player-stat records must belong to one season")
		}
		incoming[playerKey(record.PlayerID)+"\x00"+playerStatsGameKey(record)] = struct{}{}
	}
	existing, err := r.indexedKeys(ctx, playerStatsSeasonKey(season))
	if err != nil {
		return err
	}
	for start := 0; start < len(records); start += 25 {
		end := min(start+25, len(records))
		writes := make([]types.WriteRequest, 0, end-start)
		for _, record := range records[start:end] {
			writes = append(writes, types.WriteRequest{PutRequest: &types.PutRequest{Item: encodePlayerStats(record)}})
		}
		if err := r.batchWrite(ctx, writes); err != nil {
			return fmt.Errorf("write %d weekly player stats in DynamoDB: %w", season, err)
		}
	}
	deletes := make([]types.WriteRequest, 0)
	for _, existingKey := range existing {
		if _, ok := incoming[itemKey(existingKey)]; !ok {
			deletes = append(deletes, types.WriteRequest{DeleteRequest: &types.DeleteRequest{Key: existingKey}})
		}
	}
	if err := r.batchWrite(ctx, deletes); err != nil {
		return fmt.Errorf("remove stale %d weekly player stats from DynamoDB: %w", season, err)
	}
	return nil
}

func (r *Repository) PlayerGameSnaps(ctx context.Context, query history.SnapQuery) ([]history.PlayerGameSnaps, error) {
	if err := query.Validate(); err != nil {
		return nil, err
	}
	seasons := make(map[int]struct{}, len(query.Seasons))
	for _, season := range query.Seasons {
		seasons[season] = struct{}{}
	}
	groups := make(map[string]struct{}, len(query.PositionGroups))
	for _, group := range query.PositionGroups {
		groups[strings.ToUpper(strings.TrimSpace(group))] = struct{}{}
	}

	type queryResult struct {
		records []history.PlayerGameSnaps
		err     error
	}
	jobs := make(chan player.ID, len(query.PlayerIDs))
	results := make(chan queryResult, len(query.PlayerIDs))
	for _, playerID := range query.PlayerIDs {
		jobs <- playerID
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(10, len(query.PlayerIDs)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for playerID := range jobs {
				records, err := r.queryPlayer(ctx, playerID, seasons, groups)
				results <- queryResult{records: records, err: err}
			}
		}()
	}
	workers.Wait()
	close(results)
	var result []history.PlayerGameSnaps
	for queried := range results {
		if queried.err != nil {
			return nil, queried.err
		}
		result = append(result, queried.records...)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Season != result[j].Season {
			return result[i].Season < result[j].Season
		}
		if result[i].Week != result[j].Week {
			return result[i].Week < result[j].Week
		}
		if result[i].GameID != result[j].GameID {
			return result[i].GameID < result[j].GameID
		}
		return result[i].PlayerID < result[j].PlayerID
	})
	return result, nil
}

func (r *Repository) PlayerGameStats(ctx context.Context, query history.PlayerStatsQuery) ([]history.PlayerGameStats, error) {
	if err := query.Validate(); err != nil {
		return nil, err
	}
	seasons := make(map[int]struct{}, len(query.Seasons))
	for _, season := range query.Seasons {
		seasons[season] = struct{}{}
	}
	type queryResult struct {
		records []history.PlayerGameStats
		err     error
	}
	jobs := make(chan player.ID, len(query.PlayerIDs))
	results := make(chan queryResult, len(query.PlayerIDs))
	for _, playerID := range query.PlayerIDs {
		jobs <- playerID
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(10, len(query.PlayerIDs)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for playerID := range jobs {
				records, err := r.queryPlayerStats(ctx, playerID, seasons)
				results <- queryResult{records: records, err: err}
			}
		}()
	}
	workers.Wait()
	close(results)
	var result []history.PlayerGameStats
	for queried := range results {
		if queried.err != nil {
			return nil, queried.err
		}
		result = append(result, queried.records...)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Season != result[j].Season {
			return result[i].Season < result[j].Season
		}
		if result[i].Week != result[j].Week {
			return result[i].Week < result[j].Week
		}
		if result[i].GameID != result[j].GameID {
			return result[i].GameID < result[j].GameID
		}
		return result[i].PlayerID < result[j].PlayerID
	})
	return result, nil
}

func (r *Repository) queryPlayerStats(ctx context.Context, playerID player.ID, seasons map[int]struct{}) ([]history.PlayerGameStats, error) {
	var result []history.PlayerGameStats
	var startKey map[string]types.AttributeValue
	for {
		output, err := r.client.Query(ctx, &dynamodb.QueryInput{
			TableName:              aws.String(r.tableName),
			KeyConditionExpression: aws.String("pk = :pk AND begins_with(sk, :prefix)"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk": stringValue(playerKey(playerID)), ":prefix": stringValue("GAME#"),
			},
			ExclusiveStartKey: startKey,
		})
		if err != nil {
			return nil, fmt.Errorf("query player %s weekly stats from DynamoDB: %w", playerID, err)
		}
		for _, item := range output.Items {
			if optionalString(item, "entity_type") != "player_game_weekly_stats" {
				continue
			}
			record, err := decodePlayerStats(item)
			if err != nil {
				return nil, fmt.Errorf("decode player %s weekly stats: %w", playerID, err)
			}
			if _, ok := seasons[record.Season]; ok {
				result = append(result, record)
			}
		}
		if len(output.LastEvaluatedKey) == 0 {
			break
		}
		startKey = output.LastEvaluatedKey
	}
	return result, nil
}

func (r *Repository) queryPlayer(ctx context.Context, playerID player.ID, seasons map[int]struct{}, groups map[string]struct{}) ([]history.PlayerGameSnaps, error) {
	var result []history.PlayerGameSnaps
	var startKey map[string]types.AttributeValue
	for {
		output, err := r.client.Query(ctx, &dynamodb.QueryInput{
			TableName:              aws.String(r.tableName),
			KeyConditionExpression: aws.String("pk = :pk AND begins_with(sk, :prefix)"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":pk": stringValue(playerKey(playerID)), ":prefix": stringValue("GAME#"),
			},
			ExclusiveStartKey: startKey,
		})
		if err != nil {
			return nil, fmt.Errorf("query player %s stats from DynamoDB: %w", playerID, err)
		}
		for _, item := range output.Items {
			if optionalString(item, "entity_type") != "player_game_stats" {
				continue
			}
			record, err := decode(item)
			if err != nil {
				return nil, fmt.Errorf("decode player %s stats: %w", playerID, err)
			}
			if _, ok := seasons[record.Season]; !ok {
				continue
			}
			if len(groups) > 0 {
				if _, ok := groups[strings.ToUpper(record.PositionGroup)]; !ok {
					continue
				}
			}
			result = append(result, record)
		}
		if len(output.LastEvaluatedKey) == 0 {
			break
		}
		startKey = output.LastEvaluatedKey
	}
	return result, nil
}

func (r *Repository) SnapDatasetState(ctx context.Context, season int) (history.SnapDatasetState, error) {
	output, err := r.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(r.tableName), ConsistentRead: aws.Bool(true), Key: stateKey(season),
	})
	if err != nil {
		return history.SnapDatasetState{}, fmt.Errorf("get %d snap dataset state: %w", season, err)
	}
	if len(output.Item) == 0 {
		return history.SnapDatasetState{}, nil
	}
	state := history.SnapDatasetState{
		Season: season, SourceVersion: optionalString(output.Item, "source_version"),
		Version: optionalString(output.Item, "dataset_version"),
	}
	state.RecordCount, err = optionalInt(output.Item, "record_count")
	if err != nil {
		return history.SnapDatasetState{}, err
	}
	if raw := optionalString(output.Item, "imported_at"); raw != "" {
		state.ImportedAt, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return history.SnapDatasetState{}, fmt.Errorf("imported_at: %w", err)
		}
	}
	return state, nil
}

func (r *Repository) PutSnapDatasetState(ctx context.Context, state history.SnapDatasetState) error {
	if state.Season < 2012 || state.SourceVersion == "" || state.Version == "" || state.RecordCount < 1 || state.ImportedAt.IsZero() {
		return fmt.Errorf("complete snap dataset state is required")
	}
	item := stateKey(state.Season)
	item["entity_type"] = stringValue("dataset_state")
	item["dataset"] = stringValue("snap_counts")
	item["season"] = numberValue(state.Season)
	item["dataset_version"] = stringValue(state.Version)
	item["source_version"] = stringValue(state.SourceVersion)
	item["record_count"] = numberValue(state.RecordCount)
	item["imported_at"] = stringValue(state.ImportedAt.UTC().Format(time.RFC3339Nano))
	_, err := r.client.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(r.tableName), Item: item})
	if err != nil {
		return fmt.Errorf("put %d snap dataset state: %w", state.Season, err)
	}
	return nil
}

func (r *Repository) PlayerStatsDatasetState(ctx context.Context, season int) (history.PlayerStatsDatasetState, error) {
	output, err := r.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(r.tableName), ConsistentRead: aws.Bool(true), Key: playerStatsStateKey(season),
	})
	if err != nil {
		return history.PlayerStatsDatasetState{}, fmt.Errorf("get %d player-stat dataset state: %w", season, err)
	}
	if len(output.Item) == 0 {
		return history.PlayerStatsDatasetState{}, nil
	}
	state := history.PlayerStatsDatasetState{
		Season: season, SourceVersion: optionalString(output.Item, "source_version"),
		Version: optionalString(output.Item, "dataset_version"),
	}
	state.RecordCount, err = optionalInt(output.Item, "record_count")
	if err != nil {
		return history.PlayerStatsDatasetState{}, err
	}
	if raw := optionalString(output.Item, "imported_at"); raw != "" {
		state.ImportedAt, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return history.PlayerStatsDatasetState{}, fmt.Errorf("imported_at: %w", err)
		}
	}
	return state, nil
}

func (r *Repository) PutPlayerStatsDatasetState(ctx context.Context, state history.PlayerStatsDatasetState) error {
	if state.Season < 1999 || state.SourceVersion == "" || state.Version == "" || state.RecordCount < 1 || state.ImportedAt.IsZero() {
		return fmt.Errorf("complete player-stat dataset state is required")
	}
	item := playerStatsStateKey(state.Season)
	item["entity_type"] = stringValue("dataset_state")
	item["dataset"] = stringValue("player_stats")
	item["season"] = numberValue(state.Season)
	item["dataset_version"] = stringValue(state.Version)
	item["source_version"] = stringValue(state.SourceVersion)
	item["record_count"] = numberValue(state.RecordCount)
	item["imported_at"] = stringValue(state.ImportedAt.UTC().Format(time.RFC3339Nano))
	_, err := r.client.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(r.tableName), Item: item})
	if err != nil {
		return fmt.Errorf("put %d player-stat dataset state: %w", state.Season, err)
	}
	return nil
}

func (r *Repository) indexedKeys(ctx context.Context, indexPartition string) ([]map[string]types.AttributeValue, error) {
	var result []map[string]types.AttributeValue
	var startKey map[string]types.AttributeValue
	for {
		output, err := r.client.Query(ctx, &dynamodb.QueryInput{
			TableName: aws.String(r.tableName), IndexName: aws.String(seasonIndex),
			KeyConditionExpression:    aws.String("gsi1pk = :season"),
			ExpressionAttributeValues: map[string]types.AttributeValue{":season": stringValue(indexPartition)},
			ProjectionExpression:      aws.String("pk, sk"), ExclusiveStartKey: startKey,
		})
		if err != nil {
			return nil, fmt.Errorf("list existing %s player-game stats: %w", indexPartition, err)
		}
		for _, item := range output.Items {
			result = append(result, map[string]types.AttributeValue{"pk": item["pk"], "sk": item["sk"]})
		}
		if len(output.LastEvaluatedKey) == 0 {
			break
		}
		startKey = output.LastEvaluatedKey
	}
	return result, nil
}

func (r *Repository) batchWrite(ctx context.Context, writes []types.WriteRequest) error {
	for start := 0; start < len(writes); start += 25 {
		end := min(start+25, len(writes))
		pending := map[string][]types.WriteRequest{r.tableName: writes[start:end]}
		for attempt := 0; len(pending) > 0; attempt++ {
			if attempt == 8 {
				return fmt.Errorf("DynamoDB left batch writes unprocessed after retries")
			}
			output, err := r.client.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{RequestItems: pending})
			if err != nil {
				return err
			}
			pending = output.UnprocessedItems
			if len(pending) > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Duration(1<<attempt) * 10 * time.Millisecond):
				}
			}
		}
	}
	return nil
}

func encode(record history.PlayerGameSnaps) map[string]types.AttributeValue {
	item := map[string]types.AttributeValue{
		"pk": stringValue(playerKey(record.PlayerID)), "sk": stringValue(gameKey(record)),
		"gsi1pk": stringValue(seasonKey(record.Season)), "gsi1sk": stringValue(seasonSortKey(record)),
		"entity_type": stringValue("player_game_stats"), "player_id": stringValue(string(record.PlayerID)),
		"source_player_id": stringValue(record.SourcePlayerID), "player_name": stringValue(record.PlayerName),
		"game_id": stringValue(record.GameID), "source_game_id": stringValue(record.SourceGameID),
		"season": numberValue(record.Season), "week": numberValue(record.Week),
		"game_type": stringValue(record.GameType), "team": stringValue(record.Team), "opponent": stringValue(record.Opponent),
		"position": stringValue(record.Position), "position_group": stringValue(record.PositionGroup),
		"offense_snaps": numberValue(record.OffenseSnaps), "offense_snap_pct": decimalValue(record.OffenseSnapPct),
		"defense_snaps": numberValue(record.DefenseSnaps), "team_defense_snaps": numberValue(record.TeamDefenseSnaps),
		"defense_snap_pct": decimalValue(record.DefenseSnapPct), "special_teams_snaps": numberValue(record.SpecialTeamSnaps),
		"special_teams_snap_pct": decimalValue(record.SpecialTeamPct), "source": stringValue(record.Source),
		"ingestion_run_id": stringValue(record.IngestionRunID),
	}
	if !record.GameDate.IsZero() {
		item["game_date"] = stringValue(record.GameDate.UTC().Format(time.RFC3339Nano))
	}
	return item
}

func decode(item map[string]types.AttributeValue) (history.PlayerGameSnaps, error) {
	record := history.PlayerGameSnaps{
		PlayerID: player.ID(optionalString(item, "player_id")), SourcePlayerID: optionalString(item, "source_player_id"),
		PlayerName: optionalString(item, "player_name"), GameID: optionalString(item, "game_id"), SourceGameID: optionalString(item, "source_game_id"),
		GameType: optionalString(item, "game_type"), Team: optionalString(item, "team"), Opponent: optionalString(item, "opponent"),
		Position: optionalString(item, "position"), PositionGroup: optionalString(item, "position_group"),
		Source: optionalString(item, "source"), IngestionRunID: optionalString(item, "ingestion_run_id"),
	}
	var err error
	for name, target := range map[string]*int{
		"season": &record.Season, "week": &record.Week, "offense_snaps": &record.OffenseSnaps,
		"defense_snaps": &record.DefenseSnaps, "team_defense_snaps": &record.TeamDefenseSnaps,
		"special_teams_snaps": &record.SpecialTeamSnaps,
	} {
		*target, err = optionalInt(item, name)
		if err != nil {
			return history.PlayerGameSnaps{}, err
		}
	}
	for name, target := range map[string]*float64{
		"offense_snap_pct": &record.OffenseSnapPct, "defense_snap_pct": &record.DefenseSnapPct,
		"special_teams_snap_pct": &record.SpecialTeamPct,
	} {
		*target, err = optionalFloat(item, name)
		if err != nil {
			return history.PlayerGameSnaps{}, err
		}
	}
	if raw := optionalString(item, "game_date"); raw != "" {
		record.GameDate, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return history.PlayerGameSnaps{}, fmt.Errorf("game_date: %w", err)
		}
	}
	return record, record.Validate()
}

func encodePlayerStats(record history.PlayerGameStats) map[string]types.AttributeValue {
	item := map[string]types.AttributeValue{
		"pk": stringValue(playerKey(record.PlayerID)), "sk": stringValue(playerStatsGameKey(record)),
		"gsi1pk": stringValue(playerStatsSeasonKey(record.Season)), "gsi1sk": stringValue(playerStatsSeasonSortKey(record)),
		"entity_type": stringValue("player_game_weekly_stats"), "player_id": stringValue(string(record.PlayerID)),
		"source_player_id": stringValue(record.SourcePlayerID), "player_name": stringValue(record.PlayerName),
		"display_name": stringValue(record.DisplayName), "position": stringValue(record.Position),
		"position_group": stringValue(record.PositionGroup), "season": numberValue(record.Season), "week": numberValue(record.Week),
		"game_type": stringValue(record.GameType), "game_id": stringValue(record.GameID), "team": stringValue(record.Team),
		"opponent": stringValue(record.Opponent), "source": stringValue(record.Source), "ingestion_run_id": stringValue(record.IngestionRunID),
	}
	if len(record.Metrics) > 0 {
		values := make(map[string]types.AttributeValue, len(record.Metrics))
		for name, value := range record.Metrics {
			values[name] = decimalValue(value)
		}
		item["metrics"] = &types.AttributeValueMemberM{Value: values}
	}
	if len(record.Attributes) > 0 {
		values := make(map[string]types.AttributeValue, len(record.Attributes))
		for name, value := range record.Attributes {
			values[name] = stringValue(value)
		}
		item["attributes"] = &types.AttributeValueMemberM{Value: values}
	}
	return item
}

func decodePlayerStats(item map[string]types.AttributeValue) (history.PlayerGameStats, error) {
	record := history.PlayerGameStats{
		PlayerID: player.ID(optionalString(item, "player_id")), SourcePlayerID: optionalString(item, "source_player_id"),
		PlayerName: optionalString(item, "player_name"), DisplayName: optionalString(item, "display_name"),
		Position: optionalString(item, "position"), PositionGroup: optionalString(item, "position_group"),
		GameType: optionalString(item, "game_type"), GameID: optionalString(item, "game_id"),
		Team: optionalString(item, "team"), Opponent: optionalString(item, "opponent"),
		Source: optionalString(item, "source"), IngestionRunID: optionalString(item, "ingestion_run_id"),
		Metrics: map[string]float64{}, Attributes: map[string]string{},
	}
	var err error
	record.Season, err = optionalInt(item, "season")
	if err != nil {
		return history.PlayerGameStats{}, err
	}
	record.Week, err = optionalInt(item, "week")
	if err != nil {
		return history.PlayerGameStats{}, err
	}
	if values, ok := item["metrics"].(*types.AttributeValueMemberM); ok {
		for name, value := range values.Value {
			number, ok := value.(*types.AttributeValueMemberN)
			if !ok {
				return history.PlayerGameStats{}, fmt.Errorf("metric %s must be numeric", name)
			}
			parsed, err := strconv.ParseFloat(number.Value, 64)
			if err != nil {
				return history.PlayerGameStats{}, fmt.Errorf("metric %s: %w", name, err)
			}
			record.Metrics[name] = parsed
		}
	}
	if values, ok := item["attributes"].(*types.AttributeValueMemberM); ok {
		for name, value := range values.Value {
			text, ok := value.(*types.AttributeValueMemberS)
			if !ok {
				return history.PlayerGameStats{}, fmt.Errorf("attribute %s must be text", name)
			}
			record.Attributes[name] = text.Value
		}
	}
	return record, record.Validate()
}

func playerKey(id player.ID) string { return "PLAYER#" + string(id) }
func seasonKey(season int) string   { return fmt.Sprintf("SEASON#%04d", season) }
func gameKey(record history.PlayerGameSnaps) string {
	return fmt.Sprintf("GAME#%04d#%02d#%s", record.Season, record.Week, record.GameID)
}
func seasonSortKey(record history.PlayerGameSnaps) string {
	return fmt.Sprintf("PLAYER#%s#%02d#%s", record.PlayerID, record.Week, record.GameID)
}
func playerStatsSeasonKey(season int) string {
	return fmt.Sprintf("PLAYER_STATS#SEASON#%04d", season)
}
func playerStatsGameKey(record history.PlayerGameStats) string {
	return fmt.Sprintf("GAME#%04d#%02d#%s#PLAYER_STATS", record.Season, record.Week, record.GameID)
}
func playerStatsSeasonSortKey(record history.PlayerGameStats) string {
	return fmt.Sprintf("PLAYER#%s#%02d#%s", record.PlayerID, record.Week, record.GameID)
}
func stateKey(season int) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{"pk": stringValue("DATASET#SNAP_COUNTS"), "sk": stringValue(seasonKey(season))}
}
func playerStatsStateKey(season int) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{"pk": stringValue("DATASET#PLAYER_STATS"), "sk": stringValue(seasonKey(season))}
}
func itemKey(item map[string]types.AttributeValue) string {
	return optionalString(item, "pk") + "\x00" + optionalString(item, "sk")
}
func stringValue(value string) types.AttributeValue {
	return &types.AttributeValueMemberS{Value: value}
}
func numberValue(value int) types.AttributeValue {
	return &types.AttributeValueMemberN{Value: strconv.Itoa(value)}
}
func decimalValue(value float64) types.AttributeValue {
	return &types.AttributeValueMemberN{Value: strconv.FormatFloat(value, 'f', -1, 64)}
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
func optionalFloat(item map[string]types.AttributeValue, name string) (float64, error) {
	value, ok := item[name].(*types.AttributeValueMemberN)
	if !ok || value.Value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseFloat(value.Value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return parsed, nil
}

var _ history.SnapReader = (*Repository)(nil)
var _ history.SnapWriter = (*Repository)(nil)
var _ history.SnapDatasetStateStore = (*Repository)(nil)
var _ history.PlayerStatsReader = (*Repository)(nil)
var _ history.PlayerStatsWriter = (*Repository)(nil)
var _ history.PlayerStatsDatasetStateStore = (*Repository)(nil)
