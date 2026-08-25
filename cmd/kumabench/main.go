// Command kumabench runs a benchmark suite and writes the results.
//
// It spawns one runner process per measurement, reads the JSON that runner
// prints, adds the context the runner does not have, and appends a record to a
// JSON Lines file under results/. Then it prints a table.
//
//	kumabench -suite dbbench -libs kuma,pandas,polars -size 0.5GB -runs 5
//
// A fresh process per measurement is the point rather than an implementation
// detail. Running all five repetitions inside one process measures a warm
// allocator and a warm import cache, and running two libraries in one process
// measures whichever one went second. Neither is what anybody wants to know.
//
// Nothing here has an opinion about how a query is implemented. The runners
// have that, one per library, and three of the four are not written in Go.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/tamnd/kuma-bench/bench"
	"github.com/tamnd/kuma-bench/suites/dbbench"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "kumabench: %v\n", err)
		os.Exit(1)
	}
}

type options struct {
	suite   string
	libs    []string
	queries []string
	rows    int
	size    string
	nas     int
	runs    int
	data    string
	out     string
	timeout time.Duration
	python  string
	docker  bool
	pyImage string
	goImage string
	runner  string
	commit  string
	dryRun  bool

	// repoRoot and dataRoot are absolute, worked out once at startup. The
	// runners need absolute paths because one of them runs from a
	// subdirectory and the others may run inside a container.
	repoRoot string
	dataRoot string
}

func run() error {
	var (
		suite   = flag.String("suite", "dbbench", "which suite to run")
		libs    = flag.String("libs", "kuma,pandas,polars", "comma separated libraries")
		queries = flag.String("queries", "", "comma separated query names, default all")
		size    = flag.String("size", "0.5GB", "dataset size: 0.5GB, 5GB or 50GB")
		rows    = flag.Int("rows", 0, "row count, overriding -size")
		nas     = flag.Int("nas", 0, "percentage of missing values in the dataset")
		runs    = flag.Int("runs", 5, "repetitions per query, the first being the cold run")
		data    = flag.String("data", "data", "directory holding the generated datasets")
		out     = flag.String("out", "results", "directory to append results to")
		timeout = flag.Duration("timeout", 30*time.Minute, "per measurement timeout")
		python  = flag.String("python", "python3", "python interpreter for the pandas and polars runners")
		docker  = flag.Bool("docker", false, "run each library in its pinned image")
		pyImage = flag.String("python-image", "kuma-bench-python", "image for the pandas and polars runners")
		goImage = flag.String("go-image", "kuma-bench-go", "image for the kuma runner")
		runner  = flag.String("runner", "", "label for the machine, for example bare-metal")
		commit  = flag.String("commit", "", "the kuma commit under test")
		dryRun  = flag.Bool("n", false, "print what would run without running it")
	)
	flag.Parse()

	if *suite != "dbbench" {
		return fmt.Errorf("unknown suite %q, only dbbench exists so far", *suite)
	}
	if *runs < 1 {
		return fmt.Errorf("-runs must be at least 1")
	}

	// The size label goes in every record and is what the reporting groups by,
	// so a run with -rows must not be filed under whatever -size happened to
	// default to. Different row counts are different benchmarks.
	var err error
	n, label := *rows, *size
	if n == 0 {
		if n, err = rowsForSize(*size); err != nil {
			return err
		}
	} else {
		label = fmt.Sprintf("%drows", n)
	}

	opts := options{
		suite: *suite, libs: split(*libs), queries: split(*queries),
		rows: n, size: label, nas: *nas, runs: *runs,
		data: *data, out: *out, timeout: *timeout, python: *python,
		docker: *docker, pyImage: *pyImage, goImage: *goImage,
		runner: *runner, commit: *commit, dryRun: *dryRun,
	}
	if opts.repoRoot, err = filepath.Abs("."); err != nil {
		return err
	}
	if opts.dataRoot, err = filepath.Abs(opts.data); err != nil {
		return err
	}
	if len(opts.libs) == 0 {
		return errors.New("-libs is empty")
	}
	for _, lib := range opts.libs {
		if _, ok := runners[lib]; !ok {
			return fmt.Errorf("unknown library %q, want kuma, pandas or polars", lib)
		}
	}

	selected, err := selectQueries(opts.queries)
	if err != nil {
		return err
	}
	if err := checkInputs(selected, opts); err != nil {
		return err
	}

	if opts.dryRun {
		return printPlan(selected, opts)
	}
	return measure(selected, opts)
}

