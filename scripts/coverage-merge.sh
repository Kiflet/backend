#!/usr/bin/env bash
# Go coverage profiles merged into one, each block once: covered if any of
# the profiles covered it. With -coverpkg=./..., every test binary reports
# every package, so the unit run's profile repeats a block once per
# package tested, and the integration run's covers the same blocks again.
#
#   scripts/coverage-merge.sh coverage-unit.out coverage-integration.out > coverage.out
set -euo pipefail

[ $# -gt 0 ] || { echo "usage: $0 profile..." >&2; exit 2; }
awk '
	FNR == 1 { next } # each file'"'"'s mode line
	{
		key = $1 " " $2
		if (!(key in hit)) { order[++n] = key; hit[key] = 0 }
		if ($3 > 0) hit[key] = 1
	}
	END {
		print "mode: set"
		for (i = 1; i <= n; i++) print order[i], hit[order[i]]
	}' "$@"
