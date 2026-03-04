package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dsoprea/go-exif/v3"
	exifcommon "github.com/dsoprea/go-exif/v3/common"
	"github.com/evanoberholster/imagemeta"
)

type importConfig struct {
	From       string
	To         string
	Filter     string
	Start      uint
	End        uint
	MaxWorkers int
}

type importSummary struct {
	processed int
	copied    int
	skipped   int
	failed    int
}

// Copy a file from src to dst
func copyFile(src, dst string) (err error) {
	sfi, err := os.Stat(src)
	if err != nil {
		return
	}
	if !sfi.Mode().IsRegular() {
		// Cannot copy non-regular files (directories, symlinks, devices, etc.)
		return fmt.Errorf("Non-regular source file %s (%q)", sfi.Name(), sfi.Mode().String())
	}
	dfi, err := os.Stat(dst)
	if err != nil {
		if !os.IsNotExist(err) {
			return
		}
	} else {
		if !(dfi.Mode().IsRegular()) {
			return fmt.Errorf("Non-regular destination file %s (%q)", dfi.Name(), dfi.Mode().String())
		}
		if os.SameFile(sfi, dfi) {
			return
		}
	}
	err = copyFileContents(src, dst, sfi.ModTime())
	return
}

// Copy the contents of the file named src to the file named by dst setting the given mtime. If the
// destination file exists, all of its contents will be replaced by the contents of the source file.
func copyFileContents(src, dst string, mtime time.Time) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return
	}

	// Update the timestamps
	if err := os.Chtimes(dst, time.Now(), mtime); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return
	}
	return
}

// Find a tag in all IFDs and return the value as a string
func findTagInAllIfds(index *exif.IfdIndex, tagName string) (string, error) {
	ifds := index.Ifds
	for _, ifd := range ifds {
		results, err := ifd.FindTagWithName(tagName)
		if err == nil && len(results) > 0 {
			valueRaw, err := results[0].Value()
			if err != nil {
				return "", err
			}
			value, ok := valueRaw.(string)
			if !ok {
				return "", fmt.Errorf("tag %s is not a string", tagName)
			}
			return value, nil
		}
	}
	return "", fmt.Errorf("tag not found")
}

func parseFlags(args []string) (importConfig, error) {
	var cfg importConfig
	fs := flag.NewFlagSet("file-importer", flag.ContinueOnError)
	fs.StringVar(&cfg.From, "from", "", "Source path")
	fs.StringVar(&cfg.To, "to", "", "Destination path")
	fs.StringVar(&cfg.Filter, "filter", "", "Optional file type filter")
	fs.UintVar(&cfg.Start, "start", uint(0), "Start date")
	fs.UintVar(&cfg.End, "end", ^uint(0), "End date")
	fs.IntVar(&cfg.MaxWorkers, "workers", 10, "Maximum number of concurrent workers")
	if err := fs.Parse(args); err != nil {
		return importConfig{}, err
	}
	if cfg.From == "" || cfg.To == "" {
		return importConfig{}, fmt.Errorf("need source and target directory (use '--from' and '--to')")
	}
	if cfg.MaxWorkers < 1 {
		return importConfig{}, fmt.Errorf("--workers must be >= 1")
	}
	cfg.Filter = strings.ToLower(cfg.Filter)
	return cfg, nil
}

