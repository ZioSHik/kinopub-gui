package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// countTempFiles returns the number of ".tmp-*" files left behind in dir.
func countTempFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", dir, err)
	}
	n := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			n++
		}
	}
	return n
}

// TestAtomicWrite_NoTempLeftOnSuccess verifies the temp file is consumed by the
// rename and no stray ".tmp-*" files remain after a successful write.
func TestAtomicWrite_NoTempLeftOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.bin")

	if err := AtomicWrite(path, []byte("payload"), 0644); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}
	if n := countTempFiles(t, dir); n != 0 {
		t.Errorf("found %d leftover temp files, want 0", n)
	}
	// Exactly one regular file should exist: the target.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want 1 (the target)", len(entries))
	}
}

// TestAtomicWrite_RenameFailureLeavesDestUntouched verifies that when the final
// rename fails, the pre-existing destination content is preserved unchanged and
// no temp file is leaked. We force the rename to fail by making the destination
// a non-empty directory, which os.Rename(file, dir) refuses to overwrite.
func TestAtomicWrite_RenameFailureLeavesDestUntouched(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")

	// target is a directory containing a child, so rename-over fails.
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(target, "keep.txt")
	if err := os.WriteFile(child, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}

	err := AtomicWrite(target, []byte("new data"), 0644)
	if err == nil {
		t.Fatal("expected error when renaming over a non-empty directory")
	}
	if !strings.Contains(err.Error(), "rename temp to target") {
		t.Errorf("error = %v, want it to mention rename failure", err)
	}

	// The destination directory and its contents must be untouched.
	info, statErr := os.Stat(target)
	if statErr != nil {
		t.Fatalf("Stat target: %v", statErr)
	}
	if !info.IsDir() {
		t.Errorf("target is no longer a directory after failed write")
	}
	got, readErr := os.ReadFile(child)
	if readErr != nil {
		t.Fatalf("ReadFile child: %v", readErr)
	}
	if string(got) != "original" {
		t.Errorf("child content = %q, want %q (must be untouched)", got, "original")
	}

	// The temp file must have been cleaned up despite the rename failure.
	if n := countTempFiles(t, dir); n != 0 {
		t.Errorf("found %d leftover temp files after failed rename, want 0", n)
	}
}

// TestAtomicWrite_CreateTempFailureNoSideEffects verifies that when the temp
// file cannot be created (parent dir does not exist), AtomicWrite returns a
// wrapped error and creates nothing.
func TestAtomicWrite_CreateTempFailureNoSideEffects(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist")
	path := filepath.Join(missing, "file.txt")

	err := AtomicWrite(path, []byte("x"), 0644)
	if err == nil {
		t.Fatal("expected error for non-existent parent directory")
	}
	if !strings.Contains(err.Error(), "create temp file") {
		t.Errorf("error = %v, want it to mention temp file creation", err)
	}
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Errorf("the missing directory should not have been created")
	}
}

// TestAtomicWrite_EmptyData verifies that writing empty data produces an empty
// file (not an error, not a missing file).
func TestAtomicWrite_EmptyData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")

	if err := AtomicWrite(path, nil, 0644); err != nil {
		t.Fatalf("AtomicWrite(nil): %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("size = %d, want 0", info.Size())
	}
}

// TestAtomicWrite_LargeData round-trips a payload bigger than a typical pipe
// buffer to exercise the write path with multi-segment data.
func TestAtomicWrite_LargeData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.bin")
	data := make([]byte, 1<<20) // 1 MiB
	for i := range data {
		data[i] = byte(i % 251)
	}

	if err := AtomicWrite(path, data, 0600); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(got) != len(data) {
		t.Fatalf("len = %d, want %d", len(got), len(data))
	}
	for i := range data {
		if got[i] != data[i] {
			t.Fatalf("byte %d = %d, want %d", i, got[i], data[i])
		}
	}
}

// TestAtomicWrite_RespectsPermModes verifies the requested permission bits land
// on the final file across a couple of common modes (Unix only).
func TestAtomicWrite_RespectsPermModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits not meaningful on Windows")
	}
	// Use a permissive umask so requested bits are not masked away.
	old := syscallUmask(0)
	defer syscallUmask(old)

	for _, perm := range []os.FileMode{0600, 0640, 0644, 0664, 0755} {
		dir := t.TempDir()
		path := filepath.Join(dir, "f")
		if err := AtomicWrite(path, []byte("data"), perm); err != nil {
			t.Fatalf("AtomicWrite(perm=%o): %v", perm, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if info.Mode().Perm() != perm {
			t.Errorf("perm = %o, want %o", info.Mode().Perm(), perm)
		}
	}
}

// TestAtomicWrite_OverwriteReplacesContentAndPerm verifies overwriting an
// existing file both swaps its content and applies the new permission bits.
func TestAtomicWrite_OverwriteReplacesContentAndPerm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("old-old-old"), 0666); err != nil {
		t.Fatal(err)
	}

	if err := AtomicWrite(path, []byte("new"), 0600); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Errorf("content = %q, want %q", got, "new")
	}
	if runtime.GOOS != "windows" {
		old := syscallUmask(0)
		syscallUmask(old)
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Errorf("perm = %o, want 0600", info.Mode().Perm())
		}
	}
	if n := countTempFiles(t, dir); n != 0 {
		t.Errorf("leftover temp files = %d, want 0", n)
	}
}

// TestAtomicRename_Error verifies AtomicRename wraps and reports failures, and
// that the wrapped message includes both paths.
func TestAtomicRename_Error(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "does-not-exist")
	dst := filepath.Join(dir, "dst")

	err := AtomicRename(src, dst)
	if err == nil {
		t.Fatal("expected error renaming a non-existent source")
	}
	if !strings.Contains(err.Error(), "rename") {
		t.Errorf("error = %v, want it to mention rename", err)
	}
	if !strings.Contains(err.Error(), src) || !strings.Contains(err.Error(), dst) {
		t.Errorf("error = %v, want it to include both src %q and dst %q", err, src, dst)
	}
}

// TestAtomicRename_OverwritesExistingFile verifies renaming over an existing
// regular file succeeds and replaces its content (POSIX rename semantics).
func TestAtomicRename_OverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("fresh"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := AtomicRename(src, dst); err != nil {
		t.Fatalf("AtomicRename: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "fresh" {
		t.Errorf("dst content = %q, want %q", got, "fresh")
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src should be gone after rename")
	}
}
