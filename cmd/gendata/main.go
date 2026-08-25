// Command gendata writes the benchmark datasets.
//
// The files it produces are read by every library, so they are generated once
// and never per library and never per run. Given the same flags it writes the
// same bytes on every machine, which is what lets a result from March be
// compared against a result from September.
//
//	gendata -suite groupby -size 0.5GB -out data/
//	gendata -suite join    -size 5GB   -out data/
//	gendata -suite all     -size 0.5GB -out data/
//
// Sizes are the db-benchmark sizes: 0.5GB is ten million rows, 5GB is a
// hundred million, 50GB is a billion. Pass -rows for anything else.
//
// The output is CSV, because that is the input format db-benchmark uses and
// the one every library can read without an extra dependency. Parquet and
// NDJSON copies are made by a converter that runs afterwards, since writing
// Parquet from Go would mean a dependency and this module has none.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// The buffer is large because the write pattern is a single sequential stream
// of small records, which is the case where the syscall count dominates.
const writeBuffer = 4 << 20

func main() {
	log := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}

	var (
		suite = flag.String("suite", "all", "which dataset: groupby, join or all")
		size  = flag.String("size", "0.5GB", "dataset size: 0.5GB, 5GB or 50GB")
		rows  = flag.Int("rows", 0, "row count, overriding -size")
		k     = flag.Int("k", 100, "key cardinality for the groupby dataset")
		nas   = flag.Int("nas", 0, "percentage of missing values, 0 to 100")
		seed  = flag.Uint64("seed", 1, "random seed")
		out   = flag.String("out", "data", "output directory")
		force = flag.Bool("force", false, "regenerate files that already exist")
	)
	flag.Parse()

	n := *rows
	if n == 0 {
		var err error
		if n, err = rowsForSize(*size); err != nil {
			log("gendata: %v", err)
			os.Exit(2)
		}
	}
	if *nas < 0 || *nas > 100 {
		log("gendata: -nas must be between 0 and 100")
		os.Exit(2)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log("gendata: %v", err)
		os.Exit(1)
	}

	type file struct {
		name  string
		write func(*bufio.Writer) error
	}
	var files []file

	if *suite == "groupby" || *suite == "all" {
		g := groupBy{n: n, k: *k, nas: *nas, seed: *seed}
		files = append(files, file{
			name:  fmt.Sprintf("G1_%s_%s_%d_0.csv", exp(n), exp(*k), *nas),
			write: g.write,
		})
	}
	if *suite == "join" || *suite == "all" {
		j := join{n: n, seed: *seed}
		small, medium, big := j.sizes()
		files = append(files,
			file{
				name:  fmt.Sprintf("J1_%s_NA_%d_0.csv", exp(n), *nas),
				write: j.writeLeft,
			},
			file{
				name:  fmt.Sprintf("J1_%s_%s_%d_0.csv", exp(n), exp(small), *nas),
				write: func(w *bufio.Writer) error { return j.writeRight(w, small, 1) },
			},
			file{
				name:  fmt.Sprintf("J1_%s_%s_%d_0.csv", exp(n), exp(medium), *nas),
				write: func(w *bufio.Writer) error { return j.writeRight(w, medium, 2) },
			},
			file{
				name:  fmt.Sprintf("J1_%s_%s_%d_0.csv", exp(n), exp(big), *nas),
				write: func(w *bufio.Writer) error { return j.writeRight(w, big, 3) },
			},
		)
	}
	if len(files) == 0 {
		log("gendata: unknown suite %q, want groupby, join or all", *suite)
		os.Exit(2)
	}

	for _, f := range files {
		path := filepath.Join(*out, f.name)
		if !*force {
			if info, err := os.Stat(path); err == nil {
				log("gendata: %s already exists, %s, skipping", f.name, humanBytes(info.Size()))
				continue
			}
		}
		start := time.Now()
		size, err := writeFile(path, f.write)
		if err != nil {
			log("gendata: %v", err)
			os.Exit(1)
		}
		log("gendata: %s, %s, %s", f.name, humanBytes(size), time.Since(start).Round(time.Millisecond))
	}
}

// writeFile writes through a temporary file and renames it into place, so that
// an interrupted run leaves no half generated dataset behind. Finding out that
// a file was truncated after a six hour benchmark is a bad way to find out.
func writeFile(path string, write func(*bufio.Writer) error) (int64, error) {
	tmp := path + ".partial"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp)

	w := bufio.NewWriterSize(f, writeBuffer)
	if err := write(w); err != nil {
		f.Close()
		return 0, fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return 0, fmt.Errorf("flush %s: %w", filepath.Base(path), err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return 0, err
	}
	if err := f.Close(); err != nil {
		return 0, fmt.Errorf("close %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// rowsForSize maps a db-benchmark size label to a row count. The labels are
// approximate descriptions of the CSV on disk and the row counts are the real
// parameter, which is why both appear in the file names.
func rowsForSize(size string) (int, error) {
	switch size {
	case "0.5GB":
		return 10_000_000, nil
	case "5GB":
		return 100_000_000, nil
	case "50GB":
		return 1_000_000_000, nil
	}
	return 0, fmt.Errorf("unknown size %q, want 0.5GB, 5GB or 50GB, or pass -rows", size)
}

// exp formats a round number the way db-benchmark names its files, so 10000000
// becomes 1e7. Numbers that are not a power of ten are written out in full,
// which only happens when someone passes -rows by hand.
func exp(n int) string {
	if n <= 0 {
		return fmt.Sprint(n)
	}
	digits := 0
	for v := n; v%10 == 0; v /= 10 {
		digits++
	}
	if lead := n / pow10(digits); lead != 1 {
		return fmt.Sprint(n)
	}
	return fmt.Sprintf("1e%d", digits)
}

func pow10(n int) int {
	p := 1
	for range n {
		p *= 10
	}
	return p
}

func humanBytes(n int64) string {
	const unit = 1 << 10
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, e := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		e++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[e])
}
