package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseFlagsNormalizesFilterAndWorkers(t *testing.T) {
	cfg, err := parseFlags([]string{"--from", "/src", "--to", "/dst", "--filter", "JPG", "--workers", "3"})
	if err != nil {
		t.Fatalf("parseFlags returned error: %v", err)
	}
	if cfg.Filter != "jpg" {
		t.Fatalf("expected lower-cased filter, got: %q", cfg.Filter)
	}
	if cfg.MaxWorkers != 3 {
		t.Fatalf("expected workers=3, got: %d", cfg.MaxWorkers)
	}
}

func TestParseFlagsRejectsInvalidWorkers(t *testing.T) {
	_, err := parseFlags([]string{"--from", "/src", "--to", "/dst", "--workers", "0"})
	if err == nil {
		t.Fatal("expected error for workers=0")
	}
	if !strings.Contains(err.Error(), "--workers") {
		t.Fatalf("expected workers validation error, got: %v", err)
	}
}

func TestParseFlagsParsesValidDates(t *testing.T) {
	cfg, err := parseFlags([]string{"--from", "/src", "--to", "/dst", "--start", "2024-03-03", "--end", "2024-03-31"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.Start.Year() != 2024 || cfg.Start.Month() != 3 || cfg.Start.Day() != 3 {
		t.Fatalf("expected start 2024-03-03, got: %v", cfg.Start)
	}
	if cfg.End.Year() != 2024 || cfg.End.Month() != 3 || cfg.End.Day() != 31 {
		t.Fatalf("expected end 2024-03-31, got: %v", cfg.End)
	}
	if cfg.End.Hour() != 23 || cfg.End.Minute() != 59 || cfg.End.Second() != 59 {
		t.Fatalf("expected end time 23:59:59, got: %v", cfg.End)
	}
}

func TestParseFlagsRejectsInvalidStartOrEnd(t *testing.T) {
	_, err := parseFlags([]string{"--from", "/src", "--to", "/dst", "--start", "invalid"})
	if err == nil || !strings.Contains(err.Error(), "invalid start date format") {
		t.Fatalf("expected start date validation error, got: %v", err)
	}

	_, err = parseFlags([]string{"--from", "/src", "--to", "/dst", "--end", "2024-99-99"})
	if err == nil || !strings.Contains(err.Error(), "invalid end date format") {
		t.Fatalf("expected end date validation error, got: %v", err)
	}
}

func TestRunImportAppliesFilterAndDateRange(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, "from")
	to := filepath.Join(root, "to")
	if err := os.MkdirAll(from, 0o755); err != nil {
		t.Fatalf("mkdir from failed: %v", err)
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatalf("mkdir to failed: %v", err)
	}

	inRange := filepath.Join(from, "in_range.jpg")
	outOfRange := filepath.Join(from, "out_range.jpg")
	filtered := filepath.Join(from, "ignored.txt")

	mustWriteFile(t, inRange, "jpg content")
	mustWriteFile(t, outOfRange, "older jpg")
	mustWriteFile(t, filtered, "text content")

	mustSetMtime(t, inRange, time.Date(2024, 3, 5, 10, 0, 0, 0, time.UTC))
	mustSetMtime(t, outOfRange, time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC))
	mustSetMtime(t, filtered, time.Date(2024, 3, 5, 10, 0, 0, 0, time.UTC))

	cfg := importConfig{
		From:       from,
		To:         to,
		Filter:     "jpg",
		Start:      time.Date(2024, 3, 3, 0, 0, 0, 0, time.UTC),
		End:        time.Date(2024, 3, 31, 23, 59, 59, 0, time.UTC),
		MaxWorkers: 2,
	}

	var out bytes.Buffer
	summary, err := runImport(cfg, &out, nil)
	if err != nil {
		t.Fatalf("runImport returned error: %v\noutput:\n%s", err, out.String())
	}

	if summary.processed != 2 {
		t.Fatalf("expected processed=2 (only jpg files), got: %d", summary.processed)
	}
	if summary.copied != 1 {
		t.Fatalf("expected copied=1, got: %d", summary.copied)
	}
	if summary.skipped != 1 {
		t.Fatalf("expected skipped=1, got: %d", summary.skipped)
	}
	if summary.failed != 0 {
		t.Fatalf("expected failed=0, got: %d", summary.failed)
	}

	copiedPath := filepath.Join(to, "2024-03-05-jpg", "in_range.jpg")
	if got := readFileString(t, copiedPath); got != "jpg content" {
		t.Fatalf("copied file content mismatch: %q", got)
	}

	notCopiedPath := filepath.Join(to, "2024-03-01-jpg", "out_range.jpg")
	if _, err := os.Stat(notCopiedPath); !os.IsNotExist(err) {
		t.Fatalf("expected out-of-range file to not be copied, stat err=%v", err)
	}

	ignoredPath := filepath.Join(to, "2024-03-05-txt", "ignored.txt")
	if _, err := os.Stat(ignoredPath); !os.IsNotExist(err) {
		t.Fatalf("expected filtered file to not be copied, stat err=%v", err)
	}
}

