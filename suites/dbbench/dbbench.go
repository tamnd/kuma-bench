// Package dbbench describes the db-benchmark queries.
//
// This is the catalog and nothing else. It knows the names of the queries, the
// question each one answers, and which files it reads. It does not know how to
// run anything, because the implementations live one per library and three of
// the four are not written in Go.
//
// The catalog exists so that there is one place where a query name is defined.
// The alternative is a list in the orchestrator, another in each runner, and a
// morning spent working out why polars_q7 has no results. The test in this
// package reads the runners and checks they implement exactly what is here.
//
// The queries are the ones from duckdblabs/db-benchmark. The wording of each
// question is theirs too, kept verbatim so that our table can be read next to
// theirs.
package dbbench

import "fmt"

// Kind is which dataset a query reads.
type Kind string

// The two db-benchmark datasets.
const (
	GroupBy Kind = "groupby"
	Join    Kind = "join"
)

// Query is one benchmark query.
type Query struct {
	// Name is what the runners are called with and what appears in the
	// results, for example "groupby_q1".
	Name string
	// Kind is which dataset it reads.
	Kind Kind
	// Question is the query in words, taken from db-benchmark.
	Question string
	// Advanced marks the queries db-benchmark itself labels advanced. They are
	// the ones several libraries fail outright, so a suite average that
	// silently omits them flatters whoever failed most.
	Advanced bool
}

// Queries is every query in the suite, in the order db-benchmark defines them.
//
// The order matters for reading the results table and nothing else, but that
// is enough reason not to sort it.
var Queries = []Query{
	{Name: "groupby_q1", Kind: GroupBy, Question: "sum v1 by id1"},
	{Name: "groupby_q2", Kind: GroupBy, Question: "sum v1 by id1 and id2"},
	{Name: "groupby_q3", Kind: GroupBy, Question: "sum v1 and mean v3 by id3"},
	{Name: "groupby_q4", Kind: GroupBy, Question: "mean v1, v2 and v3 by id4"},
	{Name: "groupby_q5", Kind: GroupBy, Question: "sum v1, v2 and v3 by id6"},
	{Name: "groupby_q6", Kind: GroupBy, Question: "median and standard deviation of v3 by id4 and id6", Advanced: true},
	{Name: "groupby_q7", Kind: GroupBy, Question: "max v1 minus min v2 by id3"},
	{Name: "groupby_q8", Kind: GroupBy, Question: "the two largest v3 by id6", Advanced: true},
	{Name: "groupby_q9", Kind: GroupBy, Question: "the squared correlation of v1 and v2 by id2 and id4", Advanced: true},
	{Name: "groupby_q10", Kind: GroupBy, Question: "sum v3 and count by all six keys"},

	{Name: "join_q1", Kind: Join, Question: "inner join on the small table by integer key"},
	{Name: "join_q2", Kind: Join, Question: "inner join on the medium table by integer key"},
	{Name: "join_q3", Kind: Join, Question: "left join on the medium table by integer key"},
	{Name: "join_q4", Kind: Join, Question: "inner join on the medium table by string key"},
	{Name: "join_q5", Kind: Join, Question: "inner join on the big table by integer key"},
}

// Lookup returns the query with the given name.
func Lookup(name string) (Query, bool) {
	for _, q := range Queries {
		if q.Name == name {
			return q, true
		}
	}
	return Query{}, false
}

// Names returns every query name, in suite order.
func Names() []string {
	out := make([]string, len(Queries))
	for i, q := range Queries {
		out[i] = q.Name
	}
	return out
}

// OfKind returns the queries that read the given dataset.
func OfKind(k Kind) []Query {
	var out []Query
	for _, q := range Queries {
		if q.Kind == k {
			out = append(out, q)
		}
	}
	return out
}

// Inputs returns the file names a query reads, in the order a runner should
// take them: the main table first, then the table it joins against.
//
// The names follow the db-benchmark convention, which encodes the parameters
// rather than describing the file. G1_1e7_1e2_0_0.csv is the group by dataset
// with ten million rows, a key cardinality of a hundred, no missing values and
// unsorted. It is not a friendly name and it is the right one, because a
// directory of these is unambiguous about what is in it.
func Inputs(q Query, rows int, nas int) ([]string, error) {
	if rows <= 0 {
		return nil, fmt.Errorf("dbbench: row count must be positive, got %d", rows)
	}
	if q.Kind == GroupBy {
		return []string{fmt.Sprintf("G1_%s_1e2_%d_0.csv", exp(rows), nas)}, nil
	}

	left := fmt.Sprintf("J1_%s_NA_%d_0.csv", exp(rows), nas)
	right := func(size int) string {
		return fmt.Sprintf("J1_%s_%s_%d_0.csv", exp(rows), exp(max(size, 1)), nas)
	}
	switch q.Name {
	case "join_q1":
		return []string{left, right(rows / 1_000_000)}, nil
	case "join_q2", "join_q3", "join_q4":
		return []string{left, right(rows / 1_000)}, nil
	case "join_q5":
		return []string{left, right(rows)}, nil
	}
	return nil, fmt.Errorf("dbbench: no inputs defined for %s", q.Name)
}

// exp formats a round number the way db-benchmark names its files, so ten
// million becomes 1e7. This is duplicated in cmd/gendata on purpose: the two
// have to agree, and a test in this package checks that they do, which is a
// cheaper guarantee than a shared package for eight lines of formatting.
func exp(n int) string {
	if n <= 0 {
		return fmt.Sprint(n)
	}
	digits := 0
	for v := n; v%10 == 0; v /= 10 {
		digits++
	}
	lead := n
	for range digits {
		lead /= 10
	}
	if lead != 1 {
		return fmt.Sprint(n)
	}
	return fmt.Sprintf("1e%d", digits)
}
