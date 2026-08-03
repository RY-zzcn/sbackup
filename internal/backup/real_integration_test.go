package backup

import (
	"context"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"sbackup/internal/config"
	"sbackup/internal/repository"
	"sbackup/internal/store"
)

func TestRealResticLocalRepositoryFlow(t *testing.T) {
	restic, err := exec.LookPath("restic")
	if err != nil {
		t.Skip("restic 未安装，跳过真实仓库演练")
	}
	root := t.TempDir()
	source := filepath.Join(root, "source")
	repo := filepath.Join(root, "repository")
	password := filepath.Join(root, "repository.pass")
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	content := []byte("sbackup real restic integration\n")
	if err := os.WriteFile(filepath.Join(source, "payload.txt"), content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(password, []byte("test-only-password\n"), 0600); err != nil {
		t.Fatal(err)
	}

	c := config.Default()
	c.Global.StateDB = filepath.Join(root, "state.db")
	c.Global.TempDir = filepath.Join(root, "tmp")
	c.Global.LogDir = filepath.Join(root, "logs")
	c.Tools.ResticPath = restic
	c.Storages = []config.Storage{{ID: "local", Name: "local", Type: "local", RepositoryPath: repo, PasswordFile: password}}
	c.Jobs = []config.Job{{ID: "real", Name: "real", Enabled: true, StorageID: "local", Sources: config.Sources{Paths: []string{source}, StrictPaths: true}, Retention: config.Retention{KeepLast: 2}, Verification: config.Verification{MetadataAfterBackup: true}}}

	if err := repository.Init(context.Background(), &c, c.Storages[0], nil); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(c.Global.StateDB)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := Service{Config: &c, Store: st}
	first, err := svc.Run(context.Background(), "real", false)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "success" || first.SnapshotID == "" {
		t.Fatalf("unexpected first run: %#v", first)
	}
	if err := os.WriteFile(filepath.Join(source, "payload.txt"), append(content, []byte("changed\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := svc.Run(context.Background(), "real", false)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != "success" || second.SnapshotID == first.SnapshotID {
		t.Fatalf("unexpected second run: %#v", second)
	}
	snapshots, err := svc.Snapshots(context.Background(), "real")
	if err != nil || len(snapshots) != 2 {
		t.Fatalf("snapshots=%#v err=%v", snapshots, err)
	}
	files, err := svc.SnapshotFiles(context.Background(), "real", first.SnapshotID, "payload.txt")
	if err != nil || len(files) != 1 {
		t.Fatalf("snapshot files=%#v err=%v", files, err)
	}
	if err := svc.Verify(context.Background(), "real", "metadata", nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.Verify(context.Background(), "real", "standard", nil); err != nil {
		t.Fatal(err)
	}
	restoreTarget := filepath.Join(root, "restore")
	if err := svc.Restore(context.Background(), "real", first.SnapshotID, restoreTarget, nil, nil); err != nil {
		t.Fatal(err)
	}
	var restored string
	_ = filepath.Walk(restoreTarget, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr == nil && info != nil && info.Name() == "payload.txt" {
			restored = path
		}
		return nil
	})
	got, err := os.ReadFile(restored)
	if err != nil || string(got) != string(content) {
		t.Fatalf("restored=%q err=%v", restored, err)
	}
	if sha256.Sum256(got) != sha256.Sum256(content) {
		t.Fatal("restored file hash mismatch")
	}
}
