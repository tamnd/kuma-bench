package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"testing"
)

// render runs a writer and returns the CSV it produced, parsed. Everything in
// this file goes through it, because the property that matters about this
// command is not what the numbers are but that the file is well formed and
// reproducible.
func render(t *testing.T, write func(*bufio.Writer) error) [][]string {
	t.Helper()

	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	if err := write(w); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	r := csv.NewReader(strings.NewReader(buf.String()))
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("the generated file is not valid CSV: %v", err)
	}
	return rows
}

func TestGroupByShape(t *testing.T) {
	const n, k = 5000, 100
	rows := render(t, groupBy{n: n, k: k, seed: 1}.write)

	if len(rows) != n+1 {
		t.Fatalf("got %d lines, want %d rows plus a header", len(rows), n)
	}
	want := []string{"id1", "id2", "id3", "id4", "id5", "id6", "v1", "v2", "v3"}
	if got := rows[0]; !equal(got, want) {
		t.Errorf("header = %v, want %v", got, want)
	}
}

func TestGroupByCardinalities(t *testing.T) {
	// n over k is 50, so id3 should land close to 50 distinct values and id1
	// close to 100. Close rather than exact, because these are drawn with
	// replacement and a value can go unpicked.
	const n, k = 5000, 100
	rows := render(t, groupBy{n: n, k: k, seed: 1}.write)[1:]

	tests := []struct {
		col   int
		name  string
		upper int
	}{
		{0, "id1", k},
		{1, "id2", k},
		{2, "id3", n / k},
		{3, "id4", k},
		{4, "id5", k},
		{5, "id6", n / k},
	}
	for _, tt := range tests {
		seen := map[string]bool{}
		for _, row := range rows {
			seen[row[tt.col]] = true
		}
		if len(seen) > tt.upper {
			t.Errorf("%s has %d distinct values, more than the %d it was asked for", tt.name, len(seen), tt.upper)
		}
		// Half the requested cardinality is a floor no honest draw goes under
		// at this row count, and it catches the real bug, which is a column
		// accidentally generated from the wrong key space.
		if len(seen) < tt.upper/2 {
			t.Errorf("%s has %d distinct values, far below the %d it was asked for", tt.name, len(seen), tt.upper)
		}
	}
}

func TestGroupByValueRanges(t *testing.T) {
	rows := render(t, groupBy{n: 2000, k: 100, seed: 7}.write)[1:]

	for i, row := range rows {
		for col := range 3 {
			if !strings.HasPrefix(row[col], "id") {
				t.Fatalf("row %d column %d = %q, want an id prefix", i+1, col, row[col])
			}
		}
		checkRange(t, i, "v1", row[6], 1, 5)
		checkRange(t, i, "v2", row[7], 1, 15)
		checkFloatRange(t, i, "v3", row[8], 0, 100)
	}
}

func TestGroupByIsDeterministic(t *testing.T) {
	g := groupBy{n: 1000, k: 50, seed: 42}
	first, second := render(t, g.write), render(t, g.write)

	if len(first) != len(second) {
		t.Fatalf("two runs produced %d and %d lines", len(first), len(second))
	}
	for i := range first {
		if !equal(first[i], second[i]) {
			t.Fatalf("line %d differs between runs: %v then %v", i, first[i], second[i])
		}
	}
}

func TestGroupBySeedChangesTheData(t *testing.T) {
	a := render(t, groupBy{n: 1000, k: 50, seed: 1}.write)
	b := render(t, groupBy{n: 1000, k: 50, seed: 2}.write)

	same := 0
	for i := range a {
		if equal(a[i], b[i]) {
			same++
		}
	}
	// The header always matches. Anything much beyond that means the seed is
	// not reaching the generator, which is a mistake that would otherwise go
	// unnoticed for as long as nobody changes the seed.
	if same > len(a)/10 {
		t.Errorf("%d of %d lines are identical across seeds", same, len(a))
	}
}

