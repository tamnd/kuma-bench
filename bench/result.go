// Package bench defines the record every benchmark run produces.
//
// One record is one query, run once, by one library. Everything downstream,
// meaning the tables, the charts and the regression checks, reads these and
// nothing else. Keeping that surface narrow is what lets a runner be written
// in Python or Go or anything else without the reporting side caring.
//
// Records are written as JSON Lines, one object per line, appended to a file
// per run under results/. Those files are committed. The history is the point.
package bench

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"time"
)

// Result is one timed query.
type Result struct {
	// Suite is the benchmark family, for example "dbbench" or "tpch".
	Suite string `json:"suite"`
	// Query is the query name within the suite, for example "groupby_q1".
	Query string `json:"query"`
	// Size is the dataset size label, for example "0.5GB".
	Size string `json:"size"`

	// Library is "kuma", "pandas" or "polars".
	Library string `json:"library"`
	// LibraryVersion is the exact version that ran, never a range.
	LibraryVersion string `json:"library_version"`
	// Mode records how the library was configured, for example "lazy",
	// "streaming" or "arrow-backend". A benchmark that does not record this is
	// not reproducible, because the same library can differ several fold
	// depending on which path it took.
	Mode string `json:"mode,omitempty"`

	// Run is the repetition index, starting at 1. Run 1 is the cold run and is
	// reported separately, because it includes page cache misses and lazy
	// initialization and it is the one users actually feel.
	Run int `json:"run"`

	// Elapsed is wall time for the whole query, including reading the input.
	Elapsed Duration `json:"elapsed"`
	// PeakRSS is the peak resident set size in bytes for the query process.
	PeakRSS int64 `json:"peak_rss_bytes,omitempty"`
	// InRows and OutRows are the input and result cardinalities. OutRows is
	// how a reviewer checks that the query actually ran, so it is required
	// even though it is not a timing.
	InRows  int64 `json:"in_rows"`
	OutRows int64 `json:"out_rows"`
	// Checksum is a stable digest of the result, used to confirm that all
	// three libraries computed the same answer. A fast wrong answer is not a
	// benchmark result.
	Checksum string `json:"checksum,omitempty"`

	// Err is set when the query failed or was not implemented. A failed run is
	// still a record, because a suite that silently drops the queries a
	// library cannot do reports a misleading average over the ones it can.
	Err string `json:"error,omitempty"`

	// Timestamp is when the query finished, in UTC.
	Timestamp time.Time `json:"timestamp"`
	// Machine describes what it ran on.
	Machine Machine `json:"machine"`
	// Commit is the kuma commit under test, for the kuma rows.
	Commit string `json:"commit,omitempty"`
}

// OK reports whether the query completed.
func (r Result) OK() bool { return r.Err == "" }

// Machine describes the host a run happened on. Results from different
// machines are not comparable, so this travels with every record rather than
// being written once in a header somewhere and lost.
type Machine struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	CPUModel  string `json:"cpu_model,omitempty"`
	CPUs      int    `json:"cpus"`
	MemoryGB  int    `json:"memory_gb,omitempty"`
	GoVersion string `json:"go_version,omitempty"`
	// SIMD records whether GOEXPERIMENT=simd was set for the kuma build. It is
	// a pointer so that "not applicable" and "off" stay distinguishable in the
	// JSON, since they mean different things for a pandas row.
	SIMD *bool `json:"simd,omitempty"`
	// Runner is "github-actions" or "bare-metal" or similar. Shared cloud
	// runners have noisy neighbours and their numbers deserve a caveat.
	Runner string `json:"runner,omitempty"`
}

// LocalMachine fills in what can be determined from the running process. The
// fields it cannot know, meaning the CPU model, the memory size and the
// runner, are left to the caller.
func LocalMachine() Machine {
	return Machine{
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		CPUs:      runtime.NumCPU(),
		GoVersion: runtime.Version(),
	}
}

// Duration is a time.Duration that marshals as fractional seconds, because
// every tool that will read these files afterwards expects a number rather
// than Go's duration string.
type Duration time.Duration

// MarshalJSON implements json.Marshaler.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).Seconds())
}

// UnmarshalJSON implements json.Unmarshaler.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var secs float64
	if err := json.Unmarshal(b, &secs); err != nil {
		return fmt.Errorf("bench: duration must be a number of seconds: %w", err)
	}
	*d = Duration(secs * float64(time.Second))
	return nil
}

// String returns the duration in a human readable form.
func (d Duration) String() string { return time.Duration(d).String() }

// Writer appends results to a JSON Lines file.
type Writer struct {
	w   *bufio.Writer
	f   *os.File
	enc *json.Encoder
}

// Create opens path for appending, creating it if it does not exist.
func Create(path string) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("bench: open results file: %w", err)
	}
	bw := bufio.NewWriter(f)
	return &Writer{w: bw, f: f, enc: json.NewEncoder(bw)}, nil
}

// Write appends one result.
func (w *Writer) Write(r Result) error {
	if err := w.enc.Encode(r); err != nil {
		return fmt.Errorf("bench: encode result: %w", err)
	}
	return nil
}

// Close flushes and closes the file.
func (w *Writer) Close() error {
	if err := w.w.Flush(); err != nil {
		_ = w.f.Close()
		return fmt.Errorf("bench: flush results: %w", err)
	}
	if err := w.f.Close(); err != nil {
		return fmt.Errorf("bench: close results: %w", err)
	}
	return nil
}

// ReadAll reads every result from r.
func ReadAll(r io.Reader) ([]Result, error) {
	var out []Result
	dec := json.NewDecoder(r)
	for {
		var res Result
		err := dec.Decode(&res)
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, fmt.Errorf("bench: decode result %d: %w", len(out)+1, err)
		}
		out = append(out, res)
	}
}

// Median returns the median elapsed time of the successful runs, ignoring the
// cold run. It returns zero if there is nothing left to take a median of.
//
// The cold run is dropped here rather than never recorded, because it is
// reported on its own elsewhere and throwing it away at collection time would
// make that impossible.
func Median(results []Result) Duration {
	warm := make([]float64, 0, len(results))
	for _, r := range results {
		if r.OK() && r.Run > 1 {
			warm = append(warm, time.Duration(r.Elapsed).Seconds())
		}
	}
	if len(warm) == 0 {
		return 0
	}
	sort.Float64s(warm)
	mid := len(warm) / 2
	if len(warm)%2 == 1 {
		return Duration(warm[mid] * float64(time.Second))
	}
	return Duration((warm[mid-1] + warm[mid]) / 2 * float64(time.Second))
}
