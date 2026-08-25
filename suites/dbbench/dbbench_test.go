package dbbench_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/tamnd/kuma-bench/suites/dbbench"
)

func TestQueriesAreTheSuite(t *testing.T) {
	if got := len(dbbench.Queries); got != 15 {
		t.Errorf("the suite has %d queries, want 10 group by and 5 join", got)
	}
	if got := len(dbbench.OfKind(dbbench.GroupBy)); got != 10 {
		t.Errorf("got %d group by queries, want 10", got)
	}
	if got := len(dbbench.OfKind(dbbench.Join)); got != 5 {
		t.Errorf("got %d join queries, want 5", got)
	}
}

func TestQueriesAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, q := range dbbench.Queries {
		if seen[q.Name] {
			t.Errorf("%s appears twice", q.Name)
		}
		seen[q.Name] = true

		if q.Question == "" {
			t.Errorf("%s has no question", q.Name)
		}
		if q.Kind != dbbench.GroupBy && q.Kind != dbbench.Join {
			t.Errorf("%s has kind %q", q.Name, q.Kind)
		}
		if !strings.HasPrefix(q.Name, string(q.Kind)+"_q") {
			t.Errorf("%s is a %s query but is not named like one", q.Name, q.Kind)
		}
	}
}

func TestLookup(t *testing.T) {
	q, ok := dbbench.Lookup("groupby_q6")
	if !ok {
		t.Fatal("groupby_q6 is not in the catalog")
	}
	if !q.Advanced {
		t.Error("groupby_q6 computes a median and should be marked advanced")
	}
	if _, ok := dbbench.Lookup("groupby_q99"); ok {
		t.Error("Lookup found a query that does not exist")
	}
}

func TestNames(t *testing.T) {
	names := dbbench.Names()
	if len(names) != len(dbbench.Queries) {
		t.Fatalf("Names returned %d of %d queries", len(names), len(dbbench.Queries))
	}
	if names[0] != "groupby_q1" || names[len(names)-1] != "join_q5" {
		t.Errorf("Names is not in suite order: %v", names)
	}
}

func TestInputs(t *testing.T) {
	const rows = 10_000_000

	tests := []struct {
		query string
		want  []string
	}{
		{"groupby_q1", []string{"G1_1e7_1e2_0_0.csv"}},
		{"groupby_q10", []string{"G1_1e7_1e2_0_0.csv"}},
		{"join_q1", []string{"J1_1e7_NA_0_0.csv", "J1_1e7_1e1_0_0.csv"}},
		{"join_q2", []string{"J1_1e7_NA_0_0.csv", "J1_1e7_1e4_0_0.csv"}},
		{"join_q3", []string{"J1_1e7_NA_0_0.csv", "J1_1e7_1e4_0_0.csv"}},
		{"join_q4", []string{"J1_1e7_NA_0_0.csv", "J1_1e7_1e4_0_0.csv"}},
		{"join_q5", []string{"J1_1e7_NA_0_0.csv", "J1_1e7_1e7_0_0.csv"}},
	}
	for _, tt := range tests {
		q, ok := dbbench.Lookup(tt.query)
		if !ok {
			t.Fatalf("%s is not in the catalog", tt.query)
		}
		got, err := dbbench.Inputs(q, rows, 0)
		if err != nil {
			t.Fatalf("Inputs(%s): %v", tt.query, err)
		}
		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("Inputs(%s) = %v, want %v", tt.query, got, tt.want)
		}
	}
}

func TestInputsForEveryQuery(t *testing.T) {
	// A query with no inputs defined would fail forty minutes into a run
	// rather than at startup, so check the whole catalog at every size.
	for _, rows := range []int{10_000_000, 100_000_000, 1_000_000_000} {
		for _, q := range dbbench.Queries {
			in, err := dbbench.Inputs(q, rows, 0)
			if err != nil {
				t.Errorf("Inputs(%s, %d): %v", q.Name, rows, err)
				continue
			}
			want := 1
			if q.Kind == dbbench.Join {
				want = 2
			}
			if len(in) != want {
				t.Errorf("Inputs(%s, %d) returned %d files, want %d", q.Name, rows, len(in), want)
			}
		}
	}
}

