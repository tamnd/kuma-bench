//go:build unix

package main

import (
	"runtime"
	"syscall"
)

// peakRSS returns the largest resident set this process has reached, in bytes.
//
// getrusage reports kilobytes on Linux and bytes on macOS, and nothing in the
// documentation warns you, so a number that is off by a factor of a thousand is
// the usual first result. The Python runners have the same two lines in
// common.py for the same reason.
func peakRSS() int64 {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0
	}
	if runtime.GOOS == "darwin" {
		return int64(usage.Maxrss)
	}
	return int64(usage.Maxrss) * 1024
}
