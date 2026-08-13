package mflcredentials

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

type fakeClient struct{ secret string }

func (f fakeClient) GetSecretValue(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(f.secret)}, nil
}

func TestProviderReturnsAPIKeyEnvironment(t *testing.T) {
	provider, err := New(fakeClient{secret: `{"api_key":"owner-key"}`}, "secret-arn")
	if err != nil {
		t.Fatal(err)
	}
	environment, err := provider.Environment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if environment["MFL_API_KEY"] != "owner-key" {
		t.Fatalf("API key was not returned")
	}
	if _, present := environment["MFL_USER_COOKIE"]; present {
		t.Fatal("unexpected user cookie")
	}
}

func TestProviderRejectsAmbiguousSecret(t *testing.T) {
	provider, err := New(fakeClient{secret: `{"api_key":"key","user_cookie":"cookie"}`}, "secret-arn")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Environment(context.Background()); err == nil {
		t.Fatal("expected ambiguous secret error")
	}
}
