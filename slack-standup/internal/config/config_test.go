package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadOrCreateWritesDefaultConfigOnFirstRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "standup_config.toml")

	cfg, created, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}
	if !created {
		t.Error("created = false, want true when the file did not exist")
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Errorf("cfg = %+v, want the default config", cfg)
	}

	reloaded, created, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("second LoadOrCreate() error = %v", err)
	}
	if created {
		t.Error("created = true on second load, want false")
	}
	if !reflect.DeepEqual(reloaded, Default()) {
		t.Errorf("reloaded = %+v, want it to round-trip the default config", reloaded)
	}
}

func TestLoadOrCreateReadsExistingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "standup_config.toml")
	contents := "[github]\nusername = \"kcotten\"\nrepos = [\"acme/api\"]\n\n[slack]\nchannel = \"#team\"\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, created, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}
	if created {
		t.Error("created = true, want false for an existing file")
	}
	if cfg.GitHub.Username != "kcotten" || cfg.Slack.Channel != "#team" {
		t.Errorf("cfg = %+v, did not pick up file values", cfg)
	}
}

func TestLoadOrCreateRejectsMalformedConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "standup_config.toml")
	if err := os.WriteFile(path, []byte("[github\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := LoadOrCreate(path); err == nil {
		t.Error("LoadOrCreate() error = nil, want a parse error")
	}
}
