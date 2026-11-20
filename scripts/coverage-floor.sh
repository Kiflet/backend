#!/usr/bin/env bash
# The floor under each package's coverage, by the unit and integration
# tests together (tests/coverage-floor.txt). It only goes up.
#
#   scripts/coverage-floor.sh check coverage.out   # fail if a package fell below its floor
#   scripts/coverage-floor.sh raise coverage.out   # raise the floors to what is covered now
#
# A floor sits a point under what was covered when it was raised: the
# integration tests run workers and retries on timers, so a run can cover
# a few statements more or less than the last without any change.
set -euo pipefail

usage() { echo "usage: $0 check|raise profile" >&2; exit 2; }
[ $# -eq 2 ] || usage
cmd=$1 profile=$2
floor=$(dirname "$0")/../tests/coverage-floor.txt
packages=$("$(dirname "$0")/coverage-packages.sh" "$profile")

case $cmd in
check)
	printf '%s\n' "$packages" | awk '
		FILENAME != "-" { if ($0 !~ /^#/ && NF == 2) floor[$1] = $2; next }
		{
			pct = $2 ? 100 * $3 / $2 : 0
			seen[$1] = 1
			if (!($1 in floor)) { printf "%s: %.1f%%, no floor yet (make coverage-floor)\n", $1, pct; next }
			if (pct + 0.05 < floor[$1]) { printf "%s: %.1f%% covered, below its floor of %s%%\n", $1, pct, floor[$1]; bad++ }
			else if (pct - floor[$1] >= 2) printf "%s: %.1f%%, floor %s%% (make coverage-floor raises it)\n", $1, pct, floor[$1]
		}
		END {
			for (p in floor) if (!(p in seen)) printf "%s has a floor but no statements: remove it (make coverage-floor)\n", p
			if (bad) { printf "%d package(s) below the coverage floor\n", bad; exit 1 }
		}' "$floor" -
	;;
raise)
	tmp=$(mktemp)
	{
		echo "# The least each package's statements must be covered by the unit and"
		echo "# integration tests together, in percent: make cover checks it, and CI"
		echo "# fails below it. Written by make coverage-floor, which only raises it."
		printf '%s\n' "$packages" | awk '
			FILENAME != "-" { if ($0 !~ /^#/ && NF == 2) floor[$1] = $2; next }
			{
				pct = $2 ? 100 * $3 / $2 : 0
				f = int((pct - 1) * 10) / 10
				if (f < 0) f = 0
				if (($1 in floor) && floor[$1] + 0 > f) f = floor[$1]
				printf "%s %.1f\n", $1, f
			}' "$floor" -
	} > "$tmp"
	mv "$tmp" "$floor"
	;;
*) usage ;;
esac