// command is one runner invocation.
type command struct {
	argv []string
	// dir is where to run it, relative to the repository root. Only the kuma
	// runner needs this, because it lives in its own Go module and go run only
	// looks at the module it is standing in.
	dir string
}

func (c command) String() string {
	if c.dir == "" {
		return strings.Join(c.argv, " ")
	}
	return "(cd " + c.dir + " && " + strings.Join(c.argv, " ") + ")"
}

// runners maps a library name to the command that measures one query for it.
// The signature is deliberately narrow: given the options and the input paths,
// produce a command. Everything else about running a subprocess is the same
// for all of them, and keeping it that way is what stops one library from
// quietly getting a different deal than the others.
var runners = map[string]func(o options, query string, inputs []string) command{
	"kuma":   kumaRunner,
	"pandas": pythonRunner("pandas"),
	"polars": pythonRunner("polars"),
}

func kumaRunner(o options, query string, inputs []string) command {
	// The kuma runner is its own Go module, so go run has to be standing in
	// that directory. Anywhere else and it looks in the wrong module and says
	// the package does not exist.
	dir := filepath.Join("suites", o.suite, "kuma")
	if o.docker {
		argv := append(dockerPrefix(o, o.goImage, "/work/"+filepath.ToSlash(dir)),
			"go", "run", ".", "-query", query)
		return command{argv: appendFlags(argv, "-input", containerPaths(inputs))}
	}
	argv := []string{"go", "run", ".", "-query", query}
	return command{argv: appendFlags(argv, "-input", inputs), dir: dir}
}

func pythonRunner(lib string) func(options, string, []string) command {
	return func(o options, query string, inputs []string) command {
		script := filepath.Join("suites", o.suite, lib, "run.py")
		if o.docker {
			argv := append(dockerPrefix(o, o.pyImage, "/work"),
				"python", "/work/"+filepath.ToSlash(script), "--query", query)
			return command{argv: appendFlags(argv, "--input", containerPaths(inputs))}
		}
		argv := []string{o.python, script, "--query", query}
		return command{argv: appendFlags(argv, "--input", inputs)}
	}
}

// dockerPrefix mounts the repository read only at /work and the datasets read
// only at /data. Read only for both, because a runner that writes to the data
// directory has changed the input for whatever runs next, and that is a whole
// afternoon of confusion for no benefit.
func dockerPrefix(o options, image, workdir string) []string {
	return []string{
		"docker", "run", "--rm",
		"-v", o.repoRoot + ":/work:ro",
		"-v", o.dataRoot + ":/data:ro",
		"-w", workdir,
		image,
	}
}

// containerPaths rewrites host dataset paths to where they are mounted. The
// files are flat in one directory, so the base name is enough.
func containerPaths(inputs []string) []string {
	out := make([]string, len(inputs))
	for i, in := range inputs {
		out[i] = "/data/" + filepath.Base(in)
	}
	return out
}

func appendFlags(argv []string, flag string, values []string) []string {
	for _, v := range values {
		argv = append(argv, flag, v)
	}
	return argv
}

func measure(queries []dbbench.Query, o options) error {
	if err := os.MkdirAll(o.out, 0o755); err != nil {
		return err
	}
	path := filepath.Join(o.out, resultsFileName(o))
	w, err := bench.Create(path)
	if err != nil {
		return err
	}
	defer w.Close()

	machine := bench.LocalMachine()
	machine.Runner = o.runner

	var all []bench.Result
	for _, q := range queries {
		inputs, err := dbbench.Inputs(q, o.rows, o.nas)
		if err != nil {
			return err
		}
		for i, in := range inputs {
			inputs[i] = filepath.Join(o.dataRoot, in)
		}

		for _, lib := range o.libs {
			for i := 1; i <= o.runs; i++ {
				base := bench.Result{
					Suite: o.suite, Query: q.Name, Size: o.size,
					Library: lib, Run: i, InRows: int64(o.rows),
					Timestamp: time.Now().UTC(), Machine: machine, Commit: o.commit,
				}

				res := one(base, runners[lib](o, q.Name, inputs), o)
				if err := w.Write(res); err != nil {
					return err
				}
				all = append(all, res)
				report(res)
			}
		}
	}

	if err := w.Close(); err != nil {
		return err
	}
	fmt.Printf("\n%s\n%s\n", bench.Table(all), path)
	return nil
}

