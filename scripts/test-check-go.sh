#!/usr/bin/env bash
# Verify that parallel checks cannot turn a failing test/vet into green CI.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
cat > "$scratch/go" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
module=$2
operation=$3
printf '%s %s\n' "$module" "$operation" >> "$CI_RUNNER_TRACE"
if [[ "$operation" == build ]]; then touch "$CI_RUNNER_TRACE.unsafe-build"; exit 99; fi
if [[ "$module" == *"$CI_RUNNER_FAIL_MODULE" && "$operation" == "$CI_RUNNER_FAIL_OPERATION" ]]; then exit 42; fi
FAKE
chmod +x "$scratch/go"
for fault in 'services/gateway test' 'sdk/go/adapter test' 'sdk/go/adapter vet' 'adapters/auth/oidc test' 'adapters/auth/oidc vet' 'mocks/lokos test' 'mocks/lokos vet' 'apps/sakhicredit test' 'apps/sakhicredit vet'; do
  read -r fail_module fail_operation <<< "$fault"
  export CI_RUNNER_FAIL_MODULE="$fail_module" CI_RUNNER_FAIL_OPERATION="$fail_operation" CI_RUNNER_TRACE="$scratch/trace"
  : > "$CI_RUNNER_TRACE"
  if PATH="$scratch:$PATH" "$root/scripts/check-go.sh" > "$scratch/output" 2>&1; then
    echo "Parallel runner swallowed $fault failure" >&2; exit 1
  fi
  if [[ -e "$CI_RUNNER_TRACE.unsafe-build" ]]; then echo 'Runner advanced past a failed verification gate' >&2; exit 1; fi
  grep -q 'services/gateway test' "$CI_RUNNER_TRACE"
  grep -q 'sdk/go/adapter test' "$CI_RUNNER_TRACE"
done
echo 'Parallel runner propagates test/vet failures and waits for both workers.'
