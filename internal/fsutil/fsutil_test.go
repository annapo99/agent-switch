package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileReplacesThroughSymlinksAndKeepsItPrivate(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "config.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := WriteFile(link, []byte("new")); err != nil {
		t.Fatal(err)
	}

	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink was replaced: %v %v", info, err)
	}
	data, err := os.ReadFile(target)
	info, statErr := os.Stat(target)
	if err != nil || string(data) != "new" || statErr != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("target data=%q perm=%v err=%v", data, info.Mode().Perm(), err)
	}
	entries, _ := os.ReadDir(filepath.Dir(target))
	if len(entries) != 1 {
		t.Fatalf("temporary files were left behind: %v", entries)
	}
}