func TestGroupByMissingValues(t *testing.T) {
	rows := render(t, groupBy{n: 5000, k: 100, nas: 10, seed: 3}.write)[1:]

	blank, total := 0, 0
	for _, row := range rows {
		for _, field := range row {
			total++
			if field == "" {
				blank++
			}
		}
	}
	got := float64(blank) / float64(total) * 100
	if got < 8 || got > 12 {
		t.Errorf("%.1f%% of fields are missing, want about 10%%", got)
	}
}

func TestGroupByNoMissingValuesByDefault(t *testing.T) {
	for _, row := range render(t, groupBy{n: 1000, k: 100, seed: 3}.write)[1:] {
		for col, field := range row {
			if field == "" {
				t.Fatalf("column %d is empty with nas set to 0", col)
			}
		}
	}
}

func TestJoinSizes(t *testing.T) {
	j := join{n: 4_000_000, seed: 1}
	small, medium, big := j.sizes()

	if small != 4 || medium != 4000 || big != 4_000_000 {
		t.Errorf("sizes = %d, %d, %d, want 4, 4000, 4000000", small, medium, big)
	}
}

func TestJoinSizesNeverZero(t *testing.T) {
	// A thousand rows would give a small table of zero rows by division, and a
	// zero row right table makes every join query return nothing, which reads
	// as a very fast engine.
	small, medium, big := join{n: 1000, seed: 1}.sizes()
	if small < 1 || medium < 1 || big < 1 {
		t.Errorf("sizes = %d, %d, %d, want all at least 1", small, medium, big)
	}
}

func TestJoinLeftShape(t *testing.T) {
	const n = 3000
	rows := render(t, join{n: n, seed: 1}.writeLeft)

	if len(rows) != n+1 {
		t.Fatalf("got %d lines, want %d rows plus a header", len(rows), n)
	}
	want := []string{"id1", "id2", "id3", "id4", "id5", "id6", "v1"}
	if !equal(rows[0], want) {
		t.Errorf("header = %v, want %v", rows[0], want)
	}
	for i, row := range rows[1:] {
		// The string keys are the integer keys with a prefix, and a query that
		// joins on id4 must return exactly what a query joining on id1 does.
		for col := range 3 {
			if row[col+3] != "id"+row[col] {
				t.Fatalf("row %d: %s does not match %s", i+1, row[col+3], row[col])
			}
		}
	}
}

func TestJoinRightTables(t *testing.T) {
	// The width is what varies here, not the key count, so the size is fixed
	// at something large enough for the nine in ten sample to be stable. Real
	// runs give the small table as few as ten rows, and at ten rows the ratio
	// swings by twenty points on a single key, so it is checked separately.
	const size = 20_000
	j := join{n: 20_000_000, seed: 1}

	tests := []struct {
		name   string
		width  int
		header []string
	}{
		{"small", 1, []string{"id1", "id4", "v2"}},
		{"medium", 2, []string{"id1", "id2", "id4", "id5", "v2"}},
		{"big", 3, []string{"id1", "id2", "id3", "id4", "id5", "id6", "v2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := render(t, func(w *bufio.Writer) error { return j.writeRight(w, size, tt.width) })
			if !equal(rows[0], tt.header) {
				t.Fatalf("header = %v, want %v", rows[0], tt.header)
			}
			for i, row := range rows[1:] {
				if len(row) != len(tt.header) {
					t.Fatalf("row %d has %d fields, want %d", i+1, len(row), len(tt.header))
				}
				// The string keys mirror the integer keys, so joining on id4
				// must give the same answer as joining on id1.
				for col := range tt.width {
					if row[col+tt.width] != "id"+row[col] {
						t.Fatalf("row %d: %s does not match %s", i+1, row[col+tt.width], row[col])
					}
				}
			}
			// About nine keys in ten are kept, and the missing tenth is what
			// makes an inner join drop rows and an outer join produce nulls.
			kept := float64(len(rows)-1) / size
			if kept < 0.88 || kept > 0.92 {
				t.Errorf("kept %.0f%% of keys, want about 90%%", kept*100)
			}
		})
	}
}

