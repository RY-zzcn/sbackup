package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateRestoreTarget(t *testing.T) {
	root := t.TempDir()
	valid := filepath.Join(root, "restore")
	if err := validateRestoreTarget(valid); err != nil {
		t.Fatalf("valid target rejected: %v", err)
	}
	for _, target := range []string{"", "/", "relative/path"} {
		if err := validateRestoreTarget(target); err == nil {
			t.Fatalf("dangerous target accepted: %q", target)
		}
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink("/", link); err != nil {
		t.Fatal(err)
	}
	if err := validateRestoreTarget(link); err == nil {
		t.Fatal("symlink target accepted")
	}
	if err := validateRestoreTarget(filepath.Join(link, "child")); err == nil {
		t.Fatal("target below root symlink accepted")
	}
}
