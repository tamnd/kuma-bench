#!/usr/bin/env bash
#
# Run the kuma microbenchmarks on real machines and record the results.
#
#   scripts/micro.sh server1 server3 gamingpc
#   scripts/micro.sh -c 10 -r github.com/tamnd/kuma/bitmap server3
#
# Each host gets a kuma checkout under ~/src/kuma, fetched to the commit being
# measured, and runs go test -bench there. The output comes back over the pipe
# and cmd/micro turns it into records under results/.
#
# The benchmarks also run in kuma's own CI on every pull request. That is the
# right place to catch a benchmark that doubled and the wrong place to learn
# what the code does on hardware, because a shared cloud runner is a shared
# cloud runner. These runs are the ones with a machine behind them.
#
# The host has to have a Go toolchain new enough for kuma and nothing else.
# There is no Python here and no dataset, which is the reason this reads text
# on a pipe instead of installing this repository on every box.

set -euo pipefail

count=5
bench_regexp='.'
packages='./...'
ref=''
outdir='results'
goexp=''

usage() {
	cat >&2 <<'EOF'
usage: scripts/micro.sh [options] host...

  -c N        repetitions, passed to go test -count (default 5)
  -b REGEXP   which benchmarks to run, passed to go test -bench (default .)
  -p PKGS     which packages to run them in (default ./...)
  -r REF      the kuma commit or branch to measure (default origin/main)
  -o DIR      where to write the results (default results)
  -s          set GOEXPERIMENT=simd for the run
EOF
	exit 2
}

while getopts 'c:b:p:r:o:sh' opt; do
	case "$opt" in
	c) count="$OPTARG" ;;
	b) bench_regexp="$OPTARG" ;;
	p) packages="$OPTARG" ;;
	r) ref="$OPTARG" ;;
	o) outdir="$OPTARG" ;;
	s) goexp='simd' ;;
	*) usage ;;
	esac
done
shift $((OPTIND - 1))
[ $# -gt 0 ] || usage

repo='https://github.com/tamnd/kuma.git'
root="$(cd "$(dirname "$0")/.." && pwd)"
mkdir -p "$root/$outdir"

for host in "$@"; do
	echo "== $host" >&2

	raw="$(mktemp)"
	trap 'rm -f "$raw"' EXIT

	# The gaming box runs Windows, so it gets the same run driven by a
	# PowerShell script that is copied over each time rather than kept in sync
	# by hand. Everything after the pipe is identical, which is the point of
	# having the tool parse text.
	if ! ssh "$host" 'uname -s' >/dev/null 2>&1; then
		scp -q "$root/scripts/micro.ps1" "$host:micro.ps1"
		# PowerShell takes these as its own arguments, so they are quoted the
		# way it expects rather than the way a shell would.
		# The variables are meant to expand here, on this side, since the
		# remote has no idea what was asked for.
		# shellcheck disable=SC2029
		ssh "$host" "powershell -NoProfile -ExecutionPolicy Bypass -File micro.ps1 \
			-Ref \"${ref:-origin/main}\" -Count $count -Bench \"$bench_regexp\" \
			-Packages \"$packages\" -GoExperiment \"$goexp\"" | tee "$raw" >&2

		# Windows ends its lines with a carriage return, which read would
		# otherwise hand to a flag that wants a number.
		read -r commit go_version memory_gb <<-EOF
			$(ssh "$host" "powershell -NoProfile -ExecutionPolicy Bypass -File micro.ps1 -Facts" | tr -d '\r')
		EOF

		go run "$root/cmd/micro" \
			-in "$raw" -out "$root/$outdir" -host "$host" -runner bare-metal \
			-commit "$commit" -go "$go_version" -memory-gb "$memory_gb" \
			${goexp:+-simd} -v

		rm -f "$raw"
		trap - EXIT
		continue
	fi

	# Taken before the run rather than after, so that it says what the machine
	# was already doing rather than counting the benchmark itself. A box under
	# load is not a box you can time anything on, and the number goes in the
	# record either way so that a reader can throw the run out on purpose
	# rather than believing it by accident.
	load="$(ssh "$host" 'cut -d" " -f1 /proc/loadavg' 2>/dev/null || echo 0)"
	cores="$(ssh "$host" 'nproc' 2>/dev/null || echo 0)"
	if [ "$(echo "$load $cores" | awk '{print ($1 > $2) ? 1 : 0}')" = 1 ]; then
		echo "   warning: $host is at load $load on $cores cores, these timings are not evidence of much" >&2
	fi

	# Everything below runs on the host. The checkout is left in place between
	# runs on purpose, so that a rerun costs a fetch rather than a clone.
	# shellcheck disable=SC2029
	ssh "$host" "
		set -eu
		export PATH=\"\$HOME/sdk/go1.27.0/bin:\$PATH\"
		mkdir -p ~/src
		if [ ! -d ~/src/kuma/.git ]; then git clone -q '$repo' ~/src/kuma; fi
		cd ~/src/kuma
		git fetch -q origin
		# A bare branch name is resolved against origin first. Asking git to
		# check out a name it only knows as a remote branch makes it want to
		# create a local branch of that name, which is not something --detach
		# will sit next to.
		at=\$(git rev-parse -q --verify 'origin/${ref:-origin/main}^{commit}' || git rev-parse --verify '${ref:-origin/main}^{commit}')
		git checkout -q --detach \"\$at\"
		go version
		${goexp:+GOEXPERIMENT=$goexp }go test -run '^\$' -bench '$bench_regexp' -benchmem -count '$count' $packages
	" | tee "$raw" >&2

	# The commit is read back out of the checkout rather than carried over from
	# the run above, because these are two separate login sessions and a file
	# left in /tmp by the first one is not something the second one can count on
	# finding. The checkout is still sitting at the commit that was measured, so
	# it is the thing to ask.
	read -r commit go_version memory_gb <<-EOF
		$(ssh "$host" "
			git -C ~/src/kuma rev-parse HEAD | tr -d '\n'
			printf ' '
			\$HOME/sdk/go1.27.0/bin/go env GOVERSION | tr -d '\n'
			printf ' '
			awk '/MemTotal/ {printf \"%d\", int(\$2/1024/1024 + 0.5)}' /proc/meminfo
		")
	EOF

	go run "$root/cmd/micro" \
		-in "$raw" \
		-out "$root/$outdir" \
		-host "$host" \
		-runner bare-metal \
		-commit "$commit" \
		-go "$go_version" \
		-memory-gb "$memory_gb" \
		-load "$load" \
		${goexp:+-simd} \
		-v

	rm -f "$raw"
	trap - EXIT
done