func TestJoinKeysAreUnique(t *testing.T) {
	// The last key column of every right table is its primary key. If it
	// repeats, an inner join multiplies rows and every downstream number is
	// wrong in a way that looks like a slow engine.
	j := join{n: 50_000, seed: 1}
	rows := render(t, func(w *bufio.Writer) error { return j.writeRight(w, 50, 1) })[1:]

	seen := map[string]bool{}
	for _, row := range rows {
		if seen[row[0]] {
			t.Fatalf("key %s appears twice", row[0])
		}
		seen[row[0]] = true
	}
}

func TestMatchesKeepsAboutNineInTen(t *testing.T) {
	kept := 0
	const n = 100_000
	for key := 1; key <= n; key++ {
		if matches(key) {
			kept++
		}
	}
	if r := float64(kept) / n; r < 0.88 || r > 0.92 {
		t.Errorf("matches kept %.1f%% of keys, want about 90%%", r*100)
	}
}

func TestMatchesIsNotAStride(t *testing.T) {
	// The obvious implementation, key%10 != 0, would also pass the ratio test
	// above while making every tenth key missing, which is a pattern a hash
	// join could get lucky on. Check that the gaps are not evenly spaced.
	gaps := map[int]int{}
	last := 0
	for key := 1; key <= 10_000; key++ {
		if !matches(key) {
			gaps[key-last]++
			last = key
		}
	}
	if len(gaps) < 5 {
		t.Errorf("missing keys fall at only %d distinct spacings, which is a stride not a sample", len(gaps))
	}
}

func TestRound6(t *testing.T) {
	tests := []struct {
		in   float64
		want float64
	}{
		{0, 0},
		{1, 1},
		{0.1234564, 0.123456},
		{0.1234566, 0.123457},
		{99.9999994, 99.999999},
	}
	for _, tt := range tests {
		if got := round6(tt.in); got != tt.want {
			t.Errorf("round6(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestExp(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{10_000_000, "1e7"},
		{100, "1e2"},
		{1_000_000_000, "1e9"},
		{10, "1e1"},
		{1, "1e0"},
		{0, "0"},
		{500, "500"},   // not a power of ten, so it is written out
		{1234, "1234"}, // likewise
	}
	for _, tt := range tests {
		if got := exp(tt.in); got != tt.want {
			t.Errorf("exp(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRowsForSize(t *testing.T) {
	tests := []struct {
		size string
		want int
		ok   bool
	}{
		{"0.5GB", 10_000_000, true},
		{"5GB", 100_000_000, true},
		{"50GB", 1_000_000_000, true},
		{"1GB", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		got, err := rowsForSize(tt.size)
		if (err == nil) != tt.ok {
			t.Errorf("rowsForSize(%q) error = %v, want ok = %v", tt.size, err, tt.ok)
			continue
		}
		if got != tt.want {
			t.Errorf("rowsForSize(%q) = %d, want %d", tt.size, got, tt.want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1536, "1.5 KiB"},
		{5 << 20, "5.0 MiB"},
		{3 << 30, "3.0 GiB"},
	}
	for _, tt := range tests {
		if got := humanBytes(tt.in); got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func BenchmarkGroupBy(b *testing.B) {
	g := groupBy{n: 100_000, k: 100, seed: 1}
	w := bufio.NewWriter(io.Discard)

	b.SetBytes(int64(g.n))
	for b.Loop() {
		if err := g.write(w); err != nil {
			b.Fatal(err)
		}
	}
	w.Flush()
}

func checkRange(t *testing.T, row int, name, field string, lo, hi int) {
	t.Helper()
	v := 0
	if _, err := fmt.Sscan(field, &v); err != nil {
		t.Fatalf("row %d: %s = %q is not an integer", row+1, name, field)
	}
	if v < lo || v > hi {
		t.Fatalf("row %d: %s = %d, want between %d and %d", row+1, name, v, lo, hi)
	}
}

func checkFloatRange(t *testing.T, row int, name, field string, lo, hi float64) {
	t.Helper()
	var v float64
	if _, err := fmt.Sscan(field, &v); err != nil {
		t.Fatalf("row %d: %s = %q is not a number", row+1, name, field)
	}
	if v < lo || v > hi {
		t.Fatalf("row %d: %s = %v, want between %v and %v", row+1, name, v, lo, hi)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
