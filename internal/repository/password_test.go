package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreatePasswordFileGeneratedAndNoOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets", "repo.pass")
	password, generated, err := CreatePasswordFile(path, "")
	if err != nil || !generated || len(password) < 40 {
		t.Fatalf("password=%q generated=%v err=%v", password, generated, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	b, _ := os.ReadFile(path)
	if strings.TrimSpace(string(b)) != password {
		t.Fatal("password file content mismatch")
	}
	if _, _, err := CreatePasswordFile(path, "another-secure-password"); err == nil {
		t.Fatal("existing password file was overwritten")
	}
}

func TestCreatePasswordFileCustomValidation(t *testing.T) {
	if _, _, err := CreatePasswordFile(filepath.Join(t.TempDir(), "short.pass"), "short"); err == nil {
		t.Fatal("short password accepted")
	}
	path := filepath.Join(t.TempDir(), "custom.pass")
	want := "correct-horse-battery-staple"
	got, generated, err := CreatePasswordFile(path, want)
	if err != nil || generated || got != want {
		t.Fatalf("got=%q generated=%v err=%v", got, generated, err)
	}
}
