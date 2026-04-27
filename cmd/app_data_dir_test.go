package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppDataDirDefaultsToHomeCHB(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APP_DATA_DIR", "")

	want := filepath.Join(home, ".chb")
	got := AppDataDir()
	if got != want {
		t.Fatalf("AppDataDir() = %q, want %q", got, want)
	}
	assertMode(t, got, 0755)
}

func TestAppDataDirUsesEnvOverride(t *testing.T) {
	appDir := filepath.Join(t.TempDir(), "app")
	t.Setenv("APP_DATA_DIR", appDir)

	got := AppDataDir()
	if got != appDir {
		t.Fatalf("AppDataDir() = %q, want %q", got, appDir)
	}
	assertMode(t, got, 0755)
}

func TestDataDirDefaultsUnderAppDataDir(t *testing.T) {
	appDir := filepath.Join(t.TempDir(), "app")
	t.Setenv("APP_DATA_DIR", appDir)
	t.Setenv("DATA_DIR", "")

	want := filepath.Join(appDir, "data")
	got := DataDir()
	if got != want {
		t.Fatalf("DataDir() = %q, want %q", got, want)
	}
	assertMode(t, got, 0755)
}

func TestDataDirEnvOverridesAppDataDir(t *testing.T) {
	appDir := filepath.Join(t.TempDir(), "app")
	dataDir := filepath.Join(t.TempDir(), "data")
	t.Setenv("APP_DATA_DIR", appDir)
	t.Setenv("DATA_DIR", dataDir)

	got := DataDir()
	if got != dataDir {
		t.Fatalf("DataDir() = %q, want %q", got, dataDir)
	}
	if _, err := os.Stat(filepath.Join(appDir, "data")); !os.IsNotExist(err) {
		t.Fatalf("APP_DATA_DIR/data should not be created when DATA_DIR is set")
	}
	assertMode(t, dataDir, 0755)
}

func TestSettingsDirUsesAppDataDirOverride(t *testing.T) {
	appDir := filepath.Join(t.TempDir(), "app")
	t.Setenv("APP_DATA_DIR", appDir)

	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("mkdir app dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "settings.json"), []byte(`{}`), 0644); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	got := settingsDir()
	if got != appDir {
		t.Fatalf("settingsDir() = %q, want %q", got, appDir)
	}
}

func TestConfigFilesUseAppDataDir(t *testing.T) {
	appDir := filepath.Join(t.TempDir(), "app")
	t.Setenv("APP_DATA_DIR", appDir)

	cases := map[string]string{
		"accounts":    accountsConfigPath(),
		"categories":  categoriesPath(),
		"collectives": collectivesPath(),
		"config.env":  configEnvPath(),
		"rules":       rulesPath(),
		"nostr keys":  nostrKeysPath(),
	}

	for name, got := range cases {
		if !filepath.IsAbs(got) {
			t.Fatalf("%s path is not absolute: %q", name, got)
		}
		if filepath.Dir(got) != appDir {
			t.Fatalf("%s path dir = %q, want %q", name, filepath.Dir(got), appDir)
		}
	}
}
