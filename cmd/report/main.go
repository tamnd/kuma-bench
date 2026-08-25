// Command report reads the committed results and prints them.
//
//	report                                  the latest run, as a table
//	report -all                             every run pooled into one table
//	report -query groupby_q1 -history       one query over time
//
// It reads JSON Lines and writes Markdown. That is the whole program. The
// charts and the site are generated from the same files by whatever is best at
// making charts, and it is not this.
//
// Results from different machines are never combined. A pooled table across
// two machines is not a comparison of libraries, it is a comparison of
// machines wearing the wrong label, so this refuses to make one.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/kuma-bench/bench"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "report: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		in      = flag.String("in", "results", "results file or directory")
		query   = flag.String("query", "", "only this query")
		library = flag.String("library", "", "only this library")
		size    = flag.String("size", "", "only this dataset size")
		all     = flag.Bool("all", false, "pool every run rather than taking the latest")
		history = flag.Bool("history", false, "print one line per run over time")
		strict  = flag.Bool("strict", false, "exit non-zero if the libraries disagree on an answer")
	)
	flag.Parse()

	files, err := resultFiles(*in)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no result files under %s", *in)
	}

	runs, err := load(files)
	if err != nil {
		return err
	}
	if !*all && !*history {
		runs = runs[len(runs)-1:]
	}

	var results []bench.Result
	for _, r := range runs {
		results = append(results, r.results...)
	}
	results = filter(results, *query, *library, *size)
	if len(results) == 0 {
		return fmt.Errorf("nothing matched")
	}

	if *history {
		if *query == "" {
			return fmt.Errorf("-history needs -query, since a chart of every query at once is not a chart")
		}
		printHistory(runs, *query, *library, *size)
		return nil
	}

	if err := checkOneMachine(results); err != nil {
		return err
	}
	// The table is printed either way, because the table is how someone works
	// out what happened. Under -strict the exit code carries the verdict, and
	// that is what CI runs: a timing next to a wrong answer is worse than no
	// timing at all, so it should fail the build rather than print a warning
	// into a log nobody reads.
	mismatched := disagreements(results)
	if len(mismatched) > 0 && !*strict {
		fmt.Fprintf(os.Stderr, "warning: the libraries disagree on %s\n\n", strings.Join(mismatched, ", "))
	}
	fmt.Print(bench.Table(results))
	if *strict && len(mismatched) > 0 {
		return fmt.Errorf("the libraries computed different answers for %s", strings.Join(mismatched, ", "))
	}
	return nil
}

// runFile is one results file and what came out of it.
type runFile struct {
	path    string
	started time.Time
	results []bench.Result
}

func load(paths []string) ([]runFile, error) {
	var out []runFile
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		results, err := bench.ReadAll(f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if len(results) == 0 {
			continue
		}
		out = append(out, runFile{path: path, started: results[0].Timestamp, results: results})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].started.Before(out[j].started) })
	return out, nil
}

func resultFiles(in string) ([]string, error) {
	info, err := os.Stat(in)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{in}, nil
	}
	return filepath.Glob(filepath.Join(in, "*.jsonl"))
}

func filter(results []bench.Result, query, library, size string) []bench.Result {
	var out []bench.Result
	for _, r := range results {
		if query != "" && r.Query != query {
			continue
		}
		if library != "" && r.Library != library {
			continue
		}
		if size != "" && r.Size != size {
			continue
		}
		out = append(out, r)
	}
	return out
}

// printHistory is the output this repository exists for: one query, one line
// per run, over as long as there is history. A table says which library is
// faster today. This says which commit made it slower.
func printHistory(runs []runFile, query, library, size string) {
	libs := map[string]bool{}
	for _, run := range runs {
		for _, r := range filter(run.results, query, library, size) {
			libs[r.Library] = true
		}
	}
	names := make([]string, 0, len(libs))
	for lib := range libs {
		names = append(names, lib)
	}
	sort.Strings(names)

	fmt.Printf("| date | commit |")
	for _, lib := range names {
		fmt.Printf(" %s |", lib)
	}
	fmt.Printf("\n| --- | --- |")
	for range names {
		fmt.Printf(" ---: |")
	}
	fmt.Println()

	for _, run := range runs {
		matched := filter(run.results, query, library, size)
		if len(matched) == 0 {
			continue
		}
		fmt.Printf("| %s | %s |", run.started.Format("2006-01-02"), shortCommit(matched))
		for _, lib := range names {
			var forLib []bench.Result
			for _, r := range matched {
				if r.Library == lib {
					forLib = append(forLib, r)
				}
			}
			if median := bench.Median(forLib); median == 0 {
				fmt.Printf(" |")
			} else {
				fmt.Printf(" %s |", bench.FormatDuration(median))
			}
		}
		fmt.Println()
	}
}

func shortCommit(results []bench.Result) string {
	for _, r := range results {
		if len(r.Commit) >= 8 {
			return r.Commit[:8]
		}
		if r.Commit != "" {
			return r.Commit
		}
	}
	return "unknown"
}

// checkOneMachine refuses to build a table from results taken on different
// hardware. Two runners with the same instance type are not the same machine,
// and a table that mixes them reports whichever library happened to land on
// the quieter host.
func checkOneMachine(results []bench.Result) error {
	seen := map[string]bool{}
	var machines []string
	for _, r := range results {
		key := fmt.Sprintf("%s/%s %s %d cpus", r.Machine.OS, r.Machine.Arch, r.Machine.CPUModel, r.Machine.CPUs)
		if !seen[key] {
			seen[key] = true
			machines = append(machines, key)
		}
	}
	if len(machines) <= 1 {
		return nil
	}
	sort.Strings(machines)
	return fmt.Errorf("these results come from %d machines and are not comparable:\n  %s\nnarrow it down with -size, or point -in at one file",
		len(machines), strings.Join(machines, "\n  "))
}

// disagreements returns the queries where the libraries computed different
// answers. Timing a wrong answer is the failure mode that makes a benchmark
// worse than useless, so it is checked every time the table is printed rather
// than in some separate command nobody runs.
func disagreements(results []bench.Result) []string {
	sums := map[string]map[string]bool{}
	order := []string{}
	for _, r := range results {
		if !r.OK() || r.Checksum == "" {
			continue
		}
		if sums[r.Query] == nil {
			sums[r.Query] = map[string]bool{}
			order = append(order, r.Query)
		}
		sums[r.Query][r.Checksum] = true
	}
	var out []string
	for _, q := range order {
		if len(sums[q]) > 1 {
			out = append(out, q)
		}
	}
	return out
}
