package bench_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/kuma-bench/bench"
)

func TestDurationRoundTrip(t *testing.T) {
	for _, want := range []time.Duration{
		0,
		time.Millisecond,
		1500 * time.Millisecond,
		42 * time.Second,
		3 * time.Minute,
	} {
		b, err := json.Marshal(bench.Duration(want))
		if err != nil {
			t.Fatalf("Marshal(%v): %v", want, err)
		}

		var got bench.Duration
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("Unmarshal(%s): %v", b, err)
		}

		// Fractional seconds through a float64 is lossy below the microsecond,
		// which is fine for a benchmark and would not be fine for anything
		// else. Assert the tolerance rather than pretending it is exact.
		if diff := time.Duration(got) - want; diff > time.Microsecond || diff < -time.Microsecond {
			t.Errorf("round trip of %v gave %v", want, time.Duration(got))
		}
	}
}

func TestDurationMarshalsAsSeconds(t *testing.T) {
	b, err := json.Marshal(bench.Duration(1500 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != "1.5" {
		t.Errorf("Marshal = %s, want 1.5", got)
	}
}

func TestDurationRejectsNonNumber(t *testing.T) {
	var d bench.Duration
	if err := json.Unmarshal([]byte(`"1.5s"`), &d); err == nil {
		t.Error("accepted a Go duration string, want an error")
	}
}

func TestReadAll(t *testing.T) {
	in := `{"suite":"dbbench","query":"q1","library":"kuma","run":1,"elapsed":0.5,"out_rows":100}
{"suite":"dbbench","query":"q1","library":"kuma","run":2,"elapsed":0.25,"out_rows":100}
`
	got, err := bench.ReadAll(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	if got[1].Elapsed != bench.Duration(250*time.Millisecond) {
		t.Errorf("second elapsed = %v, want 250ms", got[1].Elapsed)
	}
}

func TestReadAllReportsTheBadLine(t *testing.T) {
	in := `{"query":"q1","elapsed":0.5}
not json
`
	_, err := bench.ReadAll(strings.NewReader(in))
	if err == nil {
		t.Fatal("want an error")
	}
	// The line number is the whole value of the message. Without it, finding a
	// bad record in a hundred thousand line file is a bisect.
	if !strings.Contains(err.Error(), "result 2") {
		t.Errorf("error does not say which record failed: %v", err)
	}
}

func TestMedian(t *testing.T) {
	tests := []struct {
		name    string
		results []bench.Result
		want    time.Duration
	}{
		{
			name: "drops the cold run",
			results: []bench.Result{
				{Run: 1, Elapsed: bench.Duration(10 * time.Second)},
				{Run: 2, Elapsed: bench.Duration(2 * time.Second)},
				{Run: 3, Elapsed: bench.Duration(1 * time.Second)},
				{Run: 4, Elapsed: bench.Duration(3 * time.Second)},
			},
			want: 2 * time.Second,
		},
		{
			name: "averages the middle two when even",
			results: []bench.Result{
				{Run: 2, Elapsed: bench.Duration(1 * time.Second)},
				{Run: 3, Elapsed: bench.Duration(3 * time.Second)},
			},
			want: 2 * time.Second,
		},
		{
			name: "ignores failed runs",
			results: []bench.Result{
				{Run: 2, Elapsed: bench.Duration(1 * time.Second)},
				{Run: 3, Elapsed: bench.Duration(99 * time.Second), Err: "out of memory"},
				{Run: 4, Elapsed: bench.Duration(3 * time.Second)},
			},
			want: 2 * time.Second,
		},
		{
			name:    "nothing to average",
			results: []bench.Result{{Run: 1, Elapsed: bench.Duration(time.Second)}},
			want:    0,
		},
		{
			name:    "empty",
			results: nil,
			want:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bench.Median(tt.results); time.Duration(got) != tt.want {
				t.Errorf("Median = %v, want %v", time.Duration(got), tt.want)
			}
		})
	}
}

func TestResultOK(t *testing.T) {
	if !(bench.Result{}).OK() {
		t.Error("a result with no error should be OK")
	}
	if (bench.Result{Err: "boom"}).OK() {
		t.Error("a result with an error should not be OK")
	}
}

func TestWriterRoundTrip(t *testing.T) {
	path := t.TempDir() + "/results.jsonl"

	w, err := bench.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	want := bench.Result{
		Suite: "dbbench", Query: "groupby_q1", Library: "kuma",
		Run: 1, Elapsed: bench.Duration(time.Second), OutRows: 100,
		Machine: bench.LocalMachine(),
	}
	if err := w.Write(want); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	got, err := bench.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
	if got[0].Query != want.Query || got[0].Elapsed != want.Elapsed {
		t.Errorf("got %+v, want %+v", got[0], want)
	}
	if got[0].Machine.CPUs == 0 {
		t.Error("machine information did not survive the round trip")
	}
}
