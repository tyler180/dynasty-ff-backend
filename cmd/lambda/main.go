package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	awslambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/tyler180/dynasty-ff-backend/internal/app/freeagenttrends"
	"github.com/tyler180/dynasty-ff-backend/internal/app/identitysync"
	"github.com/tyler180/dynasty-ff-backend/internal/app/mflingest"
	"github.com/tyler180/dynasty-ff-backend/internal/app/playerstatsync"
	"github.com/tyler180/dynasty-ff-backend/internal/app/snapcountsync"
	"github.com/tyler180/dynasty-ff-backend/internal/app/snapshotanalysis"
	"github.com/tyler180/dynasty-ff-backend/internal/lambdaapp"
	"github.com/tyler180/dynasty-ff-backend/internal/provider/dynastyprocess"
	"github.com/tyler180/dynasty-ff-backend/internal/provider/fantasypros"
	"github.com/tyler180/dynasty-ff-backend/internal/provider/mflcredentials"
	"github.com/tyler180/dynasty-ff-backend/internal/provider/mflfreeagents"
	"github.com/tyler180/dynasty-ff-backend/internal/provider/mflidentity"
	"github.com/tyler180/dynasty-ff-backend/internal/provider/nflverse"
	"github.com/tyler180/dynasty-ff-backend/internal/storage/dynamodbidentity"
	"github.com/tyler180/dynasty-ff-backend/internal/storage/dynamodbplayerstats"
	"github.com/tyler180/dynasty-ff-backend/internal/storage/s3leaguestore"
	"github.com/tyler180/dynasty-ff-backend/internal/storage/s3snapstore"
	"github.com/tyler180/dynasty-ff-backend/internal/storage/snapstore"
)

func main() {
	handler, err := buildHandler(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	entrypoint, err := lambdaapp.NewEntrypoint(handler)
	if err != nil {
		log.Fatal(err)
	}
	lambda.Start(entrypoint.Handle)
}

func buildHandler(ctx context.Context) (*lambdaapp.Handler, error) {
	tableName, err := requiredEnvironment("PLAYER_IDENTITY_TABLE")
	if err != nil {
		return nil, err
	}
	bucketName, err := requiredEnvironment("LEAGUE_DATA_BUCKET")
	if err != nil {
		return nil, err
	}
	playerGameTableName, err := requiredEnvironment("PLAYER_GAME_STATS_TABLE")
	if err != nil {
		return nil, err
	}
	awsConfig, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration: %w", err)
	}
	identities, err := dynamodbidentity.NewFromConfig(awsConfig, tableName)
	if err != nil {
		return nil, err
	}
	snapshots, err := s3leaguestore.NewFromConfig(awsConfig, bucketName)
	if err != nil {
		return nil, err
	}
	snapArchive, err := s3snapstore.NewFromConfig(awsConfig, bucketName)
	if err != nil {
		return nil, err
	}
	playerGameStats, err := dynamodbplayerstats.NewFromConfig(awsConfig, playerGameTableName)
	if err != nil {
		return nil, err
	}
	snapCounts := snapstore.Reader{Primary: playerGameStats, Fallback: snapArchive, State: playerGameStats}
	snapWriter := snapstore.Writer{Archive: snapArchive, Primary: playerGameStats}
	secretARN, err := requiredEnvironment("MFL_SECRET_ARN")
	if err != nil {
		return nil, err
	}
	mcpCommand, err := requiredEnvironment("MFL_MCP_COMMAND")
	if err != nil {
		return nil, err
	}
	credentials, err := mflcredentials.NewFromConfig(awsConfig, secretARN)
	if err != nil {
		return nil, err
	}
	fantasyProsKeys, err := fantasypros.NewSecretProviderFromConfig(awsConfig, secretARN)
	if err != nil {
		return nil, err
	}
	playerEvaluations, err := fantasypros.NewDefault(fantasyProsKeys)
	if err != nil {
		return nil, err
	}
	handler, err := lambdaapp.New(snapshots, identities)
	if err != nil {
		return nil, err
	}
	identitySourceURL := strings.TrimSpace(os.Getenv("IDENTITY_SOURCE_URL"))
	if identitySourceURL == "" {
		identitySourceURL = dynastyprocess.DefaultURL
	}
	identitySource, err := dynastyprocess.NewDefault(identitySourceURL)
	if err != nil {
		return nil, err
	}
	handler.WithIdentitySyncer(identitysync.Service{
		Source: identitySource, Repository: identities, BulkResolver: identities,
		RelevantPlayers: mflidentity.Source{MCPCommand: mcpCommand, Credentials: credentials},
	})
	handler.WithAnalyzer(snapshotanalysis.Service{Snapshots: snapshots, Players: identities})
	snapCountsURL := strings.TrimSpace(os.Getenv("SNAP_COUNTS_URL_TEMPLATE"))
	if snapCountsURL == "" {
		snapCountsURL = nflverse.DefaultSnapCountsURLTemplate
	}
	snapSource, err := nflverse.NewDefault(snapCountsURL)
	if err != nil {
		return nil, err
	}
	handler.WithSnapCounts(snapcountsync.Service{
		Source: snapSource, Identities: identities, Snaps: snapWriter, State: playerGameStats, Archive: snapArchive,
	}, snapCounts)
	playerStatsSource, err := nflverse.NewDefaultPlayerStats(splitEnvironment("PLAYER_STATS_URL_TEMPLATES"))
	if err != nil {
		return nil, err
	}
	handler.WithPlayerStats(playerstatsync.Service{
		Source: playerStatsSource, Identities: identities, Stats: playerGameStats,
		State: playerGameStats, Archive: snapArchive,
	}, playerGameStats)
	handler.WithFreeAgentTrends(freeagenttrends.Service{
		FreeAgents: mflfreeagents.Source{MCPCommand: mcpCommand, Credentials: credentials},
		Identities: identities, Snaps: snapCounts,
	})
	handler.WithSyncStarter(lambdaSyncStarter{
		client: awslambda.NewFromConfig(awsConfig), functionName: os.Getenv("AWS_LAMBDA_FUNCTION_NAME"),
	})
	return handler.WithSyncer(mflingest.Service{
		MCPCommand: mcpCommand, Credentials: credentials,
		Identities: identities, Snapshots: snapshots, Enrichments: snapshots, Evaluations: playerEvaluations,
	}), nil
}

func splitEnvironment(name string) []string {
	var values []string
	for _, value := range strings.Split(os.Getenv(name), ",") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

type lambdaInvoker interface {
	Invoke(context.Context, *awslambda.InvokeInput, ...func(*awslambda.Options)) (*awslambda.InvokeOutput, error)
}

type lambdaSyncStarter struct {
	client       lambdaInvoker
	functionName string
}

func (s lambdaSyncStarter) Start(ctx context.Context, request lambdaapp.Request) error {
	if s.client == nil || strings.TrimSpace(s.functionName) == "" {
		return fmt.Errorf("Lambda client and function name are required")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode asynchronous sync request: %w", err)
	}
	output, err := s.client.Invoke(ctx, &awslambda.InvokeInput{
		FunctionName: aws.String(s.functionName), InvocationType: types.InvocationTypeEvent, Payload: payload,
	})
	if err != nil {
		return fmt.Errorf("start asynchronous sync: %w", err)
	}
	if output.StatusCode != 202 {
		return fmt.Errorf("start asynchronous sync: unexpected Lambda status %d", output.StatusCode)
	}
	return nil
}

func requiredEnvironment(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}