func TestRunImportNormalizesExtensionFolderName(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, "from")
	to := filepath.Join(root, "to")
	if err := os.MkdirAll(from, 0o755); err != nil {
		t.Fatalf("mkdir from failed: %v", err)
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatalf("mkdir to failed: %v", err)
	}

	src := filepath.Join(from, "IMG_0001.JPG")
	mustWriteFile(t, src, "upper extension")
	mustSetMtime(t, src, time.Date(2024, 7, 8, 9, 10, 11, 0, time.UTC))

	cfg := importConfig{
		From:       from,
		To:         to,
		Filter:     "jpg",
		Start:      time.Date(2024, 7, 8, 0, 0, 0, 0, time.UTC),
		End:        time.Date(2024, 7, 8, 23, 59, 59, 0, time.UTC),
		MaxWorkers: 1,
	}

	var out bytes.Buffer
	summary, err := runImport(cfg, &out, nil)
	if err != nil {
		t.Fatalf("runImport returned error: %v\noutput:\n%s", err, out.String())
	}
	if summary.copied != 1 {
		t.Fatalf("expected copied=1, got: %d", summary.copied)
	}

	dst := filepath.Join(to, "2024-07-08-jpg", "IMG_0001.JPG")
	if got := readFileString(t, dst); got != "upper extension" {
		t.Fatalf("copied file content mismatch: %q", got)
	}
}

func TestRunImportReturnsErrorForMissingSourceDir(t *testing.T) {
	root := t.TempDir()
	cfg := importConfig{
		From:       filepath.Join(root, "missing"),
		To:         filepath.Join(root, "to"),
		MaxWorkers: 1,
	}

	var out bytes.Buffer
	_, err := runImport(cfg, &out, nil)
	if err == nil {
		t.Fatal("expected error for missing source directory")
	}
}

func TestRunImportWritesProgressToDedicatedWriter(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, "from")
	to := filepath.Join(root, "to")
	if err := os.MkdirAll(from, 0o755); err != nil {
		t.Fatalf("mkdir from failed: %v", err)
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatalf("mkdir to failed: %v", err)
	}

	src := filepath.Join(from, "progress.jpg")
	mustWriteFile(t, src, "progress content")
	mustSetMtime(t, src, time.Date(2024, 7, 8, 9, 10, 11, 0, time.UTC))

	cfg := importConfig{
		From:       from,
		To:         to,
		Filter:     "jpg",
		Start:      time.Date(2024, 7, 8, 0, 0, 0, 0, time.UTC),
		End:        time.Date(2024, 7, 8, 23, 59, 59, 0, time.UTC),
		MaxWorkers: 1,
	}

	var out bytes.Buffer
	var progress bytes.Buffer
	summary, err := runImport(cfg, &out, &progress)
	if err != nil {
		t.Fatalf("runImport returned error: %v\noutput:\n%s", err, out.String())
	}
	if summary.copied != 1 {
		t.Fatalf("expected copied=1, got: %d", summary.copied)
	}
	if strings.Contains(out.String(), "Done checking") {
		t.Fatalf("expected progress output to stay out of main output, got: %q", out.String())
	}
	if !strings.Contains(progress.String(), "Done checking 1/1") {
		t.Fatalf("expected progress output in dedicated writer, got: %q", progress.String())
	}
	if !strings.Contains(progress.String(), "\033[2K") {
		t.Fatalf("expected ANSI clearing sequence in progress output, got: %q", progress.String())
	}
}

