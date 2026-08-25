"""The polars runner.

Polars gets its lazy API here, not its eager one. That is the configuration
Polars documents, the one it optimizes for, and the one anyone benchmarking it
seriously would use. Comparing against eager Polars would produce a better
looking table for us and would not be a comparison.

Every query ends in collect(), inside the timed section. A LazyFrame that is
never collected has done no work, and reporting the time it took to build the
plan is the single most common way a dataframe benchmark comes out wrong.
scan_csv is what feeds it, so the reader participates in projection pushdown
the way it is meant to.

The streaming engine is a separate mode rather than the default. It wins on
data that does not fit in memory and costs something on data that does, so
running both and recording which is which is more informative than picking one.
Set KUMA_BENCH_POLARS_STREAMING=1 for the streaming numbers.
"""

import os
import sys

import polars as pl

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from common import main  # noqa: E402

STREAMING = os.environ.get("KUMA_BENCH_POLARS_STREAMING") == "1"
MODE = "lazy,streaming" if STREAMING else "lazy"


def collect(frame):
    if STREAMING:
        return frame.collect(engine="streaming")
    return frame.collect()


def total(series):
    """Sum a result column, skipping missing values.

    Polars propagates rather than skips: a single NaN anywhere in the column
    makes the sum NaN. pandas skips, so without this the two would disagree on
    every query that produces one, and the correlation query produces plenty,
    because a group whose v1 never varies has no correlation to report.

    drop_nans only applies to float columns, and calling it on an integer one
    raises, which is why the dtype is checked first.
    """
    if series.dtype.is_float():
        series = series.drop_nans()
    return float(series.drop_nulls().sum())


def groupby(inputs, fn, value_columns):
    # collect is what forces the work. A LazyFrame that is never collected has
    # built a plan and done nothing, and timing that is the classic way a
    # dataframe benchmark comes out wrong by two orders of magnitude.
    return collect(fn(pl.scan_csv(inputs[0]))), value_columns


def q1(inputs):
    return groupby(
        inputs,
        lambda x: x.group_by("id1").agg(pl.sum("v1")),
        ["v1"],
    )


def q2(inputs):
    return groupby(
        inputs,
        lambda x: x.group_by("id1", "id2").agg(pl.sum("v1")),
        ["v1"],
    )


def q3(inputs):
    return groupby(
        inputs,
        lambda x: x.group_by("id3").agg(pl.sum("v1"), pl.mean("v3")),
        ["v1", "v3"],
    )


def q4(inputs):
    return groupby(
        inputs,
        lambda x: x.group_by("id4").agg(pl.mean("v1"), pl.mean("v2"), pl.mean("v3")),
        ["v1", "v2", "v3"],
    )


def q5(inputs):
    return groupby(
        inputs,
        lambda x: x.group_by("id6").agg(pl.sum("v1"), pl.sum("v2"), pl.sum("v3")),
        ["v1", "v2", "v3"],
    )


def q6(inputs):
    return groupby(
        inputs,
        lambda x: x.group_by("id4", "id6").agg(
            pl.median("v3").alias("v3_median"),
            pl.std("v3").alias("v3_sd"),
        ),
        ["v3_median", "v3_sd"],
    )


def q7(inputs):
    return groupby(
        inputs,
        lambda x: x.group_by("id3").agg((pl.max("v1") - pl.min("v2")).alias("range_v1_v2")),
        ["range_v1_v2"],
    )


def q8(inputs):
    return groupby(
        inputs,
        lambda x: x.drop_nulls("v3").group_by("id6").agg(pl.col("v3").top_k(2)).explode("v3"),
        ["v3"],
    )


def q9(inputs):
    return groupby(
        inputs,
        lambda x: x.group_by("id2", "id4").agg((pl.corr("v1", "v2") ** 2).alias("r2")),
        ["r2"],
    )


def q10(inputs):
    return groupby(
        inputs,
        lambda x: x.group_by("id1", "id2", "id3", "id4", "id5", "id6").agg(
            pl.sum("v3"), pl.len().alias("count")
        ),
        ["v3", "count"],
    )


def join(inputs, on, how):
    left = pl.scan_csv(inputs[0])
    right = pl.scan_csv(inputs[1])
    return collect(left.join(right, on=on, how=how)), ["v1", "v2"]


def join_q1(inputs):
    return join(inputs, "id1", "inner")


def join_q2(inputs):
    return join(inputs, "id2", "inner")


def join_q3(inputs):
    return join(inputs, "id2", "left")


def join_q4(inputs):
    return join(inputs, "id5", "inner")


def join_q5(inputs):
    return join(inputs, "id3", "inner")


QUERIES = {
    "groupby_q1": q1,
    "groupby_q2": q2,
    "groupby_q3": q3,
    "groupby_q4": q4,
    "groupby_q5": q5,
    "groupby_q6": q6,
    "groupby_q7": q7,
    "groupby_q8": q8,
    "groupby_q9": q9,
    "groupby_q10": q10,
    "join_q1": join_q1,
    "join_q2": join_q2,
    "join_q3": join_q3,
    "join_q4": join_q4,
    "join_q5": join_q5,
}

if __name__ == "__main__":
    main("polars", pl.__version__, MODE, QUERIES, total)
