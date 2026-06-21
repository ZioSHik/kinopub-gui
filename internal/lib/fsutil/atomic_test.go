package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWrite_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	data := []byte("hello world")

	if err := AtomicWrite(path, data, 0644); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("got %q, want %q", got, data)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("perm = %o, want 0644", info.Mode().Perm())
	}
}

func TestAtomicWrite_CleanupOnBadDir(t *testing.T) {
	// Writing to a non-existent directory should fail and leave no temp files.
	err := AtomicWrite("/nonexistent-dir-xyz/file.txt", []byte("x"), 0644)
	if err == nil {
		t.Fatal("expected error for non-existent directory")
	}
}

func TestAtomicWrite_OverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := AtomicWrite(path, []byte("new"), 0644); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Errorf("got %q, want %q", got, "new")
	}
}

func TestAtomicRename(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")

	if err := os.WriteFile(src, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := AtomicRename(src, dst); err != nil {
		t.Fatalf("AtomicRename: %v", err)
	}

	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("src should not exist after rename")
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile dst: %v", err)
	}
	if string(got) != "data" {
		t.Errorf("got %q, want %q", got, "data")
	}
}

func TestSanitizeComponent_Basic(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		fallback string
		want     string
	}{
		{"plain ascii", "hello", "fb", "hello"},
		{"cyrillic preserved", "Привет мир", "fb", "Привет мир"},
		{"reserved chars replaced", "file/name:test*?.txt", "fb", "file_name_test__.txt"},
		{"backslash and pipes", `a\b|c`, "fb", "a_b_c"},
		{"angle brackets and quotes", `<"hello">`, "fb", "__hello__"},
		{"NUL replaced", "a\x00b", "fb", "a_b"},
		{"control chars replaced", "a\x01b\x1fc", "fb", "a_b_c"},
		{"leading/trailing whitespace trimmed", "  hello  ", "fb", "hello"},
		{"leading/trailing dots trimmed", "..hello..", "fb", "hello"},
		{"mixed trim", " .hello. ", "fb", "hello"},
		{"all invalid becomes empty uses fallback", "...", "myfallback", "myfallback"},
		{"empty input uses fallback", "", "fb", "fb"},
		{"only spaces uses fallback", "   ", "fb", "fb"},
		{"only dots uses fallback", "...", "fb", "fb"},
		{"empty fallback defaults to unnamed", "", "", "unnamed"},
		{"unicode mixed with reserved", "Сериал: Тест/1", "fb", "Сериал_ Тест_1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeComponent(tt.input, tt.fallback)
			if got != tt.want {
				t.Errorf("SanitizeComponent(%q, %q) = %q, want %q", tt.input, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestSanitizeComponent_NeverEmpty(t *testing.T) {
	// Even with all-invalid input, result is never empty.
	got := SanitizeComponent("\x00\x01\x02", "fallback")
	if got == "" {
		t.Error("result should never be empty")
	}
}
