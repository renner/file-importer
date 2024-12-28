package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
		log.Fatal(err)
	}
	err = out.Sync()
	return
}

// Return whether the given file or directory exists
func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return true, err
}

// Convert an offset string to seconds (untested!)
func offsetToSeconds(offset string) (int, error) {
	if len(offset) != 5 {
		return 0, fmt.Errorf("Invalid offset format")
	}

	sign := 1
	if offset[0] == '-' {
		sign = -1
	} else if offset[0] != '+' {
		return 0, fmt.Errorf("Invalid offset format")
	}

	hours, err := strconv.Atoi(offset[1:3])
	if err != nil {
		return 0, err
	}

	minutes, err := strconv.Atoi(offset[3:5])
	if err != nil {
		return 0, err
	}

	return sign * (hours*3600 + minutes*60), nil
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
	files, err := ioutil.ReadDir(from)
	if err != nil {
		log.Fatal(err)
	}

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

		// Filter for EXIF DateTime if it exists, otherwise ModTime
		file, err := os.Open(filepath.Join(from, f.Name()))
		if err != nil {
			log.Fatal(err)
		}

		var timestampValue time.Time
		rawExif, err := exif.SearchAndExtractExifWithReader(file)
		if err != nil {
			fmt.Printf("No EXIF data found (%s), using ModTime\n", err)
			timestampValue = f.ModTime()
		} else {
			im, err := exifcommon.NewIfdMappingWithStandard()
			if err != nil {
				log.Fatal(err)
			}
			ti := exif.NewTagIndex()
			_, index, err := exif.Collect(im, ti, rawExif)
			if err != nil {
				log.Fatal(err)
			}

			results, err := index.RootIfd.FindTagWithName("DateTime")
			if err != nil {
				fmt.Printf("Date Time - tag not found (%s), using ModTime\n", err)
				timestampValue = f.ModTime()
			} else {
				ite := results[0]

				valueRaw, err := ite.Value()
				if err != nil {
					log.Fatal(err)
				}

				dateTimeString := valueRaw.(string)
				fmt.Printf("DateTime = %s\n", dateTimeString)

				layout := "2006:01:02 15:04:05"
				timestampValue, err = time.Parse(layout, dateTimeString)
				if err != nil {
					log.Fatal(err)
				}
			}

			// Try to determine timezone from EXIF, otherwise use local timezone
			results, err = index.RootIfd.FindTagWithName("OffsetTime")
			if err != nil {
				fmt.Printf("Offset Time - tag not found (%s), using Local\n", err)
				timestampValue = timestampValue.In(time.Local)
			} else {
				ite := results[0]

				valueRaw, err := ite.Value()
				if err != nil {
					log.Fatal(err)
				}

				offsetString := valueRaw.(string)
				fmt.Printf("OffsetTime = %s\n", offsetString)

				offsetSeconds, err := offsetToSeconds(offsetString)
				if err != nil {
					log.Fatal(err)
				}
				location := time.FixedZone("FixedZone", offsetSeconds)
				timestampValue = timestampValue.In(location)
			}
		}

		i, _ := strconv.Atoi(timestampValue.Format("20060102"))
		if uint(i) < start || uint(i) > end {
			continue
		}

		// Create folder if needed
		timestamp := timestampValue.Format("2006-01-02")
		folder := filepath.Join(to, timestamp+"-"+strings.ToLower(ext))
		if value, err := pathExists(folder); value == false && err == nil {
			fmt.Printf("Creating folder: %s\n", folder)
			os.Mkdir(folder, 0755)
		}

		// Copy the file
		fromFile := filepath.Join(from, f.Name())
		toFile := filepath.Join(folder, f.Name())
		fmt.Printf("Copying %s -> %s (%s)\n", fromFile, toFile, timestampValue)
		err = copyFile(fromFile, toFile)
		if err != nil {
			fmt.Printf("Copy file failed: %q\n", err)
		}
	}
}
