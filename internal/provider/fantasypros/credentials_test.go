package fantasypros

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

type fakeSecretClient struct{ value string }

func (f fakeSecretClient) GetSecretValue(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(f.value)}, nil
}

func TestSecretProviderReadsDedicatedKey(t *testing.T) {
	provider, err := NewSecretProvider(fakeSecretClient{value: `{"api_key":"mfl","fantasypros_api_key":"fp-key"}`}, "arn")
	if err != nil {
		t.Fatal(err)
	}
	key, err := provider.APIKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if key != "fp-key" {
		t.Fatalf("key = %q", key)
	}
}