func TestRunImportDoesNotWriteProgressWhenDisabled(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, "from")
	to := filepath.Join(root, "to")
	if err := os.MkdirAll(from, 0o755); err != nil {
		t.Fatalf("mkdir from failed: %v", err)
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatalf("mkdir to failed: %v", err)
	}

	src := filepath.Join(from, "quiet.jpg")
	mustWriteFile(t, src, "quiet content")
	mustSetMtime(t, src, time.Date(2024, 7, 8, 9, 10, 11, 0, time.UTC))

	cfg := importConfig{
		From:       from,
		To:         to,
		Filter:     "jpg",
		Start:      time.Date(2024, 7, 8, 0, 0, 0, 0, time.UTC),
		End:        time.Date(2024, 7, 8, 23, 59, 59, 0, time.UTC),
		MaxWorkers: 1,
	}

	var out bytes.Buffer
	summary, err := runImport(cfg, &out, nil)
	if err != nil {
		t.Fatalf("runImport returned error: %v\noutput:\n%s", err, out.String())
	}
	if summary.copied != 1 {
		t.Fatalf("expected copied=1, got: %d", summary.copied)
	}
	if strings.Contains(out.String(), "Done checking") {
		t.Fatalf("expected no progress output in main output when progress is disabled, got: %q", out.String())
	}
}

func TestRunImportLeavesProgressWriterEmptyWhenNoFilesMatch(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, "from")
	to := filepath.Join(root, "to")
	if err := os.MkdirAll(from, 0o755); err != nil {
		t.Fatalf("mkdir from failed: %v", err)
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatalf("mkdir to failed: %v", err)
	}

	src := filepath.Join(from, "ignored.txt")
	mustWriteFile(t, src, "ignored content")
	mustSetMtime(t, src, time.Date(2024, 7, 8, 9, 10, 11, 0, time.UTC))

	cfg := importConfig{
		From:       from,
		To:         to,
		Filter:     "jpg",
		Start:      time.Date(2024, 7, 8, 0, 0, 0, 0, time.UTC),
		End:        time.Date(2024, 7, 8, 23, 59, 59, 0, time.UTC),
		MaxWorkers: 1,
	}

	var out bytes.Buffer
	var progress bytes.Buffer
	summary, err := runImport(cfg, &out, &progress)
	if err != nil {
		t.Fatalf("runImport returned error: %v\noutput:\n%s", err, out.String())
	}
	if summary.processed != 0 || summary.copied != 0 || summary.skipped != 0 || summary.failed != 0 {
		t.Fatalf("expected empty summary, got: %+v", summary)
	}
	if progress.Len() != 0 {
		t.Fatalf("expected no progress output when no files match, got: %q", progress.String())
	}
	if !strings.Contains(out.String(), "Done. processed=0 copied=0 skipped=0 failed=0") {
		t.Fatalf("expected final summary in main output, got: %q", out.String())
	}
}

func TestParseFlagsSetsUseModTime(t *testing.T) {
	cfg, err := parseFlags([]string{"--from", "/src", "--to", "/dst", "--fast"})
	if err != nil {
		t.Fatalf("parseFlags returned error: %v", err)
	}
	if !cfg.UseModTime {
		t.Fatalf("expected UseModTime to be true when --fast is provided")
	}
}

func TestRunImportBypassesExifWithFastFlag(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, "from")
	to := filepath.Join(root, "to")
	if err := os.MkdirAll(from, 0o755); err != nil {
		t.Fatalf("mkdir from failed: %v", err)
	}

	src := filepath.Join(from, "fake_image.jpg")
	mustWriteFile(t, src, "this is not a real jpeg")
	mtime := time.Date(2024, 7, 8, 9, 10, 11, 0, time.UTC)
	mustSetMtime(t, src, mtime)

	cfg := importConfig{
		From:       from,
		To:         to,
		Start:      time.Time{}, // 0
		End:        time.Date(2025, 1, 1, 23, 59, 59, 0, time.UTC), // 20250101
		MaxWorkers: 1,
	}

	// 1. Run without --fast (Should attempt EXIF and log the fallback)
	var outSlow bytes.Buffer
	cfg.UseModTime = false
	_, err := runImport(cfg, &outSlow, nil)
	if err != nil {
		t.Fatalf("runImport returned error: %v", err)
	}
	if !strings.Contains(outSlow.String(), "no EXIF data found, using ModTime") {
		t.Fatalf("expected standard run to complain about missing EXIF data, but got: %s", outSlow.String())
	}

	// Clean the target folder for the next test
	os.RemoveAll(to)
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatalf("mkdir to failed: %v", err)
	}

	// 2. Run with --fast (Should silently bypass EXIF and immediately use ModTime)
	var outFast bytes.Buffer
	cfg.UseModTime = true
	_, err = runImport(cfg, &outFast, nil)
	if err != nil {
		t.Fatalf("runImport returned error: %v", err)
	}
	if strings.Contains(outFast.String(), "no EXIF data found, using ModTime") {
		t.Fatalf("expected --fast run to bypass EXIF parsing completely, but got EXIF logs: %s", outFast.String())
	}
}
