package ai

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindEnvKeysAndGetEnvAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")
	if got := FindEnvKeys(ProviderOpenAI); len(got) != 1 || got[0] != "OPENAI_API_KEY" {
		t.Fatalf("expected OPENAI_API_KEY, got %#v", got)
	}
	if got := GetEnvAPIKey(ProviderOpenAI); got != "openai-key" {
		t.Fatalf("expected openai key, got %q", got)
	}
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-key")
	t.Setenv("ANTHROPIC_OAUTH_TOKEN", "anthropic-oauth")
	if got := FindEnvKeys(ProviderAnthropic); len(got) != 2 || got[0] != "ANTHROPIC_OAUTH_TOKEN" || got[1] != "ANTHROPIC_API_KEY" {
		t.Fatalf("expected anthropic env precedence, got %#v", got)
	}
	if got := GetEnvAPIKey(ProviderAnthropic); got != "anthropic-oauth" {
		t.Fatalf("expected anthropic oauth token, got %q", got)
	}
}

func TestGetEnvAPIKeyAmazonBedrockAuthenticated(t *testing.T) {
	t.Setenv("AWS_PROFILE", "default")
	if got := GetEnvAPIKey(ProviderAmazonBedrock); got != "<authenticated>" {
		t.Fatalf("expected authenticated sentinel, got %q", got)
	}
}

func TestGetEnvAPIKeyGoogleVertexAuthenticatedWithExplicitCredentials(t *testing.T) {
	credentialsPath := filepath.Join(t.TempDir(), "application_default_credentials.json")
	if err := os.WriteFile(credentialsPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", credentialsPath)
	t.Setenv("GOOGLE_CLOUD_PROJECT", "project")
	t.Setenv("GOOGLE_CLOUD_LOCATION", "us-central1")

	if got := GetEnvAPIKey(ProviderGoogleVertex); got != "<authenticated>" {
		t.Fatalf("expected authenticated sentinel, got %q", got)
	}
}

func TestGetEnvAPIKeyGoogleVertexAuthenticatedWithDefaultCredentials(t *testing.T) {
	home := t.TempDir()
	credentialsPath := filepath.Join(home, ".config", "gcloud", "application_default_credentials.json")
	if err := os.MkdirAll(filepath.Dir(credentialsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentialsPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GCLOUD_PROJECT", "project")
	t.Setenv("GOOGLE_CLOUD_LOCATION", "us-central1")

	if got := GetEnvAPIKey(ProviderGoogleVertex); got != "<authenticated>" {
		t.Fatalf("expected authenticated sentinel, got %q", got)
	}
}

func TestGetEnvAPIKeyGoogleVertexRequiresProjectAndLocation(t *testing.T) {
	credentialsPath := filepath.Join(t.TempDir(), "application_default_credentials.json")
	if err := os.WriteFile(credentialsPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", credentialsPath)
	t.Setenv("GOOGLE_CLOUD_PROJECT", "project")

	if got := GetEnvAPIKey(ProviderGoogleVertex); got != "" {
		t.Fatalf("expected no key without location, got %q", got)
	}
}
