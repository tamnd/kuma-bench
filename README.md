# kuma-bench

Benchmarks for [kuma](https://github.com/tamnd/kuma), measured against pandas and Polars.

[![CI](https://github.com/tamnd/kuma-bench/actions/workflows/ci.yml/badge.svg)](https://github.com/tamnd/kuma-bench/actions/workflows/ci.yml)
[![Nightly](https://github.com/tamnd/kuma-bench/actions/workflows/nightly.yml/badge.svg)](https://github.com/tamnd/kuma-bench/actions/workflows/nightly.yml)

**Status: early, and kuma is in the table now.** The generator, the orchestrator and all three runners are done. Thirteen of the fifteen db-benchmark queries run on kuma and give the same answers as pandas and Polars at 0.5 GB, ten million rows, on real hardware. The two that do not are waiting on aggregates kuma has not got yet, and they say so in the results rather than going missing. kuma is also three to eight times slower than both libraries today, which is written up below with the numbers, because that is the part a benchmark repository exists to be honest about.

That ordering, harness first and library second, is deliberate and the reason is below.

## Why this is a separate repository

Comparing against pandas and Polars means installing pandas and Polars, and that must never become a condition of building kuma. The Go module over there depends on the standard library and nothing else. All the Python, the Docker images, the datasets and the result history live here instead.

There is a second reason. A benchmark repository has a different rhythm from a library. The library changes when someone writes code. The benchmarks need to re-run when pandas releases, when Polars releases, when a new machine type appears, and on a schedule regardless of whether anything changed. Tying those two cadences together makes both worse.

## The rule that makes this worth doing

Benchmarks that run once, at the end, before an announcement, are marketing. Benchmarks that run continuously, from the first milestone, are engineering. The difference is that the second kind tells you which commit made things slow while you still remember what you were doing.

So this harness existed from kuma's first milestone, when there was nothing to measure and every kuma row in the table said which milestone it was waiting on. That was a useful table even then, and it paid for itself the day the first query landed: it got a number that day rather than the week somebody remembered to wire it up, and by the time the SIMD work is done there will be a year of history to check the claims against.

The second rule: **we publish the results we lose.** A benchmark suite that only shows wins is not information, and anyone experienced reads it as an advertisement and discounts everything in it including the true parts. Where pandas or Polars is faster the number goes in the table with a note about why.

## What gets measured

**db-benchmark.** Ten group by queries and five join queries, from [duckdblabs/db-benchmark](https://github.com/duckdblabs/db-benchmark), which is the maintained version of the old H2O.ai suite. Run at 0.5 GB and 5 GB in CI, and at 50 GB on demand. This is the suite people will compare us against whether we run it or not, so we run it ourselves before anyone else does.

**TPC-H.** Twenty two queries at scale factor 1 and 10. Where db-benchmark stresses group by and join in isolation, TPC-H stresses the optimizer, because the wins there come from projection pushdown, predicate pushdown, join ordering and partition pruning rather than from kernel speed. It is the suite that catches optimizer regressions and it is the one that will hurt early.

**Ingestion.** CSV, Parquet and NDJSON read time at several sizes and column mixes. Reported separately from query time, because a suite that bundles them lets a slow parser hide behind a fast engine.

**Memory.** Peak resident set for every query, alongside wall time. An engine that is twice as fast and uses five times the memory has not necessarily won, and the only way anyone finds that out is if it is in the table.

**Go microbenchmarks.** These live in the kuma repository and run on every pull request there, because they must not require a Python toolchain and they must fail the build the day a kernel gets twice as slow. What lives here is the record of running them on real machines. CI runs on a shared cloud runner, which is the right place to catch a regression and the wrong place to learn what the code actually does on hardware, so `scripts/micro.sh` runs the same benchmarks over ssh on machines we own and files the results alongside everything else. One JSONL file per host per run, with the CPU, the core count, the memory, the Go version and the load average the machine was already under.

## Making the comparison honest

This is most of the work. It is easy to produce a benchmark that is wrong in your own favour without meaning to.

**Force materialization.** A lazy engine that returns a plan has done nothing. Every timed query ends in something that requires the full result to exist. This is the single most common way dataframe benchmarks lie and it is usually an accident rather than a fraud.

**Time the whole thing.** Reading the input is included. Excluding IO measures a scenario nobody is in.

**Same data on disk.** One generator produces the files once and all three libraries read the identical bytes. Not one file per library, not a regenerated file per run.

**Warm and cold both.** The first run and the median of subsequent runs are reported separately. The first run includes page cache misses and lazy initialization, and it is the one users actually feel.

**Configure the competition properly.** Polars gets its lazy API and its streaming engine. pandas gets the Arrow dtype backend and the PyArrow engine readers, because pandas 3.0 defaults to Arrow backed strings and benchmarking it against the old object path would be a straw man. If a competitor has a tuning knob that helps, it goes on, and the fact that it went on is recorded.

**Pin everything.** Library versions, generator seed, CPU model, core count, memory, kernel version, Go version, and whether `GOEXPERIMENT=simd` was set. Results without that context are not reproducible and therefore not evidence.

**Same machine, same run.** All three libraries in one job on one machine. Numbers from different runs on different runners are not comparable no matter how similar the specs look. The reporting tool refuses to build a table out of results from two different machines rather than trusting anyone to remember.

**Check the answers.** Every library computes a checksum of its result, and CI fails if two of them disagree. A fast wrong answer is not a benchmark result, and this is the only way to notice.

## Does it actually agree

Yes. Same row counts and same checksums for every query all three libraries can answer, at ten million rows, on both machines below:

```
groupby_q1   100        v1=2.99955e+07
groupby_q2   10000      v1=2.99955e+07
groupby_q3   100000     v1=2.99955e+07 v3=5.0013e+06
groupby_q4   100        v1=299.955 v2=799.88 v3=5001.34
groupby_q5   100000     v1=2.99955e+07 v2=7.9988e+07 v3=5.00135e+08
groupby_q6   6319501    v3_median=3.16073e+08 v3_sd=6.46818e+07
groupby_q7   100000     range_v1_v2=399856
groupby_q8   200000     v3=1.97008e+07                     pandas and Polars only
groupby_q9   10000      r2=10.0628                         pandas and Polars only
groupby_q10  10000000   v3=5.00135e+08 count=1e+07
join_q1      9000695    v1=4.50194e+08 v2=4.1384e+08
join_q2      8997488    v1=4.50051e+08 v2=4.48625e+08
join_q3      10000000   v1=5.00193e+08 v2=4.48625e+08
join_q4      8997488    v1=4.50051e+08 v2=4.48625e+08
join_q5      9001850    v1=4.50243e+08 v2=4.50311e+08
```

Getting there took three fixes worth mentioning, because all of them are the kind of thing that quietly turns a benchmark into fiction. The checksum was being computed inside the timed section, which measured this harness rather than the query. pandas skips missing values when it sums while Polars propagates a NaN through the whole column, so the correlation query disagreed for reasons that had nothing to do with the answer, and each runner now says explicitly how it sums. And a row whose key is missing has to be a group of its own in all three, which pandas only does when the runner asks for it, or the row counts drift apart on every query and the timings stop comparing the same work.

## Where kuma stands today

Slower than both, everywhere. The 0.5 GB suite, ten million rows, on an idle i9-13900K, three runs a query, showing the median of the warm ones. The cold run is left out of the table and kept in the results file, where it can be read on its own:

| query | kuma | pandas | polars |
| --- | ---: | ---: | ---: |
| groupby_q1 | 3.13s | **407ms** | 485ms |
| groupby_q2 | 3.75s | 806ms | **656ms** |
| groupby_q3 | 4.17s | 850ms | **629ms** |
| groupby_q4 | 3.89s | 513ms | **460ms** |
| groupby_q5 | 4.22s | 597ms | **539ms** |
| groupby_q6 | 8.23s | 3.97s | **1.11s** |
| groupby_q7 | 5.22s | 761ms | **658ms** |
| groupby_q8 | waiting on M4 | 3.08s | **703ms** |
| groupby_q9 | waiting on M4 | 6.27s | **752ms** |
| groupby_q10 | 11.1s | 5.91s | **1.82s** |
| join_q1 | 3.51s | 868ms | **572ms** |
| join_q2 | 3.78s | 998ms | **785ms** |
| join_q3 | 3.82s | 822ms | **537ms** |
| join_q4 | 4.05s | 2.04s | **662ms** |
| join_q5 | 14.4s | 2.74s | **1.07s** |

This is the expected shape and not a surprise. kuma is eager, its kernels are scalar, it runs the whole query on the goroutine that asked for it, and most of what you see above is the CSV reader rather than the group by. pandas reads the same files through PyArrow, which is C++ and threaded, and Polars reads them through its own threaded reader and then runs a query plan across every core. Milestones M2 through M6 are the ones that close this, and the reason to publish the table now is that a starting point you did not write down is not a starting point.

Two rows are worth looking at on their own. groupby_q10 groups by all six keys at once, which is nearly as many groups as rows, and it is where the hash table shows up in the number. join_q5 joins two ten million row tables and is four times worse than any other join, which says the build side is the problem rather than the probe.

The same run on a shared eight core EPYC that was already at load 5.9 is in `results/` as well. Its absolute numbers mean very little, which is exactly why the load average goes in every record, but the ordering is the same and the same two queries stand out, so what the table above shows is the code rather than the box.

## Layout

```
bench/          the result record and the runner protocol, shared by everything
cmd/gendata     deterministic dataset generation
cmd/kumabench   the orchestrator: runs each library, collects results
cmd/report      reads the committed results, prints tables and history
suites/
  dbbench/      the fifteen queries, one runner per library
    kuma/       its own Go module, so a kuma API break cannot stop the build
  tpch/         the twenty two queries, arriving with kuma's optimizer
data/           generated, not committed
results/        committed JSONL, one file per run, this is the point
site/           static site generated from results
docker/         pinned images, by digest
```

The Go module here depends on the standard library and nothing else. The one exception is `suites/dbbench/kuma`, which is a separate module precisely so that its dependency on kuma stays contained: when kuma's API breaks, that directory breaks and nothing else does.

Each library runs in a fresh process per query, so that one query cannot warm a cache for the next, and with `-docker` in its own pinned image. One JSON record per query per library per run, and those records are committed.

## Running it

Generate the data once:

```
go run ./cmd/gendata -suite all -size 0.5GB -out data
```

Set up the Python side, which is pinned by `uv.lock`:

```
uv sync
```

Then run a suite:

```
go run ./cmd/kumabench -suite dbbench -libs kuma,pandas,polars -size 0.5GB \
    -python .venv/bin/python -host server3 -cpu "$(grep -m1 'model name' /proc/cpuinfo | cut -d: -f2- | xargs)" \
    -memory-gb "$(free -g | awk '/^Mem:/ {print $2}')" -load "$(cut -d' ' -f1 /proc/loadavg)"
```

It writes JSONL to `results/` and prints a table. The machine flags are that long winded on purpose: a result that cannot name the box it ran on cannot be compared with anything later, so `-host` is required and the rest travel with every record. Add `-n` to see the exact commands it would run without running them. Add `-docker` to use the pinned images instead of whatever is on your machine, which is what you want if the numbers are going anywhere public.

Reading the results afterwards:

```
go run ./cmd/report                                   the latest run
go run ./cmd/report -query groupby_q1 -history        that query over time
```

## Schedule

Nightly against kuma's main branch. On every kuma tagged release. On demand for a branch. And automatically when a new pandas or Polars version appears, because a comparison against a version nobody runs is not a comparison.

The small sizes run on GitHub Actions. The large ones run on a dedicated machine, because shared cloud runners have noisy neighbours and a 50 GB benchmark on a noisy machine produces numbers that are worse than no numbers.

## What we expect to find

Written down in advance so it cannot be revised after seeing the results.

We should beat pandas on nearly everything by a wide margin, because pandas is single threaded and eager. If we do not beat pandas on a query, that is a bug rather than a benchmark result.

We should be within roughly 2x of Polars on group by and join. Polars has years of tuning and a mature streaming engine. Being close is a good outcome, and being faster on specific queries is plausible where StringView helps.

We will probably lose to Polars on TPC-H early, because those queries reward optimizer maturity and ours will be new.

We should be able to win on string heavy work, because StringView by default with inline prefix comparison is a real structural advantage.

We will lose on ecosystem, forever, and no benchmark measures that. The reasons to use kuma are compile time safety, a single binary, real cancellation and no Python runtime, and none of those show up in a wall clock number. These benchmarks exist to show that choosing those things does not cost speed, not to claim that speed is the reason.

None of that has happened yet. Today kuma loses to both libraries on every query it can answer, which is what a first implementation with scalar kernels and an eager engine looks like. These predictions get judged when the work they describe is done, and they stay written exactly as they were until then.

## Sources

The suite definitions come from [duckdblabs/db-benchmark](https://github.com/duckdblabs/db-benchmark), published at [duckdblabs.github.io/db-benchmark](https://duckdblabs.github.io/db-benchmark). The old h2oai.github.io page is frozen at 2021 numbers and is not used for comparison here.

## License

Apache 2.0. See [LICENSE](LICENSE).
