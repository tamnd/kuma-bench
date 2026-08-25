"""Shared plumbing for the Python runners.

A runner measures one query, once, for one library, and prints one JSON object
to standard output. It is spawned fresh for every measurement, which is the
only reliable way to stop one query from warming a cache for the next.

The contract with the orchestrator:

  run.py --query groupby_q1 --input data/G1_1e7_1e2_0_0.csv

Input paths arrive on the command line rather than being worked out here. The
catalog in dbbench.go decides which files a query reads, and it decides for
every library at once, so there is nothing to drift.

The timed section starts before the file is opened and stops after the result
is fully materialized. Reading the input counts. A query function that returns
an unevaluated plan has not finished, so every one of them ends in a call that
forces the whole result to exist.
"""

import argparse
import json
import platform
import sys
import time
import traceback

# Windows has no getrusage. The suite runs on Linux, and the record has the
# field either way, so a platform that cannot answer says nothing rather than
# making the runner refuse to start. The kuma runner does the same thing in
# rss_other.go, so that the three libraries stay comparable on a box where the
# number is not available to any of them.
try:
    import resource
except ImportError:
    resource = None


def emit(report):
    """Write the report and exit.

    Runners exit zero even when the query failed. A failed query is a record
    with an error in it, and a non-zero exit would make the orchestrator throw
    away the one piece of information it just learned.
    """
    json.dump(report, sys.stdout)
    sys.stdout.write("\n")
    sys.stdout.flush()
    sys.exit(0)


def peak_rss_bytes():
    """Peak resident set size of this process.

    getrusage reports kilobytes on Linux and bytes on macOS, and nothing in the
    documentation warns you, so a number that is off by a factor of a thousand
    is the usual first result.

    Zero means it was not measured, which is what a platform without getrusage
    gets.
    """
    if resource is None:
        return 0
    peak = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
    return peak if platform.system() == "Darwin" else peak * 1024


def checksum(frame, columns, total):
    """A digest of a result, for comparing answers across libraries.

    It is the sum of each value column, formatted to six significant digits.
    Six rather than exact, because summing a hundred million floats in a
    different order gives a different answer in the last few bits, and that is
    arithmetic rather than a bug. Anything that disagrees earlier than the
    seventh digit is a real difference and worth stopping for.

    total comes from the runner, because summing a column with missing values
    in it is the one place these libraries genuinely disagree. pandas skips
    them, Polars propagates a NaN through the whole sum, and comparing those
    two answers would report a mismatch on every query that produces a NaN.
    Each runner is responsible for skipping, so that what is compared is the
    arithmetic rather than the convention.

    The digest stays readable on purpose. A hash tells you two engines
    disagree; this tells you which column.
    """
    return " ".join(f"{name}={total(frame[name]):.6g}" for name in columns)


def main(library, version, mode, queries, total):
    """Run one query and print the report.

    queries maps a query name to a function taking the list of input paths and
    returning (result, value_columns). Everything that function does, meaning
    reading the files, computing and materializing, happens inside the timed
    section.

    Counting the rows and taking the checksum happen after the clock stops.
    Both are cheap next to the query and neither is part of what a user would
    be waiting for, so timing them would be measuring this harness.
    """
    parser = argparse.ArgumentParser()
    parser.add_argument("--query", required=True)
    parser.add_argument("--input", action="append", default=[])
    args = parser.parse_args()

    if args.query not in queries:
        emit(
            {
                "error": f"the {library} runner has no implementation for {args.query}",
                "library_version": version,
            }
        )

    try:
        start = time.perf_counter()
        result, columns = queries[args.query](args.input)
        elapsed = time.perf_counter() - start

        out_rows = len(result)
        digest = checksum(result, columns, total)
    except Exception:  # noqa: BLE001
        # The full traceback goes to standard error so it lands in the CI log,
        # and the last line goes in the record so the results table says
        # something more useful than "failed".
        traceback.print_exc(file=sys.stderr)
        lines = traceback.format_exc().strip().splitlines()
        emit(
            {
                "error": lines[-1] if lines else "unknown error",
                "library_version": version,
                "mode": mode,
            }
        )

    emit(
        {
            "elapsed": elapsed,
            "out_rows": int(out_rows),
            "checksum": digest,
            "peak_rss_bytes": peak_rss_bytes(),
            "library_version": version,
            "mode": mode,
        }
    )
