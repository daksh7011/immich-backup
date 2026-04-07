#!/bin/bash
set -e

LABELS="$1"
REQUIRED_LABELS="dependency feature fix maintenance release"

for label in $REQUIRED_LABELS; do
  if echo "$LABELS" | grep -qw "$label"; then
    exit 0
  fi
done

echo "Error: PR must have one of the following labels: $REQUIRED_LABELS"
exit 1
