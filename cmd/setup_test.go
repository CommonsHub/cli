package cmd

import (
	"os"
	"testing"
)

func TestResolveEnvValuePrefersEnvironment(t *testing.T) {
	const key = "CHB_TEST_SETUP_KEY"

	t.Setenv(key, "from-env")

	val, source, ok := resolveEnvValue(map[string]string{
		key: "from-config",
	}, key)
	if !ok {
		t.Fatalf("expected value to be resolved")
	}
	if val != "from-env" {
		t.Fatalf("expected env value, got %q", val)
	}
	if source != "env" {
		t.Fatalf("expected env source, got %q", source)
	}
}

func TestResolveEnvValueFallsBackToConfig(t *testing.T) {
	const key = "CHB_TEST_SETUP_KEY_FALLBACK"

	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unsetenv failed: %v", err)
	}

	val, source, ok := resolveEnvValue(map[string]string{
		key: "from-config",
	}, key)
	if !ok {
		t.Fatalf("expected value to be resolved")
	}
	if val != "from-config" {
		t.Fatalf("expected config value, got %q", val)
	}
	if source != "config.env" {
		t.Fatalf("expected config.env source, got %q", source)
	}
}