// one runs a single measurement. It never returns an error: a runner that
// crashes, hangs or prints nonsense produces a record with an error in it,
// because a suite that stops at the first failure never finishes a run and a
// suite that skips failures reports a misleading average.
func one(base bench.Result, run command, o options) bench.Result {
	ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, run.argv[0], run.argv[1:]...)
	cmd.Dir = filepath.Join(o.repoRoot, run.dir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	wall := time.Since(start)

	switch {
	case ctx.Err() != nil:
		// The elapsed time is recorded even though the query did not finish,
		// because "timed out after thirty minutes" and "timed out after four
		// hours" are different findings.
		base.Elapsed = bench.Duration(wall)
		base.Err = fmt.Sprintf("timed out after %s", o.timeout)
		return base
	case err != nil:
		// A runner that exits non-zero has crashed rather than reported a
		// failure, since reporting a failure is done by printing and exiting
		// zero. Its standard error is the only clue, so some of it goes in.
		base.Err = fmt.Sprintf("%v: %s", err, lastLine(stderr.String()))
		return base
	}

	rep, perr := bench.ParseReport(stdout.Bytes())
	if perr != nil {
		base.Err = fmt.Sprintf("%v: %s", perr, lastLine(stderr.String()))
		return base
	}
	res := rep.Result(base)
	if res.OK() && res.OutRows == 0 {
		// Every query in this suite groups or joins, and none of them can
		// legitimately return nothing. A zero row result means the runner
		// returned a plan it never evaluated, and that is exactly the mistake
		// this whole harness is arranged to catch.
		res.Err = "the query returned no rows, which means it was not evaluated"
	}
	return res
}

func report(r bench.Result) {
	status := "ok"
	if !r.OK() {
		status = r.Err
	}
	fmt.Printf("%-12s %-8s run %d  %8s  %9d rows  %s\n",
		r.Query, r.Library, r.Run, r.Elapsed, r.OutRows, status)
}

// checkInputs fails before running anything if a dataset is missing. Finding
// out forty minutes in that the join files were never generated is a bad way
// to spend forty minutes.
func checkInputs(queries []dbbench.Query, o options) error {
	seen := map[string]bool{}
	var missing []string
	for _, q := range queries {
		inputs, err := dbbench.Inputs(q, o.rows, o.nas)
		if err != nil {
			return err
		}
		for _, in := range inputs {
			path := filepath.Join(o.data, in)
			if seen[path] {
				continue
			}
			seen[path] = true
			if _, err := os.Stat(path); err != nil {
				missing = append(missing, in)
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("missing %d dataset files in %s, starting with %s\nrun: go run ./cmd/gendata -suite all -size %s -out %s",
		len(missing), o.data, missing[0], o.size, o.data)
}

func printPlan(queries []dbbench.Query, o options) error {
	total := 0
	for _, q := range queries {
		inputs, err := dbbench.Inputs(q, o.rows, o.nas)
		if err != nil {
			return err
		}
		for i, in := range inputs {
			inputs[i] = filepath.Join(o.dataRoot, in)
		}
		for _, lib := range o.libs {
			fmt.Println(runners[lib](o, q.Name, inputs))
			total += o.runs
		}
	}
	fmt.Fprintf(os.Stderr, "\n%d measurements, %d runs each\n", total, o.runs)
	return nil
}

func selectQueries(names []string) ([]dbbench.Query, error) {
	if len(names) == 0 {
		return dbbench.Queries, nil
	}
	var out []dbbench.Query
	for _, name := range names {
		q, ok := dbbench.Lookup(name)
		if !ok {
			return nil, fmt.Errorf("unknown query %q, want one of %s", name, strings.Join(dbbench.Names(), ", "))
		}
		out = append(out, q)
	}
	return out, nil
}

// resultsFileName encodes what the file contains, because a directory of files
// called results1 through results40 is not a history, it is a pile.
func resultsFileName(o options) string {
	stamp := time.Now().UTC().Format("20060102-150405")
	return fmt.Sprintf("%s-%s-%s-%s-%s.jsonl", o.suite, o.size, runtime.GOOS, runtime.GOARCH, stamp)
}

func rowsForSize(size string) (int, error) {
	switch size {
	case "0.5GB":
		return 10_000_000, nil
	case "5GB":
		return 100_000_000, nil
	case "50GB":
		return 1_000_000_000, nil
	}
	return 0, fmt.Errorf("unknown size %q, want 0.5GB, 5GB or 50GB, or pass -rows", size)
}

func split(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// lastLine returns the final non empty line, which for a Python traceback is
// the exception and for most other things is the message.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return "no output"
}
