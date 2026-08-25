// Command micro turns the output of go test -bench into result records.
//
// It reads benchmark output on standard input, or from a file, and writes one
// JSON object per benchmark to a file under results/.
//
//	ssh server3 'cd kuma && go test -run "^$" -bench . -benchmem -count 5 ./...' | \
//	    micro -host server3 -commit "$(git rev-parse HEAD)" -out results/
//
// Parsing text rather than running the benchmarks itself is deliberate. The
// benchmarks have to run on the machine being measured, that machine only
// needs a Go toolchain and a kuma checkout, and this repository with its Python
// and its datasets does not have to be installed anywhere near it. It also
// means a run that somebody did by hand, and pasted, is still a record.
//
// The machine flags are not optional decoration. Results from different
// machines are not comparable, and a record that cannot say which box it came
// from is one that will be averaged with something it should not be.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamnd/kuma-bench/bench"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "micro: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		in      = flag.String("in", "-", "benchmark output to read, or - for standard input")
		out     = flag.String("out", "results", "directory to write the results file to")
		host    = flag.String("host", "", "name of the machine, such as server3")
		runner  = flag.String("runner", "bare-metal", "what kind of machine it is, such as bare-metal or github-actions")
		cpu     = flag.String("cpu", "", "CPU model, when the test binary did not report one")
		cpus    = flag.Int("cpus", 0, "core count, when it should not be taken from the benchmark suffix")
		memory  = flag.Int("memory-gb", 0, "memory size in gigabytes")
		commit  = flag.String("commit", "", "the kuma commit that was benchmarked")
		load    = flag.Float64("load", 0, "the one minute load average on the machine when the run started")
		simd    = flag.Bool("simd", false, "whether GOEXPERIMENT=simd was set")
		goVer   = flag.String("go", "", "Go version, such as go1.27.0")
		dry     = flag.Bool("n", false, "print the records to standard output instead of writing a file")
		verbose = flag.Bool("v", false, "print what was written")
	)
	flag.Parse()

	if *host == "" {
		return errors.New("-host is required, since a result that cannot name its machine cannot be compared with anything")
	}

	src := os.Stdin
	if *in != "-" {
		f, err := os.Open(*in)
		if err != nil {
			return fmt.Errorf("open input: %w", err)
		}
		defer f.Close()
		src = f
	}

	records, err := bench.ParseBench(src)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return errors.New("no benchmark lines found, so either the run failed or the wrong output was piped in")
	}

	now := time.Now().UTC()
	for i := range records {
		m := &records[i]
		m.Timestamp = now
		m.Commit = *commit
		m.Machine.Host = *host
		m.Machine.Runner = *runner
		m.Machine.MemoryGB = *memory
		m.Machine.Load1 = *load
		m.Machine.SIMD = simd
		if *cpu != "" {
			m.Machine.CPUModel = *cpu
		}
		if *goVer != "" {
			m.Machine.GoVersion = *goVer
		}
		// The suffix Go prints after a benchmark name is GOMAXPROCS, which is
		// the core count unless somebody set it by hand.
		if m.Machine.CPUs == 0 {
			m.Machine.CPUs = *cpus
		}
		if m.Machine.CPUs == 0 {
			m.Machine.CPUs = m.Procs
		}
	}

	if *dry {
		return bench.EncodeMicro(os.Stdout, records)
	}

	first := records[0].Machine
	name := fmt.Sprintf("micro-%s-%s-%s-%s.jsonl",
		*host, nonEmpty(first.OS, "unknown"), nonEmpty(first.Arch, "unknown"),
		now.Format("20060102-150405"))
	path := filepath.Join(*out, name)

	if err := os.MkdirAll(*out, 0o755); err != nil {
		return fmt.Errorf("create results directory: %w", err)
	}
	if err := bench.WriteMicro(path, records); err != nil {
		return err
	}

	if *verbose {
		fmt.Printf("%d benchmarks from %s written to %s\n", len(records), *host, path)
		summarize(os.Stdout, records)
	}
	return nil
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// summarize prints the fastest repetition of each benchmark, which is the one
// least polluted by whatever else the machine was doing.
func summarize(w io.Writer, records []bench.Micro) {
	best := map[string]bench.Micro{}
	order := make([]string, 0, len(records))
	for _, m := range records {
		key := m.Package + "." + m.Name
		prev, seen := best[key]
		if !seen {
			order = append(order, key)
		}
		if !seen || m.NsPerOp < prev.NsPerOp {
			best[key] = m
		}
	}
	for _, key := range order {
		m := best[key]
		fmt.Fprintf(w, "  %-40s %10.2f ns/op %8d B/op %6d allocs/op\n",
			shortName(key), m.NsPerOp, m.BytesPerOp, m.AllocsPerOp)
	}
}

// shortName drops the module path from a package, since every line in a run
// starts with the same one.
func shortName(key string) string {
	if i := strings.LastIndex(key, "/"); i >= 0 {
		return key[i+1:]
	}
	return key
}
