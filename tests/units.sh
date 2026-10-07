#!/bin/sh
# The systemd units: the daemon must be able to run the setuid pkexec for an
# app's system dependencies, which NoNewPrivileges=yes forbids.
set -eu
here=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
if grep -Eq '^[[:space:]]*NoNewPrivileges[[:space:]]*=[[:space:]]*(yes|true|1|on)' "$here/packaging/systemd/omastored.service"; then
  echo "omastored.service sets NoNewPrivileges: pkexec (deps.install) would fail" >&2
  exit 1
fi
echo "units: ok"
