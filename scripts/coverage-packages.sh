#!/usr/bin/env bash
# A Go coverage profile summed by package: a line each of the package (in
# the module), its statements and those covered, sorted by package. The
# profile must name each block once (scripts/coverage-merge.sh).
#
#   scripts/coverage-packages.sh coverage.out
set -euo pipefail

profile=${1:-coverage.out}
module=$(go list -m)

awk -F'[: ]' -v module="$module/" '
	NR == 1 { next } # the mode line
	{
		pkg = $1
		sub(/\/[^\/]*$/, "", pkg)
		if (index(pkg, module) == 1) pkg = substr(pkg, length(module) + 1)
		total[pkg] += $3
		if ($4 > 0) covered[pkg] += $3
	}
	END {
		if (length(total) == 0) { print "no statements in " FILENAME > "/dev/stderr"; exit 1 }
		for (p in total) printf "%s %d %d\n", p, total[p], covered[p] | "sort"
	}' "$profile"
