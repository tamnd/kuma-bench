package main

import (
	"bufio"
	"fmt"
	"math/rand/v2"
	"strconv"
)

// The generators below reproduce the shape of the db-benchmark datasets: the
// same columns, the same cardinalities, the same value ranges. They do not
// reproduce the exact bytes, because the upstream generator uses NumPy's
// random number stream and we use Go's, and no amount of care makes those two
// agree.
//
// That matters for one thing only: our numbers cannot be pasted next to the
// numbers on the duckdblabs site and read as one table. Within this repository
// it changes nothing, because all three libraries read the identical files.
// The README says this too, in the place where someone comparing tables would
// look.

// groupBy generates the G1 dataset.
//
// The columns are three string keys, three integer keys with the same
// cardinalities, and three values. id1 and id2 have k distinct values, id3 has
// n/k, and that spread is the whole point of the suite: a group by over id1 is
// a hundred groups and a group by over id3 is a hundred thousand, and engines
// that do well at one often do badly at the other.
type groupBy struct {
	n    int    // rows
	k    int    // cardinality of id1, id2, id4 and id5
	nas  int    // percentage of values replaced by an empty field
	seed uint64 // fixed, so that the file is a function of the flags alone
}

func (g groupBy) write(w *bufio.Writer) error {
	if _, err := w.WriteString("id1,id2,id3,id4,id5,id6,v1,v2,v3\n"); err != nil {
		return err
	}

	r := newRand(g.seed)
	big := g.n / g.k // cardinality of id3 and id6

	// One reused line buffer. At a hundred million rows the allocator is a
	// measurable part of the runtime, and a generator that takes an hour is a
	// generator people work around.
	buf := make([]byte, 0, 96)

	for i := range g.n {
		id1 := r.IntN(g.k) + 1
		id2 := r.IntN(g.k) + 1
		id3 := r.IntN(big) + 1
		id4 := r.IntN(g.k) + 1
		id5 := r.IntN(g.k) + 1
		id6 := r.IntN(big) + 1
		v1 := r.IntN(5) + 1
		v2 := r.IntN(15) + 1
		v3 := round6(r.Float64() * 100)

		buf = buf[:0]
		buf = appendKey(buf, id1, g.blank(r))
		buf = append(buf, ',')
		buf = appendKey(buf, id2, g.blank(r))
		buf = append(buf, ',')
		buf = appendKey(buf, id3, g.blank(r))
		buf = append(buf, ',')
		buf = appendInt(buf, id4, g.blank(r))
		buf = append(buf, ',')
		buf = appendInt(buf, id5, g.blank(r))
		buf = append(buf, ',')
		buf = appendInt(buf, id6, g.blank(r))
		buf = append(buf, ',')
		buf = appendInt(buf, v1, g.blank(r))
		buf = append(buf, ',')
		buf = appendInt(buf, v2, g.blank(r))
		buf = append(buf, ',')
		buf = appendFloat(buf, v3, g.blank(r))
		buf = append(buf, '\n')

		if _, err := w.Write(buf); err != nil {
			return fmt.Errorf("row %d: %w", i+1, err)
		}
	}
	return nil
}

// blank reports whether this field should be written as missing. It draws even
// when nas is zero so that the value stream does not depend on the nas setting,
// which keeps the keys in a 5 percent nulls file lined up with the keys in a
// clean one.
func (g groupBy) blank(r *rand.Rand) bool {
	draw := r.IntN(100)
	return g.nas > 0 && draw < g.nas
}

// join generates the four J1 tables.
//
// The left table has n rows. The three right tables are keyed on progressively
// larger key spaces, so joining against them costs progressively more: a hash
// table that fits in L2, one that fits in memory, and one the same size as the
// left side. Roughly one key in ten is missing from every right table, so an
// inner join drops rows and an outer join produces nulls, which is the only
// way to tell that a join was actually performed.
type join struct {
	n    int
	seed uint64
}

// sizes returns the row counts of the three right hand tables: n/1e6, n/1e3
// and n. Each is at least one row.
func (j join) sizes() (small, medium, big int) {
	return max(j.n/1_000_000, 1), max(j.n/1_000, 1), j.n
}

// matches reports whether key belongs to the right hand table of the given
// size. It keeps about nine keys in ten and it is a pure function of the key,
// so nothing has to be materialized: at a billion rows, holding the set of
// selected keys in memory would cost more than the file.
func matches(key int) bool { return mix(uint64(key))%10 != 0 }

