#!/usr/bin/env bash
# This repository's files are published as open source. Fail when a tracked
# file carries internal references: ticket ids, links into the internal
# tracker, or internal environment hosts.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
# No \b: git grep -E uses the platform's POSIX regex, which may not support it.
pattern='(^|[^A-Za-z0-9])(S9N|AGT|KMV|INC)-[0-9]+|notion\.(so|com)|staging\.(apps\.)?s9n\.ai'
if git grep -n -I -E "$pattern" -- . ':!scripts/check-public-hygiene.sh'; then
  echo "::error::internal references found above; remove them before merging" >&2
  exit 1
fi
echo "no internal references found"
