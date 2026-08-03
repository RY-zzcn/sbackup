package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"sbackup/internal/config"
	"sbackup/internal/store"
)

func TestLocalBackupRestoreFlowWithResticProtocol(t *testing.T) {
	root := t.TempDir()
	fake := filepath.Join(root, "restic")
	script := `#!/bin/sh
case " $* " in
  *" backup "*) echo '{"message_type":"summary","snapshot_id":"abcdef123456","files_new":1,"files_changed":2,"files_unmodified":3,"data_added":4,"total_bytes_processed":5}' ;;
  *" snapshots "*) echo '[{"id":"abcdef123456","short_id":"abcdef12","time":"2026-08-03T00:00:00Z","hostname":"test","tags":["sbackup-job=job"],"paths":["/source"]}]' ;;
  *" restore "*) exit 0 ;;
  *" check "*) exit 0 ;;
  *" forget "*) exit 0 ;;
  *) exit 0 ;;
esac
`
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	password := filepath.Join(root, "password")
	if err := os.WriteFile(password, []byte("secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := config.Default()
	c.Global.StateDB = filepath.Join(root, "state.db")
	c.Global.TempDir = filepath.Join(root, "tmp")
	c.Global.LogDir = filepath.Join(root, "logs")
	c.Tools.ResticPath = fake
	c.Storages = []config.Storage{{ID: "local", Name: "local", Type: "local", RepositoryPath: filepath.Join(root, "repo"), PasswordFile: password}}
	c.Jobs = []config.Job{{ID: "job", Name: "job", Enabled: true, StorageID: "local", Sources: config.Sources{Paths: []string{source}, StrictPaths: true}, Retention: config.Retention{KeepLast: 1}}}
	st, err := store.Open(c.Global.StateDB)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := Service{Config: &c, Store: st}
	run, err := svc.Run(context.Background(), "job", false)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "success" || run.SnapshotID != "abcdef123456" || run.FilesChanged != 2 {
		t.Fatalf("unexpected run: %#v", run)
	}
	snaps, err := svc.Snapshots(context.Background(), "job")
	if err != nil || len(snaps) != 1 || snaps[0].ShortID != "abcdef12" {
		t.Fatalf("snapshots=%#v err=%v", snaps, err)
	}
	if err := svc.Restore(context.Background(), "job", "latest", filepath.Join(root, "restore"), nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.Verify(context.Background(), "job", "standard", nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.Forget(context.Background(), "job", true, nil); err != nil {
		t.Fatal(err)
	}
}
