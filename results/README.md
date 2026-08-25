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

## Reading them

```
go run ./cmd/report -in results/ -query groupby_q1
```

Or with anything that reads JSON Lines, which is most things. The format was chosen so that it would not need a tool.
