#!/bin/bash
set -e

LABELS="$1"
VERSION_LABELS="major minor patch"

for label in $VERSION_LABELS; do
  if echo "$LABELS" | grep -qwi "$label"; then
    exit 0
  fi
done

echo "Error: PR targeting master must have one of the following labels: $VERSION_LABELS"
exit 1
