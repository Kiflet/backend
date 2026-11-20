#!/usr/bin/env bash
# What the unit and integration tests cover together, by package, as a
# Markdown table with each package's floor (tests/coverage-floor.txt). CI
# adds it to the integration job's summary.
#
#   make cover                           # writes coverage.out and runs this
#   scripts/coverage-summary.sh coverage.out
set -euo pipefail

profile=${1:-coverage.out}
floor=$(dirname "$0")/../tests/coverage-floor.txt

echo "| Package | Statements | Covered | Floor |"
echo "|:--|--:|--:|--:|"
"$(dirname "$0")/coverage-packages.sh" "$profile" | awk '
	FILENAME != "-" { floor[$1] = $2; next }
	{
		pct = $2 ? 100 * $3 / $2 : 0
		printf "| %s | %d | %.1f%% | %s |\n", $1, $2, pct, ($1 in floor) ? floor[$1] "%" : "–"
		all += $2; hit += $3
	}
	END { printf "| All | %d | %.1f%% | |\n", all, all ? 100 * hit / all : 0 }' "$floor" -
