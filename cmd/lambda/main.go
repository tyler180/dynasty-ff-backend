package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/tyler180/dynasty-ff-backend/internal/app/identitysync"
	"github.com/tyler180/dynasty-ff-backend/internal/app/mflingest"
	"github.com/tyler180/dynasty-ff-backend/internal/app/snapshotanalysis"
	"github.com/tyler180/dynasty-ff-backend/internal/lambdaapp"
	"github.com/tyler180/dynasty-ff-backend/internal/provider/dynastyprocess"
	"github.com/tyler180/dynasty-ff-backend/internal/provider/fantasypros"
	"github.com/tyler180/dynasty-ff-backend/internal/provider/mflcredentials"
	"github.com/tyler180/dynasty-ff-backend/internal/provider/mflidentity"
	"github.com/tyler180/dynasty-ff-backend/internal/storage/dynamodbidentity"
	"github.com/tyler180/dynasty-ff-backend/internal/storage/s3leaguestore"
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
	return handler.WithSyncer(mflingest.Service{
		MCPCommand: mcpCommand, Credentials: credentials,
		Identities: identities, Snapshots: snapshots, Evaluations: playerEvaluations,
	}), nil
}

func requiredEnvironment(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}
