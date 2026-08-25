package main

import (
	"fmt"
	"math"
	"strings"

	"github.com/tamnd/kuma"
	"github.com/tamnd/kuma/array"
	"github.com/tamnd/kuma/dtype"
)

// checksum is a digest of a result, for comparing answers across libraries.
//
// It is the sum of each value column formatted to six significant digits, and
// it is defined in suites/dbbench/common.py, which the Python runners share.
// The format has to match theirs character for character, because the report
// tool compares the strings.
//
//	v1=5.00007e+07 v3=49.9973
func checksum(f *kuma.Frame[kuma.Dynamic], columns []string) (string, error) {
	parts := make([]string, len(columns))
	for i, name := range columns {
		c, err := f.Column(name)
		if err != nil {
			return "", err
		}
		sum, err := total(c)
		if err != nil {
			return "", err
		}
		parts[i] = fmt.Sprintf("%s=%.6g", name, sum)
	}
	return strings.Join(parts, " "), nil
}

// total adds up one result column as a float64, skipping the values that are
// not there.
//
// Skipping is the whole point. pandas skips missing values when it sums,
// Polars propagates a NaN through the entire sum, and the two Python runners
// each say which they do rather than relying on a default. This one says it
// too. A NaN counts as missing here for the same reason: it is not an answer,
// and one of them would otherwise turn the digest of a whole column into the
// word NaN.
func total(c kuma.Column) (float64, error) {
	d := c.Data()
	switch c.DType().Kind() {
	case dtype.Int8Kind:
		return sum[int8](d), nil
	case dtype.Int16Kind:
		return sum[int16](d), nil
	case dtype.Int32Kind:
		return sum[int32](d), nil
	case dtype.Int64Kind:
		return sum[int64](d), nil
	case dtype.Uint8Kind:
		return sum[uint8](d), nil
	case dtype.Uint16Kind:
		return sum[uint16](d), nil
	case dtype.Uint32Kind:
		return sum[uint32](d), nil
	case dtype.Uint64Kind:
		return sum[uint64](d), nil
	case dtype.Float32Kind:
		return sum[float32](d), nil
	case dtype.Float64Kind:
		return sum[float64](d), nil
	}
	return 0, fmt.Errorf("cannot total column %q, which is %s", c.Name(), c.DType())
}

// sum adds up a column of one machine type.
//
// It walks the chunks rather than asking the column for value i, because
// asking costs a binary search over the chunks and there are a hundred million
// of them to ask about.
func sum[T array.Numeric](c *array.Chunked) float64 {
	var out float64
	for _, a := range c.Chunks() {
		values := a.Values[T]()
		for i, v := range values {
			f := float64(v)
			if math.IsNaN(f) || a.IsNull(i) {
				continue
			}
			out += f
		}
	}
	return out
}