func (j join) writeLeft(w *bufio.Writer) error {
	if _, err := w.WriteString("id1,id2,id3,id4,id5,id6,v1\n"); err != nil {
		return err
	}
	small, medium, big := j.sizes()
	r := newRand(j.seed)
	buf := make([]byte, 0, 96)

	for i := range j.n {
		id1 := r.IntN(small) + 1
		id2 := r.IntN(medium) + 1
		id3 := r.IntN(big) + 1

		buf = buf[:0]
		buf = strconv.AppendInt(buf, int64(id1), 10)
		buf = append(buf, ',')
		buf = strconv.AppendInt(buf, int64(id2), 10)
		buf = append(buf, ',')
		buf = strconv.AppendInt(buf, int64(id3), 10)
		buf = append(buf, ',')
		buf = appendKey(buf, id1, false)
		buf = append(buf, ',')
		buf = appendKey(buf, id2, false)
		buf = append(buf, ',')
		buf = appendKey(buf, id3, false)
		buf = append(buf, ',')
		buf = appendFloat(buf, round6(r.Float64()*100), false)
		buf = append(buf, '\n')

		if _, err := w.Write(buf); err != nil {
			return fmt.Errorf("row %d: %w", i+1, err)
		}
	}
	return nil
}

// writeRight writes one right hand table. width is 1, 2 or 3 and selects how
// many key columns it carries, matching the small, medium and big tables.
func (j join) writeRight(w *bufio.Writer, size, width int) error {
	header := []string{"id1,id4,v2\n", "id1,id2,id4,id5,v2\n", "id1,id2,id3,id4,id5,id6,v2\n"}
	if _, err := w.WriteString(header[width-1]); err != nil {
		return err
	}

	// A separate stream per table, offset by the width, so that the small
	// table is not a prefix of the medium one.
	r := newRand(j.seed + uint64(width)*1013)
	small, medium, _ := j.sizes()
	buf := make([]byte, 0, 96)

	for key := 1; key <= size; key++ {
		if !matches(key) {
			continue
		}

		// The lower cardinality columns are filled by sampling, the way the
		// upstream tables do, so that a join on id2 of the big table is not
		// secretly a join on a unique column.
		keys := []int{key}
		switch width {
		case 2:
			keys = []int{r.IntN(small) + 1, key}
		case 3:
			keys = []int{r.IntN(small) + 1, r.IntN(medium) + 1, key}
		}

		buf = buf[:0]
		for _, k := range keys {
			buf = strconv.AppendInt(buf, int64(k), 10)
			buf = append(buf, ',')
		}
		for _, k := range keys {
			buf = appendKey(buf, k, false)
			buf = append(buf, ',')
		}
		buf = appendFloat(buf, round6(r.Float64()*100), false)
		buf = append(buf, '\n')

		if _, err := w.Write(buf); err != nil {
			return fmt.Errorf("key %d: %w", key, err)
		}
	}
	return nil
}

// appendKey writes a string key as id followed by the number, which is the
// upstream format. These never contain a comma or a quote, so they go out raw
// rather than through encoding/csv, which more than doubles the generation
// time for no benefit here.
func appendKey(b []byte, v int, blank bool) []byte {
	if blank {
		return b
	}
	b = append(b, 'i', 'd')
	return strconv.AppendInt(b, int64(v), 10)
}

func appendInt(b []byte, v int, blank bool) []byte {
	if blank {
		return b
	}
	return strconv.AppendInt(b, int64(v), 10)
}

func appendFloat(b []byte, v float64, blank bool) []byte {
	if blank {
		return b
	}
	return strconv.AppendFloat(b, v, 'f', -1, 64)
}

// round6 rounds to six decimal places, matching the upstream value columns.
// Six is enough that a sum over a hundred million of them still differs
// between libraries in the last few bits, which is why the checksums compare
// rounded results rather than exact ones.
func round6(v float64) float64 {
	return float64(int64(v*1e6+0.5)) / 1e6
}

// newRand returns a deterministic generator. PCG rather than the default
// source, because the default is seeded from the runtime and the whole
// contract of this command is that the same flags produce the same file.
func newRand(seed uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
}

// mix is splitmix64's finalizer. It turns a sequential key into something with
// no structure in its low bits, which is what makes taking every key whose
// remainder is not zero a fair nine in ten sample rather than a stride.
func mix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}
