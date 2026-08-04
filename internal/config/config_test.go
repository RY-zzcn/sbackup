package config

import (
	"path/filepath"
	"testing"
)

func validConfig(t *testing.T) Config {
	t.Helper()
	c := Default()
	c.Global.StateDB = filepath.Join(t.TempDir(), "state.db")
	c.Storages = []Storage{{ID: "local", Name: "local", Type: "local", RepositoryPath: "/tmp/repo", PasswordFile: "/tmp/repo.pass"}}
	c.Jobs = []Job{{ID: "job", Name: "job", Enabled: true, StorageID: "local", Sources: Sources{Paths: []string{"/tmp"}}, Retention: Retention{KeepLast: 1}}}
	return c
}

func TestValidateRejectsDuplicateSources(t *testing.T) {
	c := validConfig(t)
	c.Jobs[0].Sources.Paths = []string{"/tmp", "/tmp"}
	if err := c.Validate(); err == nil {
		t.Fatal("expected duplicate source error")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	c := validConfig(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, &c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Jobs[0].ID != "job" || got.Storages[0].Type != "local" {
		t.Fatalf("unexpected round trip: %#v", got)
	}
}

func TestValidateReadSubset(t *testing.T) {
	c := validConfig(t)
	for _, value := range []string{"0%", "101%", "2/1", "invalid"} {
		c.Jobs[0].Verification.FullReadDataSubset = value
		if err := c.Validate(); err == nil {
			t.Fatalf("invalid subset accepted: %s", value)
		}
	}
	for _, value := range []string{"1%", "100%", "1/10"} {
		c.Jobs[0].Verification.FullReadDataSubset = value
		if err := c.Validate(); err != nil {
			t.Fatalf("valid subset rejected %s: %v", value, err)
		}
	}
}

func TestValidateMonitoringDurations(t *testing.T) {
	c := validConfig(t)
	c.Monitoring = Monitoring{Enabled: true, Endpoint: "https://monitor.example/api/v1/report", NodeID: "node", KeyFile: "/tmp/key", KeyVersion: 1, RequestTimeout: "bad", EventRetention: "30d"}
	if err := c.Validate(); err == nil {
		t.Fatal("invalid monitoring duration accepted")
	}
}

func TestValidateRejectsUnsafeGlobalSettings(t *testing.T) {
	c := validConfig(t)
	c.Global.TempDir = "relative/tmp"
	if err := c.Validate(); err == nil {
		t.Fatal("relative temp_dir accepted")
	}
	c = validConfig(t)
	c.Global.MaxParallelJobs = 0
	if err := c.Validate(); err == nil {
		t.Fatal("zero max_parallel_jobs accepted")
	}
}

func TestValidateRejectsRelativeSQLitePath(t *testing.T) {
	c := validConfig(t)
	c.Databases = []Database{{ID: "sqlite", Name: "sqlite", Type: "sqlite", Path: "relative.db"}}
	if err := c.Validate(); err == nil {
		t.Fatal("relative SQLite path accepted")
	}
}

func TestValidateScheduleTypesAndBackupMode(t *testing.T) {
	c := validConfig(t)
	c.Jobs[0].Schedule = Schedule{Enabled: true, Type: "interval", Expression: "6h"}
	c.Jobs[0].Restic.BackupMode = "full"
	if err := c.Validate(); err != nil {
		t.Fatalf("valid interval/full configuration rejected: %v", err)
	}
	c.Jobs[0].Schedule.Expression = "10s"
	if err := c.Validate(); err == nil {
		t.Fatal("too short interval accepted")
	}
	c = validConfig(t)
	c.Jobs[0].Restic.BackupMode = "legacy"
	if err := c.Validate(); err == nil {
		t.Fatal("invalid backup mode accepted")
	}
	c = validConfig(t)
	c.Jobs[0].Restic.Compression = "invalid"
	if err := c.Validate(); err == nil {
		t.Fatal("invalid compression accepted")
	}
}
