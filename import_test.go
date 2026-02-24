package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()

	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		t.Fatalf("write %s failed: %v", path, err)
	}
}

func mustSetMtime(t *testing.T, path string, mtime time.Time) {
	t.Helper()

	err := os.Chtimes(path, time.Now(), mtime)
	if err != nil {
		t.Fatalf("set mtime %s failed: %v", path, err)
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s failed: %v", path, err)
	}
	return string(b)
}

func assertMtimeClose(t *testing.T, path string, expected time.Time, tolerance time.Duration) {
	t.Helper()

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s failed: %v", path, err)
	}

	delta := fi.ModTime().Sub(expected)
	if delta < 0 {
		delta = -delta
	}
	if delta > tolerance {
		t.Fatalf("mtime mismatch: got=%s expected=%s delta=%s", fi.ModTime(), expected, delta)
	}
}

func TestCopyFileCopiesContentsAndMtime(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	dst := filepath.Join(tmp, "dst.txt")

	mustWriteFile(t, src, "hello world")
	expectedMtime := time.Date(2021, 4, 12, 13, 14, 15, 0, time.UTC)
	mustSetMtime(t, src, expectedMtime)

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile returned error: %v", err)
	}

	if got := readFileString(t, dst); got != "hello world" {
		t.Fatalf("destination content mismatch: %q", got)
	}
	assertMtimeClose(t, dst, expectedMtime, time.Second)
}

func TestCopyFileOverwritesExistingDestination(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	dst := filepath.Join(tmp, "dst.txt")

	mustWriteFile(t, src, "new content")
	mustWriteFile(t, dst, "old content")

	expectedMtime := time.Date(2022, 1, 2, 3, 4, 5, 0, time.UTC)
	mustSetMtime(t, src, expectedMtime)

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile returned error: %v", err)
	}

	if got := readFileString(t, dst); got != "new content" {
		t.Fatalf("destination content mismatch: %q", got)
	}
	assertMtimeClose(t, dst, expectedMtime, time.Second)
}

func TestCopyFileSourceDoesNotExist(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "missing.txt")
	dst := filepath.Join(tmp, "dst.txt")

	err := copyFile(src, dst)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got: %v", err)
	}
}

func TestCopyFileRejectsDirectorySource(t *testing.T) {
	tmp := t.TempDir()
	srcDir := filepath.Join(tmp, "source-dir")
	dst := filepath.Join(tmp, "dst.txt")

	if err := os.Mkdir(srcDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	err := copyFile(srcDir, dst)
	if err == nil {
		t.Fatal("expected non-regular source error")
	}
	if !strings.Contains(err.Error(), "Non-regular source file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCopyFileRejectsDirectoryDestination(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	dstDir := filepath.Join(tmp, "dest-dir")

	mustWriteFile(t, src, "data")
	if err := os.Mkdir(dstDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	err := copyFile(src, dstDir)
	if err == nil {
		t.Fatal("expected non-regular destination error")
	}
	if !strings.Contains(err.Error(), "Non-regular destination file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCopyFileSamePathIsNoop(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "same.txt")
	mustWriteFile(t, src, "same file")

	before := readFileString(t, src)
	if err := copyFile(src, src); err != nil {
		t.Fatalf("copyFile same-path returned error: %v", err)
	}
	after := readFileString(t, src)
	if after != before {
		t.Fatalf("file content unexpectedly changed: before=%q after=%q", before, after)
	}
}

func TestCopyFileContentsCopiesEmptyFile(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "empty.txt")
	dst := filepath.Join(tmp, "dst.txt")
	mustWriteFile(t, src, "")

	mtime := time.Date(2020, 7, 8, 9, 10, 11, 0, time.UTC)
	if err := copyFileContents(src, dst, mtime); err != nil {
		t.Fatalf("copyFileContents returned error: %v", err)
	}

	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if fi.Size() != 0 {
		t.Fatalf("expected empty destination, size=%d", fi.Size())
	}
	assertMtimeClose(t, dst, mtime, time.Second)
}

func TestCopyFileContentsSourceDoesNotExist(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "missing.txt")
	dst := filepath.Join(tmp, "dst.txt")

	err := copyFileContents(src, dst, time.Now())
	if err == nil {
		t.Fatal("expected error for missing source")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got: %v", err)
	}
}

func TestCopyFileContentsFailsWhenDestinationParentMissing(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	mustWriteFile(t, src, "data")

	dst := filepath.Join(tmp, "missing-parent", "dst.txt")
	err := copyFileContents(src, dst, time.Now())
	if err == nil {
		t.Fatal("expected error for missing destination parent")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got: %v", err)
	}
}

func TestCopyFileContentsSetsRequestedMtime(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	dst := filepath.Join(tmp, "dst.txt")

	mustWriteFile(t, src, "mtime target")
	mtime := time.Date(2019, 11, 20, 21, 22, 23, 0, time.UTC)

	if err := copyFileContents(src, dst, mtime); err != nil {
		t.Fatalf("copyFileContents returned error: %v", err)
	}
	assertMtimeClose(t, dst, mtime, time.Second)
}
