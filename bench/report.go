package bench

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Report is what a runner prints and the orchestrator reads.
//
// A runner is a program that measures one query, once, for one library. It
// takes the query name, the data directory and the row count on the command
// line, and it writes exactly one JSON object to standard output. Anything
// else it wants to say goes to standard error, where it is logged and
// otherwise ignored.
//
// The orchestrator fills in everything a runner cannot know or should not be
// trusted with: which run this is, what machine it happened on, when it
// happened, and which kuma commit was under test. That split is the reason a
// runner can be forty lines of Python and still produce a record that is
// complete enough to publish.
//
// A runner that cannot do a query prints a Report with Err set and exits zero.
// That is a normal outcome, not a failure, and it is how this suite runs end
// to end from the first milestone when kuma implements almost none of it.
type Report struct {
	// Elapsed is wall time in seconds for the timed section, which starts
	// before the input is read and stops after the result is fully
	// materialized.
	Elapsed Duration `json:"elapsed"`
	// InRows is the input cardinality. A runner may leave it out, and most do,
	// because a lazy engine never materializes the input and counting it would
	// mean doing work the query did not ask for. The orchestrator knows the
	// row count it generated and fills it in.
	InRows int64 `json:"in_rows,omitempty"`
	// OutRows is the result cardinality. It is required, because it is the
	// only evidence in the record that the query ran rather than returning an
	// unevaluated plan.
	OutRows int64 `json:"out_rows"`
	// Checksum is a digest of the result, compared across libraries.
	Checksum string `json:"checksum,omitempty"`
	// PeakRSS is the peak resident set size of the runner process in bytes.
	PeakRSS int64 `json:"peak_rss_bytes,omitempty"`
	// LibraryVersion is the exact version string of the library that ran.
	LibraryVersion string `json:"library_version,omitempty"`
	// Mode is how the library was configured, for example "lazy" or
	// "streaming" or "arrow-backend".
	Mode string `json:"mode,omitempty"`
	// Err explains why there is no timing. An unimplemented query says so and
	// names the milestone that will implement it.
	Err string `json:"error,omitempty"`
}

// ParseReport reads a runner's output.
//
// Runners print diagnostics to standard error, but a stray print to standard
// output happens often enough that being strict about it costs an afternoon
// every time. So the last line that parses as an object wins, and anything
// before it is treated as noise.
func ParseReport(stdout []byte) (Report, error) {
	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var r Report
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return Report{}, fmt.Errorf("bench: runner printed an object that is not a report: %w", err)
		}
		return r, nil
	}
	return Report{}, fmt.Errorf("bench: runner printed no report")
}

// Result turns a report into the record that gets written, given the context
// only the orchestrator has.
func (r Report) Result(base Result) Result {
	base.Elapsed = r.Elapsed
	base.OutRows = r.OutRows
	base.Checksum = r.Checksum
	base.PeakRSS = r.PeakRSS
	base.Err = r.Err
	if r.LibraryVersion != "" {
		base.LibraryVersion = r.LibraryVersion
	}
	if r.Mode != "" {
		base.Mode = r.Mode
	}
	if r.InRows != 0 {
		base.InRows = r.InRows
	}
	return base
}
