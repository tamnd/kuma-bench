package bench_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/kuma-bench/bench"
)

const benchOutput = `goos: linux
goarch: amd64
pkg: github.com/tamnd/kuma/bitmap
cpu: AMD Ryzen 9 5950X 16-Core Processor
BenchmarkCountOnes-8   	 1543210	       778.4 ns/op	       0 B/op	       0 allocs/op
BenchmarkAnd-8         	 2000000	       612.1 ns/op	       0 B/op	       0 allocs/op
BenchmarkCountOnes-8   	 1500000	       781.0 ns/op	       0 B/op	       0 allocs/op
PASS
ok  	github.com/tamnd/kuma/bitmap	3.214s
goos: linux
goarch: amd64
pkg: github.com/tamnd/kuma/dtype
cpu: AMD Ryzen 9 5950X 16-Core Processor
BenchmarkCoerce-8      	10000000	        11.23 ns/op	      16 B/op	       1 allocs/op
PASS
ok  	github.com/tamnd/kuma/dtype	1.102s
`

func TestParseBench(t *testing.T) {
	got, err := bench.ParseBench(strings.NewReader(benchOutput))
	if err != nil {
		t.Fatalf("ParseBench: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d records, want 4", len(got))
	}

	first := got[0]
	if first.Name != "BenchmarkCountOnes" || first.Procs != 8 {
		t.Errorf("name and procs are %q and %d", first.Name, first.Procs)
	}
	if first.Package != "github.com/tamnd/kuma/bitmap" {
		t.Errorf("package is %q", first.Package)
	}
	if first.Iterations != 1543210 || first.NsPerOp != 778.4 {
		t.Errorf("got %d iterations at %v ns/op", first.Iterations, first.NsPerOp)
	}
	if first.Run != 1 {
		t.Errorf("first run is %d", first.Run)
	}
	if first.Machine.OS != "linux" || first.Machine.Arch != "amd64" {
		t.Errorf("machine is %s/%s", first.Machine.OS, first.Machine.Arch)
	}
	if first.Machine.CPUModel != "AMD Ryzen 9 5950X 16-Core Processor" {
		t.Errorf("cpu model is %q", first.Machine.CPUModel)
	}

	// The second sighting of a benchmark is the second repetition, and the
	// repetitions are kept apart rather than averaged, because the spread
	// between them is what says whether a difference means anything.
	if third := got[2]; third.Name != "BenchmarkCountOnes" || third.Run != 2 {
		t.Errorf("third record is %s run %d, want BenchmarkCountOnes run 2", third.Name, third.Run)
	}
	if second := got[1]; second.Run != 1 {
		t.Errorf("BenchmarkAnd is run %d, want 1", second.Run)
	}

	last := got[3]
	if last.Package != "github.com/tamnd/kuma/dtype" {
		t.Errorf("the package header did not carry over, got %q", last.Package)
	}
	if last.BytesPerOp != 16 || last.AllocsPerOp != 1 {
		t.Errorf("got %d B/op and %d allocs/op", last.BytesPerOp, last.AllocsPerOp)
	}
}

func TestParseBenchSkipsNoise(t *testing.T) {
	// Real output has build lines, log output from the code under test, and
	// failures mixed in with the measurements. A parser that gave up on the
	// first line it did not recognize would never survive a real run.
	noisy := `# github.com/tamnd/kuma/bitmap
some package printed this
BenchmarkBroken-8      	       1	failed to allocate
--- FAIL: TestSomething (0.00s)
BenchmarkFine-4        	  100000	      1234 ns/op
FAIL
exit status 1
`
	got, err := bench.ParseBench(strings.NewReader(noisy))
	if err != nil {
		t.Fatalf("ParseBench: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d records, want only BenchmarkFine", len(got))
	}
	if got[0].Name != "BenchmarkFine" || got[0].Procs != 4 {
		t.Errorf("got %s at %d procs", got[0].Name, got[0].Procs)
	}
	if got[0].BytesPerOp != 0 {
		t.Errorf("a run without -benchmem reported %d B/op", got[0].BytesPerOp)
	}
}

func TestParseBenchCustomMetrics(t *testing.T) {
	// A benchmark that calls b.ReportMetric is reporting the number that
	// actually matters to it, so the unit is carried through rather than
	// dropped for not being one of the three Go prints by default.
	line := "BenchmarkScan/1e6-16   	    3000	    412000 ns/op	  2426.21 MB/s	   1000000 rows/op\n"

	got, err := bench.ParseBench(strings.NewReader(line))
	if err != nil {
		t.Fatalf("ParseBench: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1", len(got))
	}
	if got[0].Name != "BenchmarkScan/1e6" {
		t.Errorf("name is %q, want the subtest kept and the procs suffix removed", got[0].Name)
	}
	if got[0].Extra["MB/s"] != 2426.21 || got[0].Extra["rows/op"] != 1000000 {
		t.Errorf("extra metrics are %v", got[0].Extra)
	}
}

func TestParseBenchNameWithDash(t *testing.T) {
	// A benchmark is allowed a dash in its name, so the suffix is only a
	// GOMAXPROCS count when what follows the dash is a number.
	line := "BenchmarkRound-Trip   	  100000	      1234 ns/op\n"

	got, err := bench.ParseBench(strings.NewReader(line))
	if err != nil {
		t.Fatalf("ParseBench: %v", err)
	}
	if len(got) != 1 || got[0].Name != "BenchmarkRound-Trip" || got[0].Procs != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestParseBenchEmpty(t *testing.T) {
	got, err := bench.ParseBench(strings.NewReader(""))
	if err != nil {
		t.Fatalf("ParseBench: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d records from nothing", len(got))
	}
}

func TestMicroRoundTrip(t *testing.T) {
	want, err := bench.ParseBench(strings.NewReader(benchOutput))
	if err != nil {
		t.Fatalf("ParseBench: %v", err)
	}

	path := filepath.Join(t.TempDir(), "micro.jsonl")
	if err := bench.WriteMicro(path, want); err != nil {
		t.Fatalf("WriteMicro: %v", err)
	}

	var buf bytes.Buffer
	if err := bench.EncodeMicro(&buf, want); err != nil {
		t.Fatalf("EncodeMicro: %v", err)
	}
	got, err := bench.ReadMicro(&buf)
	if err != nil {
		t.Fatalf("ReadMicro: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("read %d records, wrote %d", len(got), len(want))
	}
	for i := range got {
		if got[i].Name != want[i].Name || got[i].NsPerOp != want[i].NsPerOp {
			t.Errorf("record %d came back as %s at %v ns/op, want %s at %v",
				i, got[i].Name, got[i].NsPerOp, want[i].Name, want[i].NsPerOp)
		}
	}
}

func TestReadMicroRejectsGarbage(t *testing.T) {
	_, err := bench.ReadMicro(strings.NewReader("{not json}\n"))
	if err == nil {
		t.Fatal("ReadMicro accepted a line that is not JSON")
	}
	if !strings.Contains(err.Error(), "bench:") {
		t.Errorf("error %q does not say where it came from", err)
	}
}
