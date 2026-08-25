# Results

Every measurement this repository has ever taken, one JSON object per line, one file per run.

These files are committed. That is the whole point of the repository. A single table showing kuma against pandas and Polars on one afternoon is an advertisement. Eighteen months of the same table, with the commit that moved each number attached, is engineering.

## The file name

```
dbbench-0.5GB-linux-amd64-20260825-031500.jsonl
```

Suite, size, operating system, architecture, and the UTC time the run started. A run is never overwritten and never edited. If a run was wrong, the fix is another run and a note, not a deletion.

## The record

One object per query, per library, per repetition. The full field list is documented in `bench/result.go`, which is the only definition of it.

The parts worth knowing without reading that file:

`run` starts at 1, and run 1 is the cold run. It includes page cache misses and lazy initialization and it is reported separately, because it is the one users actually feel.

`error` being set is a normal outcome, not a corrupt record. A library that cannot do a query, or ran out of memory trying, produces a record saying so. A suite that silently dropped those would report an average over the queries a library happens to be good at.

`checksum` is a digest of the result. All three libraries compute it the same way, so a disagreement means someone got a different answer. A fast wrong answer is not a benchmark result.

`machine` travels with every record rather than sitting in a header. Results from different machines are not comparable, and keeping the machine attached to the row is what stops them from being combined by accident later.

## The microbenchmark files

```
micro-server3-linux-amd64-20260825-125035.jsonl
```

Same idea, different record. These come from `go test -bench` in the kuma repository, run over ssh by `scripts/micro.sh`, and the field list is in `bench/micro.go`. A suite query has an input size, a result cardinality and a checksum that three libraries have to agree on. A microbenchmark has none of those and has three numbers a query does not, meaning time per operation, bytes allocated per operation and allocations per operation, so it gets its own record rather than half filling the other one.

The repetitions from `go test -count` are kept as separate records instead of being averaged on the way in. The spread between them is the thing that says whether a difference between two commits means anything, and an average throws exactly that away.

## The machines

`gamingpc` is a desktop, an i9-13900K with 32 threads and 64 GB, and it is idle when it runs. It is the one to read if you want to know how fast something is.

`server1` and `server3` are working VPS boxes that were bought to do a job and are doing it. Both were above a load average of fifty when they last ran. Their timings are recorded anyway, with the load average in the record, because a benchmark on a busy machine is still evidence about variance and about how the code behaves when it is not given a whole core, and because deleting the inconvenient runs is how a benchmark suite turns into an advertisement. Do not read absolute numbers off them.

`server2` is out of disk, so it is not in the set. It will be when that is fixed.

Nothing here uses the GPU. kuma is a CPU engine and the graphics card in the desktop is not part of the story.

## Reading them

```
go run ./cmd/report -in results/ -query groupby_q1
```

Or with anything that reads JSON Lines, which is most things. The format was chosen so that it would not need a tool.
