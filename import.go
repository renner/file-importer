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
)

// Copy a file from src to dst
func copyFile(src, dst string) (err error) {
	sfi, err := os.Stat(src)
	if err != nil {
		return
	}
	if !sfi.Mode().IsRegular() {
		// cannot copy non-regular files (e.g., directories,
		// symlinks, devices, etc.)
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
// destination file exists, all it's contents will be replaced by the contents of the source file.
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

func main() {
	var from, to, filter string
	flag.StringVar(&from, "from", "", "Source path")
	flag.StringVar(&to, "to", "", "Destination path")
	flag.StringVar(&filter, "filter", "", "Optional file type filter")

	var start, end int
	flag.IntVar(&start, "start", 0, "Start date")
	flag.IntVar(&end, "end", 0, "End date")
	flag.Parse()

	// Read the source directory
	fmt.Printf("Importing files from %s to %s\n", from, to)
	files, err := ioutil.ReadDir(from)
	if err != nil {
		log.Fatal(err)
	}

	for _, f := range files {
		fmt.Printf("\nNew file --> %s\n", f.Name())
		timestamp := f.ModTime().Format("2006-01-02")
		timestampValue := f.ModTime().Format("20060102")
		i, _ := strconv.Atoi(timestampValue)
		fmt.Printf("Timestamp: %d\n", i)
		if i < start || i > end {
			fmt.Println("Skipping (timestamp) ...")
			continue
		}

		// Create folder if needed
		ext := strings.Trim(filepath.Ext(f.Name()), ".")
		if filter != "" && ext != filter {
			fmt.Printf("Skipping (filter): %s vs. %s\n", ext, filter)
			continue
		}
		folder := filepath.Join(to, timestamp+"-"+strings.ToLower(ext))
		if value, err := pathExists(folder); value == false && err == nil {
			fmt.Printf("Creating folder: %s\n", folder)
			os.Mkdir(folder, 0755)
		} else {
			fmt.Printf("Folder exists: %s\n", folder)
		}

		// Copy the file
		fromFile := filepath.Join(from, f.Name())
		toFile := filepath.Join(folder, f.Name())
		fmt.Printf("Copying %s to %s\n", fromFile, toFile)
		err := copyFile(fromFile, toFile)
		if err != nil {
			fmt.Printf("Copy file failed %q\n", err)
		} else {
			fmt.Printf("Copy file succeeded\n")
		}
	}
}
