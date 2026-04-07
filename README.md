# File Importer

A high-performance concurrency-based tool designed to organize files (primarily photography/images) by moving them chronologically from a source to a destination directory. 

Built in Go, `file-importer` sorts and segregates your files into neatly categorized `YYYY-MM-DD-<extension>` folders. By default, it intelligently attempts to parse native EXIF creation times (supporting complex formats like TIFF and CR3). In case EXIF data is missing, it seamlessly falls back to accurate file modification times while completely replicating original timestamps in the destination.

## Features

- **EXIF Extraction:** Natively extracts `DateTimeOriginal` from image metadata instead of incorrectly relying on vague filesystem changes.
- **Multithreading:** Leverages highly concurrent worker routines to handle vast media libraries dramatically faster than standalone scripts.
- **Precision Filtering:** Filter processing natively by both date bounds (e.g., specific days/months) and explicit file extensions.
- **Zero Loss:** Original media modification timestamps (`mtime`) and access configurations are completely restored on the newly created directories.

## Installation

Assuming you have Go installed on your system:

```bash
git clone https://github.com/renner/file-importer.git
cd file-importer
go build -o file-importer
```

Alternatively, you could run it directly utilizing:
```bash
go run . --from /path/to/source --to /path/to/target
```

## Usage

```bash
./file-importer --from <source_path> --to <destination_path> [options]
```

### Options

| Flag | Description | Default |
| --- | --- | --- |
| `--from` | **(Required)** Path to the source directory containing the raw files. | |
| `--to` | **(Required)** Path to the destination directory. Subdirectories will be created automatically. | |
| `--start` | Start date bound (inclusive) using the `YYYY-MM-DD` format. | |
| `--end` | End date bound (inclusive) using the `YYYY-MM-DD` format. | |
| `--filter`| Only process files with a specific extension (e.g., `jpg`, `cr3`). Matches are case-insensitive. | |
| `--workers` | Maximum number of concurrent workers assigned to IO/parsing routines. | `10` |
| `--fast` | Bypasses all EXIF metadata parsing. Directly utilizes filesystem modification times for massive speed boosts. | `false` |

### Example

Import exclusively `.jpg` photos taken during a two-month summer timeframe. Limit concurrency to 4 workers.

```bash
./file-importer \
  --from /media/sd_card/DCIM \
  --to /home/user/Pictures/Imports \
  --filter jpg \
  --start 2024-06-01 \
  --end 2024-07-31 \
  --workers 4
```

*This securely reads `/media/sd_card/DCIM`, finds all JPEGs captured between June and July EXIF timestamps, and safely segregates them sequentially into formats such as `/home/user/Pictures/Imports/2024-06-03-jpg/` without losing timeline integrity.*
