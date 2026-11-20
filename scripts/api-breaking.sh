#!/usr/bin/env bash
# Breaking changes to the API's specification since a base revision
# (docs/architecture.md, section 11, "Who may call what"). A breaking change
# to a public or driver operation fails: those are promised to integrations
# and to the app versions on phones. One to an app operation fails unless
# API_BREAK=true, which CI sets from the pull request's api-break label.
#
#   scripts/api-breaking.sh origin/main
set -euo pipefail

base=${1:-origin/main}
version=${OASDIFF_VERSION:-v1.35.0}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

if ! git show "$base:api/openapi/v1.yaml" > "$tmp/base.yaml" 2>/dev/null; then
	echo "$base has no api/openapi/v1.yaml: nothing to compare"
	exit 0
fi
oasdiff() { go run "github.com/oasdiff/oasdiff@$version" "$@"; }
promised() { go run ./internal/tools/apisurface -tags public,driver "$1" > "$2"; }

promised "$tmp/base.yaml" "$tmp/base-promised.json"
promised api/openapi/v1.yaml "$tmp/promised.json"
echo "The public and driver operations, against $base:"
oasdiff breaking "$tmp/base-promised.json" "$tmp/promised.json" --fail-on ERR

echo "Every operation, against $base:"
if ! oasdiff breaking "$tmp/base.yaml" api/openapi/v1.yaml --fail-on ERR; then
	if [ "${API_BREAK:-false}" = true ]; then
		echo "An app operation changed incompatibly; the api-break label allows it."
	else
		echo "An app operation changed incompatibly. Label the pull request api-break once Kiflet/web is ready for it."
		exit 1
	fi
fi
