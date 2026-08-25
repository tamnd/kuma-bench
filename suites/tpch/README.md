# TPC-H

Not written yet. It lands when kuma has an optimizer worth testing, which is milestone M6.

## Why it is worth waiting for

db-benchmark stresses group by and join in isolation. Each of its queries is one operation on one table, so what it measures is kernel speed and hash table quality. That is most of what matters early and almost none of what matters later.

TPC-H stresses the optimizer. Its twenty two queries join six tables, filter on correlated predicates, and produce results a thousand times smaller than their inputs. Winning them is about projection pushdown, predicate pushdown, join ordering and partition pruning, and a fast kernel underneath a bad plan loses to a slow kernel underneath a good one by a margin no amount of SIMD closes.

It is also the suite that will hurt. Polars has spent years on its optimizer and ours will be new. Losing here early is expected, and the number goes in the table anyway, because the point of running it from the start is to watch the gap close.

## What it will be

Scale factor 1 and 10 in CI, 100 on demand. Generated with the standard dbgen, not a reimplementation, so the data matches what everybody else publishes.

Parquet as the input format rather than CSV. TPC-H at scale factor 100 as CSV is a parser benchmark wearing a query benchmark's clothes, and Parquet is what anyone would actually store this in.

Every query timed to a materialized result, the same rule as everywhere else in this repository.

Correctness checked against the reference answers that ship with the specification, which is stricter than the cross library checksum used in db-benchmark and is available here because TPC-H defines the expected output.
