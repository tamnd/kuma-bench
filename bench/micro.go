package bench

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Micro is one Go microbenchmark, run once.
//
// It is a different record from Result on purpose. A suite query has an input
// size, a result cardinality and a checksum that three libraries have to agree
// on. A microbenchmark has none of those and has three numbers that a query
// does not: time per operation, bytes allocated per operation and allocation
// count per operation. Squeezing one into the other would mean half the fields
// are empty in every row and the two would still not be comparable.
//
// The reason these are collected at all, when they already run on every pull
// request in the kuma repository, is that CI runs on a shared cloud machine.
// That is the right place to catch a benchmark that got twice as slow and the
// wrong place to learn what the code actually does on hardware somebody owns.
type Micro struct {
	// Package is the import path the benchmark lives in.
	Package string `json:"package"`
	// Name is the benchmark name with the GOMAXPROCS suffix removed, so that
	// the same benchmark run at two different parallelisms sorts together.
	Name string `json:"name"`
	// Procs is the GOMAXPROCS the benchmark ran at, taken from the suffix Go
	// prints after the name.
	Procs int `json:"procs"`

	// Run is the repetition index, starting at 1, matching Result.Run. Go's
	// own -count flag produces these and they are kept separate rather than
	// averaged, because the spread between repetitions is the thing that says
	// whether a difference between two commits means anything.
	Run int `json:"run"`
	// Iterations is Go's b.N for the repetition that was reported.
	Iterations int64 `json:"iterations"`

	// NsPerOp is time per operation in nanoseconds. It is kept in the unit Go
	// printed rather than converted to a Duration, because these are routinely
	// fractions of a nanosecond and a duration of zero is not a useful record
	// of a one third of a nanosecond operation.
	NsPerOp float64 `json:"ns_per_op"`
	// BytesPerOp and AllocsPerOp come from -benchmem. A kernel that got faster
	// by allocating more has not necessarily got faster.
	BytesPerOp  int64 `json:"bytes_per_op"`
	AllocsPerOp int64 `json:"allocs_per_op"`

	// Extra holds any other unit the benchmark reported through
	// b.ReportMetric, keyed by the unit string, for example "MB/s" or
	// "rows/op". Nothing here interprets them, they are carried through so
	// that a benchmark can report the number that actually matters to it.
	Extra map[string]float64 `json:"extra,omitempty"`

	// Timestamp is when the run finished, in UTC.
	Timestamp time.Time `json:"timestamp"`
	// Machine describes what it ran on.
	Machine Machine `json:"machine"`
	// Commit is the kuma commit that was benchmarked.
	Commit string `json:"commit,omitempty"`
}

// ParseBench reads the output of go test -bench and returns one Micro per
// benchmark line.
//
// It reads the header lines Go prints before each package, meaning goos,
// goarch, pkg and cpu, and fills in the fields they cover. Anything the caller
// passes in a Machine afterwards wins over those, since the caller knows
// things the test binary cannot, such as which host it was and how much memory
// it has.
//
// Lines it does not recognize are skipped rather than rejected. Benchmark
// output is mixed in with build output, test logs and whatever the code under
// test printed, and a parser that gave up on the first unfamiliar line would
// be useless in practice.
func ParseBench(r io.Reader) ([]Micro, error) {
	var (
		out  []Micro
		pkg  string
		mach Machine
		// counts tracks how many times each benchmark has been seen, which is
		// what turns go test -count=5 into five records with a Run each.
		counts = map[string]int{}
	)

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()

		if key, value, ok := header(line); ok {
			switch key {
			case "goos":
				mach.OS = value
			case "goarch":
				mach.Arch = value
			case "cpu":
				mach.CPUModel = value
			case "pkg":
				pkg = value
			}
			continue
		}

		m, ok := parseBenchLine(line)
		if !ok {
			continue
		}
		m.Package = pkg
		m.Machine = mach

		key := pkg + "." + m.Name + "-" + strconv.Itoa(m.Procs)
		counts[key]++
		m.Run = counts[key]

		out = append(out, m)
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("bench: read benchmark output: %w", err)
	}
	return out, nil
}

// header matches the "key: value" lines go test prints ahead of each package.
func header(line string) (key, value string, ok bool) {
	for _, k := range []string{"goos", "goarch", "pkg", "cpu"} {
		if rest, found := strings.CutPrefix(line, k+": "); found {
			return k, strings.TrimSpace(rest), true
		}
	}
	return "", "", false
}

// parseBenchLine parses one result line, which Go writes as the name, the
// iteration count, and then a value and a unit for every metric.
func parseBenchLine(line string) (Micro, bool) {
	fields := strings.Fields(line)
	// A name, a count, and at least one value and unit.
	if len(fields) < 4 || !strings.HasPrefix(fields[0], "Benchmark") {
		return Micro{}, false
	}
	// The metrics come in pairs, so an odd number of fields after the count
	// means this is not a result line. A benchmark that failed prints its name
	// and then a message, and that message is not a measurement.
	if (len(fields)-2)%2 != 0 {
		return Micro{}, false
	}

	iterations, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return Micro{}, false
	}

	name, procs := splitProcs(fields[0])
	m := Micro{Name: name, Procs: procs, Iterations: iterations}

	for i := 2; i < len(fields); i += 2 {
		value, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return Micro{}, false
		}
		switch unit := fields[i+1]; unit {
		case "ns/op":
			m.NsPerOp = value
		case "B/op":
			m.BytesPerOp = int64(value)
		case "allocs/op":
			m.AllocsPerOp = int64(value)
		default:
			if m.Extra == nil {
				m.Extra = map[string]float64{}
			}
			m.Extra[unit] = value
		}
	}
	return m, true
}

// splitProcs takes the GOMAXPROCS suffix off a benchmark name. Go writes
// BenchmarkAnd-8, and the 8 is a separate fact from the name.
func splitProcs(name string) (string, int) {
	i := strings.LastIndex(name, "-")
	if i < 0 {
		return name, 1
	}
	procs, err := strconv.Atoi(name[i+1:])
	if err != nil {
		// A benchmark is allowed to have a dash in its name, and a subtest
		// name after a slash routinely does.
		return name, 1
	}
	return name[:i], procs
}

// WriteMicro appends every record to path as JSON Lines, creating the file if
// it is not there.
func WriteMicro(path string, records []Micro) error {
	w, err := Create(path)
	if err != nil {
		return err
	}
	if err := EncodeMicro(w.w, records); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

// EncodeMicro writes records to w as JSON Lines.
func EncodeMicro(w io.Writer, records []Micro) error {
	enc := json.NewEncoder(w)
	for _, m := range records {
		if err := enc.Encode(m); err != nil {
			return fmt.Errorf("bench: encode microbenchmark: %w", err)
		}
	}
	return nil
}

// ReadMicro reads every record from r.
func ReadMicro(r io.Reader) ([]Micro, error) {
	var out []Micro
	dec := json.NewDecoder(r)
	for {
		var m Micro
		err := dec.Decode(&m)
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, fmt.Errorf("bench: decode microbenchmark %d: %w", len(out)+1, err)
		}
		out = append(out, m)
	}
}
