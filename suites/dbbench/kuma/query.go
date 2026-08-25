package main

import (
	"github.com/tamnd/kuma"
	"github.com/tamnd/kuma/csv"
)

// query is one implemented benchmark query.
//
// run returns the result and the names of the value columns the checksum is
// taken over, which are the columns that hold an answer rather than a key.
type query struct {
	inputs int
	run    func(files []string) (*kuma.Frame[kuma.Dynamic], []string, error)
}

// queries is every query this runner can answer. The ones missing from here
// are in the pending list in main.go, and the test in suites/dbbench checks
// that the two together cover the catalog.
var queries = map[string]query{
	"groupby_q1":  {inputs: 1, run: q1},
	"groupby_q2":  {inputs: 1, run: q2},
	"groupby_q3":  {inputs: 1, run: q3},
	"groupby_q4":  {inputs: 1, run: q4},
	"groupby_q5":  {inputs: 1, run: q5},
	"groupby_q6":  {inputs: 1, run: q6},
	"groupby_q7":  {inputs: 1, run: q7},
	"groupby_q10": {inputs: 1, run: q10},

	"join_q1": {inputs: 2, run: joinQ1},
	"join_q2": {inputs: 2, run: joinQ2},
	"join_q3": {inputs: 2, run: joinQ3},
	"join_q4": {inputs: 2, run: joinQ4},
	"join_q5": {inputs: 2, run: joinQ5},
}

// readCSV reads the named columns of a file and nothing else.
//
// A query touches three columns of the nine and a reader that takes all nine is
// measuring the reader. pandas is given usecols and Polars pushes the
// projection into scan_csv, so this is the same deal the other two get rather
// than a favour to kuma.
//
// The types are inferred, again because the other two runners infer them. A
// schema written out here would be kuma reading a file the others are guessing
// at.
func readCSV(path string, columns ...string) (*kuma.Frame[kuma.Dynamic], error) {
	return kuma.ReadCSVFile(path, &csv.Options{Columns: columns})
}

// groupBy is the shape every group by query in the suite has: read a few
// columns, group by some of them, work out the rest.
func groupBy(path string, columns, keys []string, aggs ...kuma.Aggregation) (
	*kuma.Frame[kuma.Dynamic], error) {
	f, err := readCSV(path, columns...)
	if err != nil {
		return nil, err
	}
	g, err := f.GroupBy(keys...)
	if err != nil {
		return nil, err
	}
	return g.Agg(aggs...)
}

// A row whose key is missing is a group of its own here, in pandas because the
// runner passes dropna=False, and in Polars and kuma because that is what both
// of them do already. All three therefore count the same rows, which is the
// only way the checksums mean anything.

func q1(files []string) (*kuma.Frame[kuma.Dynamic], []string, error) {
	out, err := groupBy(files[0],
		[]string{"id1", "v1"}, []string{"id1"},
		kuma.Sum("v1"))
	return out, []string{"v1"}, err
}

func q2(files []string) (*kuma.Frame[kuma.Dynamic], []string, error) {
	out, err := groupBy(files[0],
		[]string{"id1", "id2", "v1"}, []string{"id1", "id2"},
		kuma.Sum("v1"))
	return out, []string{"v1"}, err
}

func q3(files []string) (*kuma.Frame[kuma.Dynamic], []string, error) {
	out, err := groupBy(files[0],
		[]string{"id3", "v1", "v3"}, []string{"id3"},
		kuma.Sum("v1"), kuma.Mean("v3"))
	return out, []string{"v1", "v3"}, err
}

func q4(files []string) (*kuma.Frame[kuma.Dynamic], []string, error) {
	out, err := groupBy(files[0],
		[]string{"id4", "v1", "v2", "v3"}, []string{"id4"},
		kuma.Mean("v1"), kuma.Mean("v2"), kuma.Mean("v3"))
	return out, []string{"v1", "v2", "v3"}, err
}

