package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveEnvPrecedence(t *testing.T) {
	setHome(t)
	t.Setenv("KEMORY_ENV", "india")
	if env, _ := ResolveEnv(""); env != "india" {
		t.Fatalf("KEMORY_ENV ignored: %s", env)
	}
	if env, _ := ResolveEnv("LOCAL"); env != "local" {
		t.Fatalf("flag should win and be case-insensitive: %s", env)
	}
	t.Setenv("KEMORY_ENV", "")
	if env, _ := ResolveEnv(""); env != "prod" {
		t.Fatalf("default should be prod: %s", env)
	}
}

func TestUnknownEnvNamesTheChoices(t *testing.T) {
	setHome(t)
	_, err := ResolveEnv("staging")
	if err == nil || !strings.Contains(err.Error(), "india, local, prod") {
		t.Fatalf("want a list of envs, got %v", err)
	}
}

func TestExtraEnvironmentsComeFromTheEnvironmentsFile(t *testing.T) {
	home := setHome(t)
	path := filepath.Join(home, ".kemory", "environments.json")
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte(`{"staging": {"issuer": "https://i", "kemory_url": "https://k", "server_name": "kemory-staging"},
		"prod": {"issuer": "https://evil", "kemory_url": "https://evil"}}`), 0o600)
	if env, err := ResolveEnv("staging"); err != nil || env != "staging" {
		t.Fatalf("staging from file: %v %v", env, err)
	}
	if p := EnvProfile("staging"); p.KemoryURL != "https://k" || p.ClientID != ClientID {
		t.Fatalf("profile: %+v", p)
	}
	if p := EnvProfile("prod"); p.KemoryURL != "https://api.kemory.s9n.ai" {
		t.Fatalf("the file must not override a built-in env: %s", p.KemoryURL)
	}
}

func TestProfileOverrides(t *testing.T) {
	setHome(t)
	t.Setenv("KEMORY_URL", "https://self.hosted")
	t.Setenv("KEMORY_OIDC_ISSUER", "https://idp")
	t.Setenv("KEMORY_CLI_CLIENT_ID", "custom")
	p := EnvProfile("prod")
	if p.KemoryURL != "https://self.hosted" || p.Issuer != "https://idp" || p.ClientID != "custom" {
		t.Fatalf("overrides not applied: %+v", p)
	}
}
