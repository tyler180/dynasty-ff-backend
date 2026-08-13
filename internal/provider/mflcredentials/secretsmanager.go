// Package mflcredentials loads MFL read-only credentials without exposing
// secret values in Lambda events or Terraform state.
package mflcredentials

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

type Client interface {
	GetSecretValue(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

type Provider struct {
	client    Client
	secretARN string
}

type secretValue struct {
	APIKey     string `json:"api_key"`
	UserCookie string `json:"user_cookie"`
}

func New(client Client, secretARN string) (*Provider, error) {
	if client == nil {
		return nil, fmt.Errorf("Secrets Manager client is required")
	}
	if strings.TrimSpace(secretARN) == "" {
		return nil, fmt.Errorf("MFL secret ARN is required")
	}
	return &Provider{client: client, secretARN: secretARN}, nil
}

func NewFromConfig(config aws.Config, secretARN string) (*Provider, error) {
	return New(secretsmanager.NewFromConfig(config), secretARN)
}

func (p *Provider) Environment(ctx context.Context) (map[string]string, error) {
	output, err := p.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(p.secretARN)})
	if err != nil {
		return nil, fmt.Errorf("get MFL credentials from Secrets Manager: %w", err)
	}
	if output.SecretString == nil {
		return nil, fmt.Errorf("MFL credential secret must contain a JSON secret string")
	}
	var value secretValue
	if err := json.Unmarshal([]byte(aws.ToString(output.SecretString)), &value); err != nil {
		return nil, fmt.Errorf("decode MFL credential secret: %w", err)
	}
	value.APIKey = strings.TrimSpace(value.APIKey)
	value.UserCookie = strings.TrimSpace(value.UserCookie)
	if value.APIKey == "" && value.UserCookie == "" {
		return nil, fmt.Errorf("MFL credential secret requires api_key or user_cookie")
	}
	if value.APIKey != "" && value.UserCookie != "" {
		return nil, fmt.Errorf("MFL credential secret must set only one of api_key or user_cookie")
	}
	environment := map[string]string{}
	if value.APIKey != "" {
		environment["MFL_API_KEY"] = value.APIKey
	} else {
		environment["MFL_USER_COOKIE"] = value.UserCookie
	}
	return environment, nil
}