func q5(files []string) (*kuma.Frame[kuma.Dynamic], []string, error) {
	out, err := groupBy(files[0],
		[]string{"id6", "v1", "v2", "v3"}, []string{"id6"},
		kuma.Sum("v1"), kuma.Sum("v2"), kuma.Sum("v3"))
	return out, []string{"v1", "v2", "v3"}, err
}

// q6 is on db-benchmark's advanced list because a median needs the whole group
// in memory, which is what makes it the first query to fall over at the large
// sizes. The standard deviation is the sample one, a divisor of n minus one,
// which is what pandas and Polars both default to.
func q6(files []string) (*kuma.Frame[kuma.Dynamic], []string, error) {
	out, err := groupBy(files[0],
		[]string{"id4", "id6", "v3"}, []string{"id4", "id6"},
		kuma.Median("v3").As("v3_median"), kuma.Std("v3", 1).As("v3_sd"))
	return out, []string{"v3_median", "v3_sd"}, err
}

// q7 is the one query that computes something from the aggregates rather than
// just reporting them, so it is where an expression over a result frame gets
// exercised.
func q7(files []string) (*kuma.Frame[kuma.Dynamic], []string, error) {
	agg, err := groupBy(files[0],
		[]string{"id3", "v1", "v2"}, []string{"id3"},
		kuma.Max("v1"), kuma.Min("v2"))
	if err != nil {
		return nil, nil, err
	}

	out, err := agg.WithExpr("range_v1_v2", kuma.Dyn("v1").SubExpr(kuma.Dyn("v2")))
	if err != nil {
		return nil, nil, err
	}
	out, err = out.Select("id3", "range_v1_v2")
	return out, []string{"range_v1_v2"}, err
}

// q10 groups by all six keys at once, which on the ten million row file is
// nearly as many groups as rows. It is the query that says whether a group by
// is a hash table or a sort.
func q10(files []string) (*kuma.Frame[kuma.Dynamic], []string, error) {
	keys := []string{"id1", "id2", "id3", "id4", "id5", "id6"}
	out, err := groupBy(files[0],
		append(append([]string{}, keys...), "v3"), keys,
		kuma.Sum("v3"), kuma.Size().As("count"))
	return out, []string{"v3", "count"}, err
}

// join reads the two tables and joins them on one key.
//
// The value columns are v1 from the left table and v2 from the right one, so
// the checksum sees both sides and a join that quietly dropped the right hand
// columns would not pass for a fast one.
func join(files []string, key string, how kuma.JoinType) (
	*kuma.Frame[kuma.Dynamic], []string, error) {
	left, err := readCSV(files[0], key, "v1")
	if err != nil {
		return nil, nil, err
	}
	right, err := readCSV(files[1], key, "v2")
	if err != nil {
		return nil, nil, err
	}

	out, err := left.Join(right, kuma.Using(key), how)
	return out, []string{"v1", "v2"}, err
}

func joinQ1(files []string) (*kuma.Frame[kuma.Dynamic], []string, error) {
	return join(files, "id1", kuma.InnerJoin)
}

func joinQ2(files []string) (*kuma.Frame[kuma.Dynamic], []string, error) {
	return join(files, "id2", kuma.InnerJoin)
}

// joinQ3 is the left join, the one that has to keep the roughly one row in ten
// with no match. Its output is larger than the inner join's and the missing
// values on the right are what the checksum has to skip.
func joinQ3(files []string) (*kuma.Frame[kuma.Dynamic], []string, error) {
	return join(files, "id2", kuma.LeftJoin)
}

// joinQ4 is the same join as q2 by a different key, and the gap between the two
// is what the key representation costs.
func joinQ4(files []string) (*kuma.Frame[kuma.Dynamic], []string, error) {
	return join(files, "id5", kuma.InnerJoin)
}

// joinQ5 joins against a table the same size as the left one, so the hash table
// does not fit anywhere pleasant.
func joinQ5(files []string) (*kuma.Frame[kuma.Dynamic], []string, error) {
	return join(files, "id3", kuma.InnerJoin)
}
