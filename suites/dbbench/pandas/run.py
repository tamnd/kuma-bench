"""The pandas runner.

pandas 3.0 defaults to Arrow backed strings and a copy on write data model, and
both of those change the numbers a lot. It is still single threaded and still
eager, which is the honest comparison: this is what most people writing Python
data code actually run.

Two configuration choices, both made in pandas' favour, both recorded in the
mode field of every record:

  The PyArrow CSV reader, because the Python parser is several times slower and
  benchmarking against it would be measuring a reader nobody chooses.

  dtype_backend set to pyarrow, so the value columns are Arrow arrays rather
  than NumPy object arrays. This is the fast path and the one pandas is moving
  towards.

If either of those makes a query slower rather than faster, that is a pandas
result and it stays in the table.
"""

import os
import sys

import pandas as pd

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from common import main  # noqa: E402

MODE = "pyarrow-engine,arrow-backend"


# The group by queries only ever touch a handful of columns, and a reader that
# takes the whole file when the query needs three columns is measuring the
# reader. Every engine here is given the same chance, which means the columns
# the query reads and no others.
def read(path, columns):
    return pd.read_csv(
        path,
        engine="pyarrow",
        dtype_backend="pyarrow",
        usecols=columns,
    )


def total(series):
    """Sum a result column, skipping missing values.

    pandas skips them already. The wrapper exists so that both runners state
    what they do rather than one of them relying on a default, since the whole
    reason the checksums are worth having is that they are computed the same
    way on both sides.
    """
    return float(series.sum())


def groupby(inputs, columns, fn, value_columns):
    # pandas is eager, so the result is fully materialized the moment fn
    # returns and there is nothing to force.
    return fn(read(inputs[0], columns)), value_columns


def q1(inputs):
    return groupby(
        inputs,
        ["id1", "v1"],
        lambda x: x.groupby("id1", as_index=False, observed=True, dropna=False).agg(
            v1=("v1", "sum")
        ),
        ["v1"],
    )


def q2(inputs):
    return groupby(
        inputs,
        ["id1", "id2", "v1"],
        lambda x: x.groupby(["id1", "id2"], as_index=False, observed=True, dropna=False).agg(
            v1=("v1", "sum")
        ),
        ["v1"],
    )


def q3(inputs):
    return groupby(
        inputs,
        ["id3", "v1", "v3"],
        lambda x: x.groupby("id3", as_index=False, observed=True, dropna=False).agg(
            v1=("v1", "sum"), v3=("v3", "mean")
        ),
        ["v1", "v3"],
    )


def q4(inputs):
    return groupby(
        inputs,
        ["id4", "v1", "v2", "v3"],
        lambda x: x.groupby("id4", as_index=False, observed=True, dropna=False).agg(
            v1=("v1", "mean"), v2=("v2", "mean"), v3=("v3", "mean")
        ),
        ["v1", "v2", "v3"],
    )


def q5(inputs):
    return groupby(
        inputs,
        ["id6", "v1", "v2", "v3"],
        lambda x: x.groupby("id6", as_index=False, observed=True, dropna=False).agg(
            v1=("v1", "sum"), v2=("v2", "sum"), v3=("v3", "sum")
        ),
        ["v1", "v2", "v3"],
    )


def q6(inputs):
    # Median needs the whole group in memory, which is why this one is on
    # db-benchmark's advanced list and why it is the first query to fall over
    # on the large sizes.
    return groupby(
        inputs,
        ["id4", "id6", "v3"],
        lambda x: x.groupby(["id4", "id6"], as_index=False, observed=True, dropna=False).agg(
            v3_median=("v3", "median"), v3_sd=("v3", "std")
        ),
        ["v3_median", "v3_sd"],
    )


def q7(inputs):
    return groupby(
        inputs,
        ["id3", "v1", "v2"],
        lambda x: (
            x.groupby("id3", as_index=False, observed=True, dropna=False)
            .agg(v1=("v1", "max"), v2=("v2", "min"))
            .assign(range_v1_v2=lambda d: d["v1"] - d["v2"])[["id3", "range_v1_v2"]]
        ),
        ["range_v1_v2"],
    )


def q8(inputs):
    def top2(x):
        x = x[["id6", "v3"]].dropna(subset=["v3"])
        return (
            x.sort_values("v3", ascending=False)
            .groupby("id6", as_index=False, observed=True)
            .head(2)
        )

    return groupby(inputs, ["id6", "v3"], top2, ["v3"])


def q9(inputs):
    def r2(x):
        return (
            x[["id2", "id4", "v1", "v2"]]
            .groupby(["id2", "id4"], as_index=False, observed=True, dropna=False)
            .apply(lambda g: pd.Series({"r2": g["v1"].corr(g["v2"]) ** 2}), include_groups=False)
        )

    return groupby(inputs, ["id2", "id4", "v1", "v2"], r2, ["r2"])


def q10(inputs):
    keys = ["id1", "id2", "id3", "id4", "id5", "id6"]
    return groupby(
        inputs,
        keys + ["v3"],
        lambda x: x.groupby(keys, as_index=False, observed=True, dropna=False).agg(
            v3=("v3", "sum"), count=("v3", "size")
        ),
        ["v3", "count"],
    )


def join(inputs, on, how, left_columns, right_columns):
    left = read(inputs[0], left_columns)
    right = read(inputs[1], right_columns)
    return left.merge(right, on=on, how=how), ["v1", "v2"]


def join_q1(inputs):
    return join(inputs, "id1", "inner", ["id1", "v1"], ["id1", "v2"])


def join_q2(inputs):
    return join(inputs, "id2", "inner", ["id2", "v1"], ["id2", "v2"])


def join_q3(inputs):
    # The left join is the one that has to keep the roughly one row in ten with
    # no match, so its output is larger than the inner join's and the null
    # handling actually gets exercised.
    return join(inputs, "id2", "left", ["id2", "v1"], ["id2", "v2"])


def join_q4(inputs):
    # Same join as q2 by a string key rather than an integer one. The gap
    # between this and q2 is the cost of the string representation, which is
    # the whole reason kuma uses StringView.
    return join(inputs, "id5", "inner", ["id5", "v1"], ["id5", "v2"])


def join_q5(inputs):
    return join(inputs, "id3", "inner", ["id3", "v1"], ["id3", "v2"])


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
    main("pandas", pd.__version__, MODE, QUERIES, total)
