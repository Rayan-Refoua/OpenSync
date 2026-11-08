#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

mode="${1:-targeted}"
log="$(mktemp)"
trap 'rm -f "$log"' EXIT

run() {
    local label="$1"
    shift
    if "$@" >"$log" 2>&1; then
        return 0
    fi
    printf '\nFAILED: %s\n' "$label"
    cat "$log"
    exit 1
}

case "$mode" in
    targeted)
        run tests go test ./internal/premierexml ./cmd/opensync
        ;;
    full)
        run tests go test ./...
        run vet go vet ./...
        run patch-hygiene git diff --check
        ;;
    *)
        echo "Usage: tools/check.sh [targeted|full]" >&2
        exit 2
        ;;
esac

printf 'CHECK: PASS (%s)\n' "$mode"