func TestInputsRejectsNonsense(t *testing.T) {
	q := dbbench.Queries[0]
	for _, rows := range []int{0, -1} {
		if _, err := dbbench.Inputs(q, rows, 0); err == nil {
			t.Errorf("Inputs accepted %d rows", rows)
		}
	}
}

func TestInputsCarriesTheMissingValuePercentage(t *testing.T) {
	// A run with 5 percent nulls must not read the clean files, and the only
	// thing keeping those apart is the nas field in the name.
	q, _ := dbbench.Lookup("groupby_q1")
	got, err := dbbench.Inputs(q, 10_000_000, 5)
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != "G1_1e7_1e2_5_0.csv" {
		t.Errorf("Inputs = %v, want the 5 percent file", got)
	}
}

func TestInputsSmallJoinTableIsNeverEmpty(t *testing.T) {
	// A hundred thousand rows divided by a million is zero, and a zero row
	// right table makes every join return nothing, which reads as a very fast
	// engine rather than as a broken dataset.
	q, _ := dbbench.Lookup("join_q1")
	got, err := dbbench.Inputs(q, 100_000, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got[1], "_0_") && strings.HasPrefix(got[1], "J1_1e5_0") {
		t.Errorf("Inputs = %v, which names a zero row table", got)
	}
}

// queryName matches a query name wherever it appears as a quoted string, which
// covers the dict keys in the Python runners and the map keys in the Go one.
var queryName = regexp.MustCompile(`"((?:groupby|join)_q\d+)"`)

// TestRunnersImplementTheCatalog is the reason this package exists.
//
// Without it, a query added here and forgotten in one runner shows up as an
// empty column in the results table, and an empty column reads as an oversight
// in the reporting rather than as a missing implementation. The check is a
// text search rather than anything cleverer because two of the three runners
// are Python and there is nothing to import.
func TestRunnersImplementTheCatalog(t *testing.T) {
	runners := map[string]string{
		"pandas": filepath.Join("pandas", "run.py"),
		"polars": filepath.Join("polars", "run.py"),
		"kuma":   filepath.Join("kuma", "main.go"),
	}
	want := map[string]bool{}
	for _, name := range dbbench.Names() {
		want[name] = true
	}

	for lib, path := range runners {
		t.Run(lib, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("cannot read the %s runner: %v", lib, err)
			}

			found := map[string]bool{}
			for _, m := range queryName.FindAllStringSubmatch(string(src), -1) {
				found[m[1]] = true
			}

			for name := range want {
				if !found[name] {
					t.Errorf("the %s runner does not mention %s", lib, name)
				}
			}
			for name := range found {
				if !want[name] {
					t.Errorf("the %s runner has %s, which is not in the catalog", lib, name)
				}
			}
		})
	}
}

// TestFileNamesMatchTheGenerator guards the one duplication in this repository
// that would fail silently. The generator names its output and this package
// names what it reads, and if they ever disagree the result is a run that
// stops at startup saying a file is missing, which is at least loud. What
// would not be loud is the two agreeing on the group by name and not the join
// one, so the whole set is compared against a written down list.
func TestFileNamesMatchTheGenerator(t *testing.T) {
	var got []string
	seen := map[string]bool{}
	for _, q := range dbbench.Queries {
		in, err := dbbench.Inputs(q, 10_000_000, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range in {
			if !seen[name] {
				seen[name] = true
				got = append(got, name)
			}
		}
	}
	sort.Strings(got)

	// These are exactly the files cmd/gendata writes for -size 0.5GB. If this
	// list changes, the generator changed and both sides need looking at.
	want := []string{
		"G1_1e7_1e2_0_0.csv",
		"J1_1e7_1e1_0_0.csv",
		"J1_1e7_1e4_0_0.csv",
		"J1_1e7_1e7_0_0.csv",
		"J1_1e7_NA_0_0.csv",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the suite reads\n  %v\nand the generator writes\n  %v", got, want)
	}
}
