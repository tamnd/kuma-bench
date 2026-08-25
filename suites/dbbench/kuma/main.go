// Command kumarunner measures one db-benchmark query using kuma.
//
// It follows the same contract as the Python runners: take a query name and
// its input files, print one JSON object, exit zero.
//
// Thirteen of the fifteen queries are implemented. The two that are not report
// what they are waiting for and still produce a record, so the results table
// has a row for every query and says why the empty cells are empty.
//
// The rule for moving a query off that list is that it has to be written the
// way a user would write it. Reaching into unexported machinery to make a
// benchmark number happen would measure something nobody can reproduce, and
// hand rolling an aggregate the engine is supposed to provide would measure the
// runner rather than the engine.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/tamnd/kuma"
)

// This is its own Go module, separate from the rest of kuma-bench, and that is
// deliberate. The kuma-bench module depends on the standard library and
// nothing else, so it stays buildable when kuma's API is mid rewrite. Only
// this directory tracks kuma, and only this directory breaks when kuma breaks.

// mode says how kuma was configured, and goes in every record. Today there is
// nothing to configure: the kernels are scalar, the engine is eager and a query
// runs on the goroutine that asked for it. When the vectorized kernels arrive
// this is where the difference gets recorded.
const mode = "eager,scalar-kernels"

// report is the runner protocol, defined in bench/report.go. It is written out
// again here rather than imported, because importing it would make this module
// depend on the parent and undo the separation above. Seven fields is a cheap
// duplication and the test in suites/dbbench checks that they still agree.
type report struct {
	Elapsed        float64 `json:"elapsed,omitempty"`
	OutRows        int64   `json:"out_rows"`
	Checksum       string  `json:"checksum,omitempty"`
	PeakRSS        int64   `json:"peak_rss_bytes,omitempty"`
	LibraryVersion string  `json:"library_version,omitempty"`
	Mode           string  `json:"mode,omitempty"`
	Err            string  `json:"error,omitempty"`
}

// pending lists the queries that are not implemented yet and what each one is
// waiting for. The milestone references point at docs/08-milestones.md in the
// kuma repository.
var pending = map[string]string{
	"groupby_q8": "needs the two largest values within a group, milestone M4",
	"groupby_q9": "needs the correlation aggregate, milestone M4",
}

// inputs collects repeated -input flags, in the order the catalog gives them.
type inputs []string

func (in *inputs) String() string { return fmt.Sprint(*in) }

func (in *inputs) Set(v string) error {
	*in = append(*in, v)
	return nil
}

func main() {
	var files inputs
	query := flag.String("query", "", "which query to run")
	flag.Var(&files, "input", "an input file, repeatable, in catalog order")
	flag.Parse()

	if *query == "" {
		fmt.Fprintln(os.Stderr, "kumarunner: -query is required")
		os.Exit(2)
	}

	// Exit zero whatever happened. A query kuma cannot answer is a result, and
	// the orchestrator needs it in the file rather than as a non-zero exit code
	// it has to guess the meaning of.
	emit(run(*query, files))
}

// run measures one query and returns the record to print.
func run(query string, files []string) report {
	r := report{LibraryVersion: kuma.Version, Mode: mode}

	if reason, ok := pending[query]; ok {
		r.Err = reason
		return r
	}
	q, ok := queries[query]
	if !ok {
		r.Err = fmt.Sprintf("the kuma runner has no implementation for %s", query)
		return r
	}
	if len(files) != q.inputs {
		r.Err = fmt.Sprintf("%s reads %d files and was given %d", query, q.inputs, len(files))
		return r
	}

	// The clock covers the whole query, reading the files included, and stops
	// once the result exists. kuma is eager, so there is nothing to force: the
	// frame the last call returns is the answer, already materialized.
	start := time.Now()
	out, values, err := q.run(files)
	elapsed := time.Since(start)

	if err != nil {
		r.Err = err.Error()
		return r
	}

	// Counting the rows and taking the checksum happen after the clock stops.
	// Neither is part of what a user would be waiting for, so timing them would
	// be measuring this harness.
	sum, err := checksum(out, values)
	if err != nil {
		r.Err = err.Error()
		return r
	}

	r.Elapsed = elapsed.Seconds()
	r.OutRows = int64(out.NumRows())
	r.Checksum = sum
	r.PeakRSS = peakRSS()
	return r
}

// emit writes the record and exits.
func emit(r report) {
	if err := json.NewEncoder(os.Stdout).Encode(r); err != nil {
		fmt.Fprintf(os.Stderr, "kumarunner: %v\n", err)
		os.Exit(1)
	}
}
