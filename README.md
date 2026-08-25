# kuma-bench

Benchmarks for [kuma](https://github.com/tamnd/kuma), measured against pandas and Polars.

[![CI](https://github.com/tamnd/kuma-bench/actions/workflows/ci.yml/badge.svg)](https://github.com/tamnd/kuma-bench/actions/workflows/ci.yml)
[![Nightly](https://github.com/tamnd/kuma-bench/actions/workflows/nightly.yml/badge.svg)](https://github.com/tamnd/kuma-bench/actions/workflows/nightly.yml)

**Status: early.** The harness works end to end. The generator, the orchestrator, the pandas runner and the Polars runner are all done, and all fifteen db-benchmark queries run and produce identical answers in both libraries. What is missing is kuma, which cannot yet answer any of them. Every kuma row in the results says which milestone it is waiting on.

That ordering is deliberate and the reason is below.

## Why this is a separate repository

Comparing against pandas and Polars means installing pandas and Polars, and that must never become a condition of building kuma. The Go module over there depends on the standard library and nothing else. All the Python, the Docker images, the datasets and the result history live here instead.

There is a second reason. A benchmark repository has a different rhythm from a library. The library changes when someone writes code. The benchmarks need to re-run when pandas releases, when Polars releases, when a new machine type appears, and on a schedule regardless of whether anything changed. Tying those two cadences together makes both worse.

## The rule that makes this worth doing

Benchmarks that run once, at the end, before an announcement, are marketing. Benchmarks that run continuously, from the first milestone, are engineering. The difference is that the second kind tells you which commit made things slow while you still remember what you were doing.

So this harness exists from kuma's first milestone, when there is nothing to measure. Running it today gives a table with real pandas and Polars numbers and fifteen kuma rows saying which milestone each query is waiting on. That is a useful table: it means the first query kuma can answer gets a number the day it lands rather than the week somebody remembers to wire it up, and by the time the SIMD work is done there will be a year of history to check the claims against.

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

Yes, on all fifteen queries. Same row counts, same checksums, pandas against Polars:

```
groupby_q1   100      v1=299926
groupby_q2   10000    v1=299926
groupby_q3   1000     v1=299926 v3=50093.2
groupby_q4   100      v1=299.93 v2=798.2 v3=5009.22
groupby_q5   1000     v1=299926 v2=798224 v3=5.00917e+06
groupby_q6   63135    v3_median=3.16665e+06 v3_sd=644437
groupby_q7   1000     range_v1_v2=3996
groupby_q8   2000     v3=197041
groupby_q9   10000    r2=1308.75
groupby_q10  100000   v3=5.00917e+06 count=100000
join_q1      100000   v1=4.99212e+06 v2=6.12874e+06
join_q2      89810    v1=4.48623e+06 v2=4.79516e+06
join_q3      100000   v1=4.99212e+06 v2=4.79516e+06
join_q4      89810    v1=4.48623e+06 v2=4.79516e+06
join_q5      89946    v1=4.4895e+06 v2=4.4993e+06
```

Getting there took two fixes worth mentioning, because both are the kind of thing that quietly turns a benchmark into fiction. The checksum was being computed inside the timed section, which measured this harness rather than the query. And pandas skips missing values when it sums while Polars propagates a NaN through the whole column, so the correlation query disagreed for reasons that had nothing to do with the answer. Each runner now says explicitly how it sums, so what gets compared is the arithmetic rather than the convention.

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
go run ./cmd/kumabench -suite dbbench -libs kuma,pandas,polars -size 0.5GB -python .venv/bin/python
```

It writes JSONL to `results/` and prints a table. Add `-n` to see the exact commands it would run without running them. Add `-docker` to use the pinned images instead of whatever is on your machine, which is what you want if the numbers are going anywhere public.

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

## Sources

The suite definitions come from [duckdblabs/db-benchmark](https://github.com/duckdblabs/db-benchmark), published at [duckdblabs.github.io/db-benchmark](https://duckdblabs.github.io/db-benchmark). The old h2oai.github.io page is frozen at 2021 numbers and is not used for comparison here.

## License

Apache 2.0. See [LICENSE](LICENSE).
