// Command kumarunner measures one db-benchmark query using kuma.
//
// It follows the same contract as the Python runners: take a query name and
// its input files, print one JSON object, exit zero.
//
// Almost every query in here reports that it is not implemented, and that is
// the intended state rather than an unfinished file. Running the whole suite
// today produces a table where kuma has fifteen rows saying which milestone
// each query is waiting on, next to real pandas and Polars numbers. That table
// is worth having from the beginning: it makes the harness a thing that works,
// so the first query kuma can actually answer gets a number the day it lands
// rather than the week someone remembers to wire it up.
//
// Queries move from the pending list to a real implementation as the engine
// grows. The rule is that a query is only implemented once it can be written
// the way a user would write it. Reaching into unexported machinery to make a
// benchmark number happen would measure something nobody can reproduce.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/tamnd/kuma"
)

// This is its own Go module, separate from the rest of kuma-bench, and that is
// deliberate. The kuma-bench module depends on the standard library and
// nothing else, so it stays buildable when kuma's API is mid rewrite. Only
// this directory tracks kuma, and only this directory breaks when kuma breaks.

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
	"groupby_q1":  "needs the CSV reader and hash aggregation, milestone M4",
	"groupby_q2":  "needs multi key hash aggregation, milestone M4",
	"groupby_q3":  "needs hash aggregation with a high cardinality key, milestone M4",
	"groupby_q4":  "needs the mean aggregate, milestone M4",
	"groupby_q5":  "needs hash aggregation with a high cardinality key, milestone M4",
	"groupby_q6":  "needs the median and standard deviation aggregates, milestone M4",
	"groupby_q7":  "needs expressions over aggregate results, milestone M4",
	"groupby_q8":  "needs top k within a group, milestone M5",
	"groupby_q9":  "needs the correlation aggregate, milestone M5",
	"groupby_q10": "needs six key hash aggregation, milestone M4",
	"join_q1":     "needs the hash join, milestone M5",
	"join_q2":     "needs the hash join, milestone M5",
	"join_q3":     "needs the left outer hash join, milestone M5",
	"join_q4":     "needs the hash join on a string key, milestone M5",
	"join_q5":     "needs the hash join at input scale, milestone M5",
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

	r := report{LibraryVersion: kuma.Version}
	if reason, ok := pending[*query]; ok {
		r.Err = reason
	} else {
		r.Err = fmt.Sprintf("the kuma runner has no implementation for %s", *query)
	}

	// Exit zero even though there is no timing. A query kuma cannot answer is
	// a result, and the orchestrator needs it in the file rather than as a
	// non-zero exit code it has to guess the meaning of.
	if err := json.NewEncoder(os.Stdout).Encode(r); err != nil {
		fmt.Fprintf(os.Stderr, "kumarunner: %v\n", err)
		os.Exit(1)
	}
}
