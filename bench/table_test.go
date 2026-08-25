package bench_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tamnd/kuma-bench/bench"
)

func run(query, lib string, run int, seconds float64) bench.Result {
	return bench.Result{
		Query: query, Library: lib, Run: run,
		Elapsed: bench.Duration(seconds * float64(time.Second)),
		OutRows: 100,
	}
}

func TestTable(t *testing.T) {
	results := []bench.Result{
		run("q1", "kuma", 1, 9), run("q1", "kuma", 2, 1), run("q1", "kuma", 3, 1),
		run("q1", "pandas", 1, 9), run("q1", "pandas", 2, 4), run("q1", "pandas", 3, 4),
		run("q2", "kuma", 2, 2), run("q2", "pandas", 2, 8),
	}
	got := bench.Table(results)

	for _, want := range []string{"| query |", "kuma", "pandas", "| q1 |", "| q2 |"} {
		if !strings.Contains(got, want) {
			t.Errorf("table is missing %q:\n%s", want, got)
		}
	}
	// The cold run is 9 seconds for both, so if it leaked into the median the
	// numbers would be 5 and 6.5 rather than 1 and 4.
	if !strings.Contains(got, "**1.00s**") {
		t.Errorf("kuma's q1 median is not 1.00s and marked fastest:\n%s", got)
	}
	if !strings.Contains(got, "4.00s") {
		t.Errorf("pandas' q1 median is not 4.00s:\n%s", got)
	}
}

func TestTableMarksOneWinnerPerRow(t *testing.T) {
	results := []bench.Result{
		run("q1", "kuma", 2, 1), run("q1", "pandas", 2, 4), run("q1", "polars", 2, 2),
	}
	if n := strings.Count(bench.Table(results), "**"); n != 2 {
		t.Errorf("found %d bold markers, want one winner meaning two", n)
	}
}

func TestTableShowsFailuresRatherThanBlanks(t *testing.T) {
	results := []bench.Result{
		run("q9", "kuma", 2, 1),
		{Query: "q9", Library: "pandas", Run: 2, Err: "not implemented until M4"},
	}
	got := bench.Table(results)

	if !strings.Contains(got, "not implemented") {
		t.Errorf("the failure was not reported:\n%s", got)
	}
	// A cell showing 0.00s for a query that never ran is how a benchmark ends
	// up claiming an infinite speedup.
	if strings.Contains(got, "0.00s") || strings.Contains(got, "0ms") {
		t.Errorf("a failed query was rendered as a timing:\n%s", got)
	}
}

func TestTableWithAQueryThatOnlyRanCold(t *testing.T) {
	results := []bench.Result{run("q1", "kuma", 1, 5)}
	if got := bench.Table(results); !strings.Contains(got, "not run") {
		t.Errorf("a query with no warm runs should say so:\n%s", got)
	}
}

func TestTableKeepsSuiteOrder(t *testing.T) {
	// Sorted alphabetically, q10 lands between q1 and q2, which makes the
	// table impossible to read against the published one.
	var results []bench.Result
	for _, q := range []string{"q1", "q2", "q9", "q10"} {
		results = append(results, run(q, "kuma", 2, 1))
	}
	got := bench.Table(results)

	last := -1
	for _, q := range []string{"| q1 |", "| q2 |", "| q9 |", "| q10 |"} {
		i := strings.Index(got, q)
		if i < 0 {
			t.Fatalf("%s missing from:\n%s", q, got)
		}
		if i < last {
			t.Errorf("%s appears out of suite order:\n%s", q, got)
		}
		last = i
	}
}

func TestTableEmpty(t *testing.T) {
	if got := bench.Table(nil); got != "no results\n" {
		t.Errorf("Table(nil) = %q", got)
	}
}

func TestTableErrorsDoNotBreakTheColumns(t *testing.T) {
	// A pipe in an error message would split the cell and shift every column
	// after it, and Markdown gives no hint that it happened.
	results := []bench.Result{
		{Query: "q1", Library: "kuma", Run: 2, Err: "read data/a.csv | line 3: bad\nsecond line"},
		run("q1", "pandas", 2, 1),
	}
	// Three columns, meaning four pipes, on every line including the header.
	for i, line := range strings.Split(strings.TrimSpace(bench.Table(results)), "\n") {
		if n := strings.Count(line, "|"); n != 4 {
			t.Errorf("line %d has %d pipes, want 4: %q", i, n, line)
		}
	}
}

func TestTableFormatsAcrossScales(t *testing.T) {
	tests := []struct {
		seconds float64
		want    string
	}{
		{0.0000005, "0us"},
		{0.00025, "250us"},
		{0.004, "4ms"},
		{0.25, "250ms"},
		{1.5, "1.50s"},
		{12.5, "12.5s"},
		{125, "125s"},
	}
	for _, tt := range tests {
		got := bench.Table([]bench.Result{run("q1", "kuma", 2, tt.seconds)})
		if !strings.Contains(got, tt.want) {
			t.Errorf("%v seconds rendered as %q, want it to contain %q", tt.seconds, strings.TrimSpace(got), tt.want)
		}
	}
}
