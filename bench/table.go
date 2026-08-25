package bench

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Table renders results as a Markdown table: one row per query, one column per
// library, the median of the warm runs in each cell.
//
// The fastest cell in a row is marked with a star and the slowest is left
// plain. There is no colour and no bar chart, because this output has to be
// readable in a terminal, in a pull request comment and in a plain text log,
// and every attempt to make it prettier than that has made one of those worse.
//
// Queries a library could not do show the reason rather than a blank. A blank
// reads as zero, or as an oversight, and it is neither.
func Table(results []Result) string {
	if len(results) == 0 {
		return "no results\n"
	}

	libs := distinct(results, func(r Result) string { return r.Library })
	queries := ordered(results)

	// Group once. Doing this inside the render loop is quadratic and this
	// function gets called on files with a few hundred thousand records.
	type cell struct {
		median Duration
		err    string
	}
	cells := map[string]map[string]cell{}
	for _, q := range queries {
		cells[q] = map[string]cell{}
	}
	byKey := map[string][]Result{}
	for _, r := range results {
		byKey[r.Query+"\x00"+r.Library] = append(byKey[r.Query+"\x00"+r.Library], r)
	}
	for key, rs := range byKey {
		q, lib, _ := strings.Cut(key, "\x00")
		c := cell{median: Median(rs)}
		for _, r := range rs {
			if !r.OK() {
				c.err = r.Err
				break
			}
		}
		cells[q][lib] = c
	}

	var b strings.Builder
	b.WriteString("| query |")
	for _, lib := range libs {
		fmt.Fprintf(&b, " %s |", lib)
	}
	b.WriteString("\n| --- |")
	for range libs {
		b.WriteString(" ---: |")
	}
	b.WriteString("\n")

	for _, q := range queries {
		fastest, best := "", Duration(0)
		for _, lib := range libs {
			c := cells[q][lib]
			if c.err != "" || c.median == 0 {
				continue
			}
			if best == 0 || c.median < best {
				fastest, best = lib, c.median
			}
		}

		fmt.Fprintf(&b, "| %s |", q)
		for _, lib := range libs {
			c := cells[q][lib]
			switch {
			case c.err != "":
				fmt.Fprintf(&b, " %s |", short(c.err))
			case c.median == 0:
				b.WriteString(" not run |")
			case lib == fastest:
				fmt.Fprintf(&b, " **%s** |", FormatDuration(c.median))
			default:
				fmt.Fprintf(&b, " %s |", FormatDuration(c.median))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// FormatDuration writes a duration at three significant figures, so that a 12
// second query and a 4 millisecond one line up in the same column without
// either turning into noise. Go's own duration string is not usable here,
// because 1.293486853s in a table is nine digits of precision the measurement
// does not have.
func FormatDuration(d Duration) string {
	s := time.Duration(d).Seconds()
	switch {
	case s >= 100:
		return fmt.Sprintf("%.0fs", s)
	case s >= 10:
		return fmt.Sprintf("%.1fs", s)
	case s >= 1:
		return fmt.Sprintf("%.2fs", s)
	case s >= 0.001:
		return fmt.Sprintf("%.0fms", s*1000)
	default:
		return fmt.Sprintf("%.0fus", s*1e6)
	}
}

// short trims an error to something that fits in a table cell. The full text
// stays in the JSON, which is where anyone debugging will be looking anyway.
func short(err string) string {
	const limit = 40
	err = strings.ReplaceAll(err, "|", " ")
	if i := strings.IndexAny(err, "\n\r"); i >= 0 {
		err = err[:i]
	}
	if len(err) > limit {
		err = err[:limit-3] + "..."
	}
	return err
}

func distinct(results []Result, key func(Result) string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range results {
		if k := key(r); k != "" && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// ordered returns the query names in the order they were first seen, which is
// the order the suite defines them in. Sorting would put q10 between q1 and
// q2, and a table where the rows are not in suite order is harder to read
// against the published db-benchmark page.
func ordered(results []Result) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range results {
		if !seen[r.Query] {
			seen[r.Query] = true
			out = append(out, r.Query)
		}
	}
	return out
}
