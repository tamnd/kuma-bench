//go:build !unix

package main

// peakRSS returns zero on the systems with no getrusage.
//
// The suite runs on Linux, and the record has the field either way, so a
// platform that cannot answer says nothing rather than making the runner
// refuse to build. A zero here reads as not measured, which is what it is.
func peakRSS() int64 { return 0 }
