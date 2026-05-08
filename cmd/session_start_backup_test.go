package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEmitBrainAutoBackup_ConfigDisabledSkips(t *testing.T) {
	setupGGDir(t)
	cfgPath := filepath.Join(".gg", "config.yaml")
	cfg := `project_id: test-project-fixture
qdrant:
  host: localhost
  port: 6334
embedding:
  host: http://localhost:11434
  model: nomic-embed-text
memgraph:
  uri: bolt://localhost:7687
  username: ""
  password: ""
backup:
  enabled: false
  interval: 24h
  timeout: 30s
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	orig := os.Getenv("GG_AUTO_BACKUP")
	t.Setenv("GG_AUTO_BACKUP", orig)
	t.Setenv("GG_AUTO_BACKUP_INTERVAL", "")

	var out, errBuf bytes.Buffer
	emitBrainAutoBackup(&out, &errBuf)
	time.Sleep(50 * time.Millisecond)

	if out.Len() != 0 {
		t.Fatalf("expected no stdout when backup disabled, got: %q", out.String())
	}
	if errBuf.Len() != 0 {
		t.Fatalf("expected no stderr when backup disabled, got: %q", errBuf.String())
	}
}

func TestResolveBrainAutoBackupSettings_UsesProjectConfig(t *testing.T) {
	setupGGDir(t)
	cfgPath := filepath.Join(".gg", "config.yaml")
	cfg := `project_id: test-project-fixture
qdrant:
  host: localhost
  port: 6334
embedding:
  host: http://localhost:11434
  model: nomic-embed-text
memgraph:
  uri: bolt://localhost:7687
backup:
  enabled: true
  interval: 6h
  timeout: 45s
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("GG_AUTO_BACKUP", "")
	t.Setenv("GG_AUTO_BACKUP_INTERVAL", "")

	settings, ok := resolveBrainAutoBackupSettings(&bytes.Buffer{})
	if !ok {
		t.Fatal("expected backup settings to resolve")
	}
	if settings.interval != "6h" {
		t.Fatalf("interval = %q, want 6h", settings.interval)
	}
	if settings.timeout != 45*time.Second {
		t.Fatalf("timeout = %s, want 45s", settings.timeout)
	}
}

func TestResolveBrainAutoBackupSettings_EnvIntervalOverridesConfig(t *testing.T) {
	setupGGDir(t)
	cfgPath := filepath.Join(".gg", "config.yaml")
	cfg := `project_id: test-project-fixture
qdrant:
  host: localhost
  port: 6334
embedding:
  host: http://localhost:11434
  model: nomic-embed-text
memgraph:
  uri: bolt://localhost:7687
backup:
  enabled: true
  interval: 6h
  timeout: 45s
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("GG_AUTO_BACKUP", "")
	t.Setenv("GG_AUTO_BACKUP_INTERVAL", "2h")

	settings, ok := resolveBrainAutoBackupSettings(&bytes.Buffer{})
	if !ok {
		t.Fatal("expected backup settings to resolve")
	}
	if settings.interval != "2h" {
		t.Fatalf("interval = %q, want env override 2h", settings.interval)
	}
	if settings.timeout != 45*time.Second {
		t.Fatalf("timeout = %s, want config timeout 45s", settings.timeout)
	}
}

func TestResolveBrainAutoBackupSettings_DefaultsWhenConfigBlockMissing(t *testing.T) {
	setupGGDir(t)
	t.Setenv("GG_AUTO_BACKUP", "")
	t.Setenv("GG_AUTO_BACKUP_INTERVAL", "")

	settings, ok := resolveBrainAutoBackupSettings(&bytes.Buffer{})
	if !ok {
		t.Fatal("expected backup settings to resolve")
	}
	if settings.interval != "24h" {
		t.Fatalf("interval = %q, want 24h", settings.interval)
	}
	if settings.timeout != 30*time.Second {
		t.Fatalf("timeout = %s, want 30s", settings.timeout)
	}
}

func TestEmitBrainAutoBackup_InvalidTimeoutWarns(t *testing.T) {
	setupGGDir(t)
	cfgPath := filepath.Join(".gg", "config.yaml")
	cfg := `project_id: test-project-fixture
qdrant:
  host: localhost
  port: 6334
embedding:
  host: http://localhost:11434
  model: nomic-embed-text
memgraph:
  uri: bolt://localhost:7687
  username: ""
  password: ""
backup:
  enabled: true
  interval: 24h
  timeout: nope
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("GG_AUTO_BACKUP", "")
	t.Setenv("GG_AUTO_BACKUP_INTERVAL", "")

	var out, errBuf bytes.Buffer
	emitBrainAutoBackup(&out, &errBuf)
	time.Sleep(50 * time.Millisecond)

	if !strings.Contains(errBuf.String(), "backup.timeout must be a valid duration") {
		t.Fatalf("expected invalid backup timeout warning, got: %q", errBuf.String())
	}
}
