#!/bin/bash
set -euo pipefail

if ! command -v go >/dev/null 2>&1; then
  echo 'Ariel: Go 1.26+ is required. Install Go before running this script.' >&2
  exit 1
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
cd "$script_dir/.."
exec go run ./cmd/arielctl "$@"
