package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func parseYAML(t *testing.T, body string) (map[string]string, map[string][]string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scalars, lists, err := parseSimpleYAML(f)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	return scalars, lists
}

func TestParseTopLevelList(t *testing.T) {
	_, lists := parseYAML(t, `webhook:
  urls:
    - https://a.com
    - "https://b.com"
`)
	want := []string{"https://a.com", "https://b.com"}
	if got := lists["webhook.urls"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("webhook.urls = %v, want %v", got, want)
	}
}

func TestParseScalarsAndListsCoexist(t *testing.T) {
	scalars, lists := parseYAML(t, `queue:
  type: "memory"
  hosts:
    - a
    - b
  size: 100
`)
	if scalars["queue.type"] != "memory" {
		t.Fatalf("queue.type = %q", scalars["queue.type"])
	}
	if scalars["queue.size"] != "100" {
		t.Fatalf("queue.size = %q", scalars["queue.size"])
	}
	if got := lists["queue.hosts"]; !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("queue.hosts = %v", got)
	}
}

func TestParseRejectsScalarThenList(t *testing.T) {
	// A key with a scalar value can't also carry list items.
	p := filepath.Join(t.TempDir(), "c.yaml")
	os.WriteFile(p, []byte("key: value\n  - oops\n"), 0o600)
	f, _ := os.Open(p)
	defer f.Close()
	if _, _, err := parseSimpleYAML(f); err == nil {
		t.Fatal("expected error for scalar key followed by list item")
	}
}

func TestParseRejectsOrphanListItem(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	os.WriteFile(p, []byte("- orphan\n"), 0o600)
	f, _ := os.Open(p)
	defer f.Close()
	_, _, err := parseSimpleYAML(f)
	if err == nil || !strings.Contains(err.Error(), "without a parent") {
		t.Fatalf("expected orphan-list error, got %v", err)
	}
}

func TestQueueConfigFromQueueBlock(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(p, []byte(`api_token: secret
scheduler:
  max_lateness: 2m
queue:
  type: memory
  workers: 7
  size: 250
  rate_limit_per_sec: 5
`), 0o600)
	t.Setenv("CONFIG_FILE", p)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.QueueConfig.Workers != 7 {
		t.Errorf("Workers = %d, want 7", cfg.QueueConfig.Workers)
	}
	if cfg.QueueConfig.Size != 250 {
		t.Errorf("Size = %d, want 250", cfg.QueueConfig.Size)
	}
	if cfg.QueueConfig.RateLimitPerSec != 5 {
		t.Errorf("RateLimitPerSec = %d, want 5", cfg.QueueConfig.RateLimitPerSec)
	}
	if cfg.SchedulerMaxLateness != 2*time.Minute {
		t.Errorf("MaxLateness = %v, want 2m", cfg.SchedulerMaxLateness)
	}
}

func TestLegacySchedulerWorkersIgnored(t *testing.T) {
	// scheduler.workers / scheduler.queue_size are no longer read; the queue
	// block (or its defaults) is authoritative. Here only legacy keys are set,
	// so the defaults 10/100 must apply.
	p := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(p, []byte(`api_token: secret
scheduler:
  workers: 99
  queue_size: 999
  max_lateness: 1m
`), 0o600)
	t.Setenv("CONFIG_FILE", p)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.QueueConfig.Workers != 10 {
		t.Errorf("Workers = %d, want default 10 (legacy scheduler.workers must be ignored)", cfg.QueueConfig.Workers)
	}
	if cfg.QueueConfig.Size != 100 {
		t.Errorf("Size = %d, want default 100 (legacy scheduler.queue_size must be ignored)", cfg.QueueConfig.Size)
	}
}

func TestLoadPopulatesListsField(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	os.WriteFile(p, []byte(`api_token: secret
webhook:
  urls:
    - https://a.com
    - https://b.com
`), 0o600)
	t.Setenv("CONFIG_FILE", p)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Lists["webhook.urls"]; !reflect.DeepEqual(got, []string{"https://a.com", "https://b.com"}) {
		t.Fatalf("cfg.Lists[webhook.urls] = %v", got)
	}
}
