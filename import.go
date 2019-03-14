package main

import (
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

// Copy a file from src to dst.
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
	fmt.Printf("Importing files from %s to %s\n", os.Args[1], os.Args[2])

	// Parse minimum timestamp from arguments
	minTimestamp, err := strconv.ParseInt(os.Args[3], 10, 64)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(minTimestamp)

	// Read the directory
	files, err := ioutil.ReadDir(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}

	for _, f := range files {
		// fmt.Println(f.Name())
		timestamp := f.ModTime().Format("2006-01-02")
		timestampValue := f.ModTime().Format("20060102")
		i, _ := strconv.ParseInt(timestampValue, 10, 64)
		fmt.Printf("Timestamp: %d\n", i)
		if i < minTimestamp {
			fmt.Println("Skipping...")
			continue
		}

		// Create folder if needed
		ext := strings.ToLower(strings.Trim(filepath.Ext(f.Name()), "."))
		folder := filepath.Join(os.Args[2], timestamp+"-"+ext)
		if value, err := pathExists(folder); value == false && err == nil {
			fmt.Printf("Creating folder: %s\n", folder)
			os.Mkdir(folder, 0755)
		} else {
			fmt.Printf("Folder exists: %s\n", folder)
		}

		// Copy the file
		src := filepath.Join(os.Args[1], f.Name())
		dest := filepath.Join(folder, f.Name())
		fmt.Printf("Copying %s to %s\n", src, dest)
		err := copyFile(src, dest)
		if err != nil {
			fmt.Printf("Copy file failed %q\n", err)
		} else {
			fmt.Printf("Copy file succeeded\n")
		}
	}
}
