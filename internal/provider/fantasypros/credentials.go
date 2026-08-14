package fantasypros

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

type SecretClient interface {
	GetSecretValue(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

type SecretProvider struct {
	client    SecretClient
	secretARN string
}

func NewSecretProvider(client SecretClient, secretARN string) (*SecretProvider, error) {
	if client == nil || strings.TrimSpace(secretARN) == "" {
		return nil, fmt.Errorf("Secrets Manager client and secret ARN are required for FantasyPros")
	}
	return &SecretProvider{client: client, secretARN: secretARN}, nil
}

func NewSecretProviderFromConfig(config aws.Config, secretARN string) (*SecretProvider, error) {
	return NewSecretProvider(secretsmanager.NewFromConfig(config), secretARN)
}

func (p *SecretProvider) APIKey(ctx context.Context) (string, error) {
	output, err := p.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(p.secretARN)})
	if err != nil {
		return "", fmt.Errorf("get FantasyPros credentials from Secrets Manager: %w", err)
	}
	if output.SecretString == nil {
		return "", fmt.Errorf("FantasyPros credential secret must contain a JSON secret string")
	}
	var value struct {
		APIKey string `json:"fantasypros_api_key"`
	}
	if err := json.Unmarshal([]byte(aws.ToString(output.SecretString)), &value); err != nil {
		return "", fmt.Errorf("decode FantasyPros credential secret: %w", err)
	}
	if value.APIKey = strings.TrimSpace(value.APIKey); value.APIKey == "" {
		return "", fmt.Errorf("credential secret requires fantasypros_api_key")
	}
	return value.APIKey, nil
}
