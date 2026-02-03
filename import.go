package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dsoprea/go-exif/v3"
	exifcommon "github.com/dsoprea/go-exif/v3/common"
)

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
	err = out.Sync()
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
			return valueRaw.(string), nil
		}
	}
	return "", fmt.Errorf("tag not found")
}

func main() {
	var from, to, filter string
	flag.StringVar(&from, "from", "", "Source path")
	flag.StringVar(&to, "to", "", "Destination path")
	flag.StringVar(&filter, "filter", "", "Optional file type filter")

	var start, end uint
	flag.UintVar(&start, "start", uint(0), "Start date")
	flag.UintVar(&end, "end", ^uint(0), "End date")
	flag.Parse()

	if from == "" || to == "" {
		fmt.Fprintf(os.Stderr, "Error: Need source and target directory (use '--from' and '--to')\n\n")
		flag.Usage()
		os.Exit(-1)
	}

	// Read the source directory
	fmt.Printf("Importing files from %s -> %s\n", from, to)
	files, err := os.ReadDir(from)
	if err != nil {
		log.Fatal(err)
	}

	// Create a channel to distribute the work
	fileChan := make(chan os.FileInfo, len(files))
	var wg sync.WaitGroup

	// Limit the number of concurrent goroutines
	const maxGoroutines = 10
	guard := make(chan struct{}, maxGoroutines)

	for _, f := range files {
		// Skip directories
		if f.IsDir() {
			continue
		}

		// Filter for file extension
		ext := strings.Trim(filepath.Ext(f.Name()), ".")
		if filter != "" && ext != filter {
			continue
		}

		info, err := f.Info()
		if err != nil {
			fmt.Printf("Error getting info for %s: %v\n", f.Name(), err)
			continue
		}
		fileChan <- info
	}
	close(fileChan)

	for f := range fileChan {
		ext := strings.Trim(filepath.Ext(f.Name()), ".")
		wg.Add(1)
		guard <- struct{}{} // Block if guard channel is full

		go func(f os.FileInfo, ext string) {
			defer wg.Done()
			defer func() { <-guard }() // Release guard slot

			// Filter for EXIF DateTime if it exists, otherwise ModTime
			file, err := os.Open(filepath.Join(from, f.Name()))
			if err != nil {
				fmt.Printf("%s: Error opening file: %v\n", f.Name(), err)
				return
			}
			defer file.Close()

			var timestampValue time.Time
			rawExif, err := exif.SearchAndExtractExifWithReader(file)
			if err != nil {
				fmt.Printf("%s: No EXIF data found (%s), using ModTime\n", f.Name(), err)
				timestampValue = f.ModTime().In(time.UTC)
			} else {
				im, err := exifcommon.NewIfdMappingWithStandard()
				if err != nil {
					fmt.Printf("%s: Error creating IFD mapping: %v, using ModTime\n", f.Name(), err)
					timestampValue = f.ModTime().In(time.UTC)
				} else {
					ti := exif.NewTagIndex()
					_, index, err := exif.Collect(im, ti, rawExif)
					if err != nil {
						fmt.Printf("%s: Error collecting EXIF: %v, using ModTime\n", f.Name(), err)
						timestampValue = f.ModTime().In(time.UTC)
					} else {
						// Search for DateTimeOriginal tag
						dateTimeString, err := findTagInAllIfds(&index, "DateTimeOriginal")
						if err != nil {
							fmt.Printf("%s: DateTimeOriginal not found (%s), using ModTime\n", f.Name(), err)
							timestampValue = f.ModTime().In(time.UTC)
						} else {
							fmt.Printf("%s: DateTimeOriginal = %s\n", f.Name(), dateTimeString)

							layout := "2006:01:02 15:04:05"
							timestampValue, err = time.Parse(layout, dateTimeString)
							if err != nil {
								fmt.Printf("%s: Error parsing DateTimeOriginal: %v, using ModTime\n", f.Name(), err)
								timestampValue = f.ModTime().In(time.UTC)
							}
						}
					}
				}

				// Determine corresponding timezone from EXIF or use local timezone
				// offsetString, err := findTagInAllIfds(&index, "OffsetTime")
				// if err != nil {
				// 	fmt.Printf("OffsetTime - tag not found (%s), using Local\n", err)
				// 	timestampValue = timestampValue.In(time.Local)
				// } else {
				// 	fmt.Printf("OffsetTime = %s\n", offsetString)

				// 	offsetSeconds, err := offsetToSeconds(offsetString)
				// 	if err != nil {
				// 		fmt.Printf("%s: Error parsing offset: %v, using local\n", f.Name(), err)
				// 		timestampValue = timestampValue.In(time.Local)
				// 	} else {
				// 		location := time.FixedZone("FixedZone", offsetSeconds)
				// 		timestampValue = timestampValue.In(location)
				// 	}
				// }
			}

			i, _ := strconv.Atoi(timestampValue.Format("20060102"))
			if uint(i) < start || uint(i) > end {
				return
			}

			// Create folder if needed
			timestamp := timestampValue.Format("2006-01-02")
			folder := filepath.Join(to, timestamp+"-"+strings.ToLower(ext))
			if err := os.MkdirAll(folder, 0755); err != nil {
				fmt.Printf("%s: Error creating folder %s: %v\n", f.Name(), folder, err)
				return
			}

			// Copy the file
			fromFile := filepath.Join(from, f.Name())
			toFile := filepath.Join(folder, f.Name())
			fmt.Printf("Copying %s -> %s (%s)\n", fromFile, toFile, timestampValue)
			err = copyFile(fromFile, toFile)
			if err != nil {
				fmt.Printf("Copy file failed: %q\n", err)
			}
		}(f, ext)
	}

	wg.Wait()
}