func resolveTimestamp(path string, fi os.FileInfo, logf func(string, ...any)) time.Time {
	file, err := os.Open(path)
	if err != nil {
		logf("%s: error opening file: %v", fi.Name(), err)
		return fi.ModTime()
	}
	defer file.Close()

	var timestampValue time.Time
	var dateTimeString, offsetString string
	var dtErr, offErr error

	// 1. Try standard EXIF extraction (works for JPEG, TIFF, CR2, etc.)
	rawExif, err := exif.SearchAndExtractExifWithReader(file)
	if err == nil {
		im, err := exifcommon.NewIfdMappingWithStandard()
		if err == nil {
			ti := exif.NewTagIndex()
			_, index, err := exif.Collect(im, ti, rawExif)
			if err == nil {
				dateTimeString, dtErr = findTagInAllIfds(&index, "DateTimeOriginal")
				offsetString, offErr = findTagInAllIfds(&index, "OffsetTimeOriginal")
				if offErr != nil {
					offsetString, _ = findTagInAllIfds(&index, "OffsetTime")
				}
			}
		}
	}

	// 2. Fallback for CR3 and other formats using imagemeta
	if dtErr != nil || dateTimeString == "" {
		if _, err := file.Seek(0, 0); err == nil {
			md, err := imagemeta.DecodeCR3(file)
			if err == nil {
				timestampValue = md.DateTimeOriginal()
			}
		}
	}

	// 3. Process the extracted strings with timezone logic
	if timestampValue.IsZero() && dateTimeString != "" {
		layout := "2006:01:02 15:04:05"
		if offsetString != "" {
			// Attempt to parse with timezone offset
			timestampValue, err = time.Parse(layout+"-07:00", dateTimeString+offsetString)
			if err != nil {
				logf("%s: error parsing DateTimeOriginal with offset: %v", fi.Name(), err)
			}
		}

		// Fallback: parse as local time if no offset or if offset parsing failed
		if timestampValue.IsZero() {
			timestampValue, err = time.ParseInLocation(layout, dateTimeString, time.Local)
			if err != nil {
				logf("%s: error parsing DateTimeOriginal: %v", fi.Name(), err)
			}
		}
	}

	// 4. Final fallback to ModTime
	if timestampValue.IsZero() {
		if dtErr != nil || dateTimeString == "" {
			logf("%s: no EXIF data found, using ModTime", fi.Name())
		} else {
			logf("%s: failed to parse EXIF, using ModTime", fi.Name())
		}
		timestampValue = fi.ModTime()
	}
	return timestampValue
}

func processFile(cfg importConfig, fi os.FileInfo, timestamp time.Time, logf func(string, ...any)) error {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(fi.Name())), ".")
	timestampFolder := timestamp.Format("2006-01-02")
	folder := filepath.Join(cfg.To, timestampFolder+"-"+ext)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return fmt.Errorf("%s: create folder %s failed: %w", fi.Name(), folder, err)
	}

	fromFile := filepath.Join(cfg.From, fi.Name())
	toFile := filepath.Join(folder, fi.Name())
	logf("Copying %s -> %s (%s)", fromFile, toFile, timestamp)
	if err := copyFile(fromFile, toFile); err != nil {
		return fmt.Errorf("%s: copy failed: %w", fi.Name(), err)
	}
	return nil
}

func runImport(cfg importConfig, out io.Writer) (importSummary, error) {
	files, err := os.ReadDir(cfg.From)
	if err != nil {
		return importSummary{}, err
	}

	fmt.Fprintf(out, "Importing files from %s -> %s\n", cfg.From, cfg.To)

	var (
		mu      sync.Mutex
		summary importSummary
	)
	logf := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(out, format+"\n", args...)
	}

	jobs := make(chan os.FileInfo)
	var wg sync.WaitGroup
	for range cfg.MaxWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for fi := range jobs {
				mu.Lock()
				summary.processed++
				mu.Unlock()

				timestamp := resolveTimestamp(filepath.Join(cfg.From, fi.Name()), fi, logf)
				i, _ := strconv.Atoi(timestamp.Format("20060102"))
				if uint(i) < cfg.Start || uint(i) > cfg.End {
					mu.Lock()
					summary.skipped++
					mu.Unlock()
					continue
				}

				err := processFile(cfg, fi, timestamp, logf)
				if err != nil {
					logf("%v", err)
					mu.Lock()
					summary.failed++
					mu.Unlock()
					continue
				}
				mu.Lock()
				summary.copied++
				mu.Unlock()
			}
		}()
	}

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(f.Name())), ".")
		if cfg.Filter != "" && ext != cfg.Filter {
			continue
		}
		info, err := f.Info()
		if err != nil {
			logf("Error getting info for %s: %v", f.Name(), err)
			mu.Lock()
			summary.failed++
			mu.Unlock()
			continue
		}
		jobs <- info
	}
	close(jobs)
	wg.Wait()

	fmt.Fprintf(
		out,
		"Done. processed=%d copied=%d skipped=%d failed=%d\n",
		summary.processed,
		summary.copied,
		summary.skipped,
		summary.failed,
	)

	if summary.failed > 0 {
		return summary, fmt.Errorf("import completed with %d failures", summary.failed)
	}
	return summary, nil
}

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(2)
	}
	if _, err := runImport(cfg, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
