package bench_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tamnd/kuma-bench/bench"
)

func TestParseReport(t *testing.T) {
	got, err := bench.ParseReport([]byte(`{"elapsed":1.25,"in_rows":1000,"out_rows":100,"library_version":"3.0.1"}`))
	if err != nil {
		t.Fatalf("ParseReport: %v", err)
	}
	if got.Elapsed != bench.Duration(1250*time.Millisecond) {
		t.Errorf("Elapsed = %v, want 1.25s", got.Elapsed)
	}
	if got.OutRows != 100 || got.InRows != 1000 {
		t.Errorf("rows = %d in, %d out, want 1000 and 100", got.InRows, got.OutRows)
	}
	if got.LibraryVersion != "3.0.1" {
		t.Errorf("LibraryVersion = %q, want 3.0.1", got.LibraryVersion)
	}
}

func TestParseReportIgnoresNoise(t *testing.T) {
	// Import warnings and progress bars land on standard output more often
	// than anyone would like, and a run that fails because of one is a run
	// nobody gets back.
	out := `UserWarning: pyarrow will become a required dependency
loading data
{"elapsed":0.5,"out_rows":10}
`
	got, err := bench.ParseReport([]byte(out))
	if err != nil {
		t.Fatalf("ParseReport: %v", err)
	}
	if got.OutRows != 10 {
		t.Errorf("OutRows = %d, want 10", got.OutRows)
	}
}

func TestParseReportTakesTheLastObject(t *testing.T) {
	out := "{\"elapsed\":9,\"out_rows\":1}\n{\"elapsed\":0.5,\"out_rows\":2}\n"
	got, err := bench.ParseReport([]byte(out))
	if err != nil {
		t.Fatalf("ParseReport: %v", err)
	}
	if got.OutRows != 2 {
		t.Errorf("OutRows = %d, want the last object, 2", got.OutRows)
	}
}

func TestParseReportWithNoReport(t *testing.T) {
	for _, in := range []string{"", "   ", "Traceback (most recent call last):\n  File run.py\n"} {
		if _, err := bench.ParseReport([]byte(in)); err == nil {
			t.Errorf("ParseReport(%q) succeeded, want an error", in)
		}
	}
}

func TestParseReportWithBadJSON(t *testing.T) {
	if _, err := bench.ParseReport([]byte(`{"elapsed": }`)); err == nil {
		t.Error("accepted malformed JSON, want an error")
	}
}

func TestReportResult(t *testing.T) {
	base := bench.Result{
		Suite: "dbbench", Query: "groupby_q1", Library: "pandas",
		LibraryVersion: "unknown", Mode: "default", Run: 3,
	}
	got := bench.Report{
		Elapsed: bench.Duration(time.Second), OutRows: 100,
		LibraryVersion: "3.0.1",
	}.Result(base)

	if got.Query != "groupby_q1" || got.Run != 3 {
		t.Errorf("the orchestrator's fields did not survive: %+v", got)
	}
	if got.LibraryVersion != "3.0.1" {
		t.Errorf("LibraryVersion = %q, want the runner's 3.0.1", got.LibraryVersion)
	}
	// The runner left Mode empty, so the orchestrator's value stands rather
	// than being blanked out.
	if got.Mode != "default" {
		t.Errorf("Mode = %q, want default", got.Mode)
	}
}

func TestReportResultCarriesTheError(t *testing.T) {
	got := bench.Report{Err: "not implemented until M4"}.Result(bench.Result{Query: "q9"})
	if got.OK() {
		t.Error("a report with an error produced an OK result")
	}
	if !strings.Contains(got.Err, "M4") {
		t.Errorf("Err = %q, want the runner's message", got.Err)
	}
}
