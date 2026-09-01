// This directory is a separate module from the rest of kuma-bench. See the
// comment in main.go: it exists so that a breaking change in kuma cannot stop
// the harness from building.
module github.com/tamnd/kuma-bench/suites/dbbench/kuma

go 1.27.0

require github.com/tamnd/kuma v0.0.27
