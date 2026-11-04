#!/usr/bin/env bash
# Check a candidate or the published SDK tag without using go.work.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
published=false
if [[ "${1:-}" == --published ]]; then
  published=true
  shift
fi
revision=$(git -C "$root" rev-parse --verify "${1:-HEAD}^{commit}")
go_root=$(go -C "$root" env GOROOT)
dependency_cache=$(go -C "$root" env GOMODCACHE)
export PATH="$go_root/bin:$PATH"
export GOTOOLCHAIN=local
scratch=$(mktemp -d /tmp/gramstack-sdk-release.XXXXXX)
trap 'rm -rf "$scratch"' EXIT
mkdir "$scratch/source" "$scratch/consumer"
git -C "$root" archive "$revision" sdk/go/adapter adapters/lokos | tar -xf - -C "$scratch/source"
module=github.com/theflywheel/GRAMStack/sdk/go/adapter
version=v0.1.1-rehearsal
if ! "$published"; then
  timestamp=$(git -C "$root" show -s --format=%cI "$revision")
  python3 - "$scratch" "$module" "$version" "$timestamp" <<'PY'
import json
import pathlib
import sys
import zipfile

scratch, module, version, timestamp = sys.argv[1:]
root = pathlib.Path(scratch)
source = root / "source/sdk/go/adapter"
escaped = "".join("!" + c.lower() if c.isupper() else c for c in module)
proxy = root / "proxy" / escaped / "@v"
proxy.mkdir(parents=True)
(proxy / (version + ".mod")).write_bytes((source / "go.mod").read_bytes())
(proxy / (version + ".info")).write_text(json.dumps({"Version": version, "Time": timestamp}))
with zipfile.ZipFile(proxy / (version + ".zip"), "w", zipfile.ZIP_DEFLATED) as archive:
    for file in sorted(source.rglob("*")):
        if file.is_symlink():
            raise SystemExit("SDK release contains a symbolic link")
        if file.is_file():
            archive.write(file, module + "@" + version + "/" + file.relative_to(source).as_posix())
PY
fi
export GOWORK=off
export GOMODCACHE="$scratch/cache"
export GOPROXY="file://$dependency_cache/cache/download,${GOPROXY:-https://proxy.golang.org}"
export GONOSUMDB="$module"
if "$published"; then
  version=v0.1.0
  export GONOPROXY="$module"
  export GIT_TERMINAL_PROMPT=0
else
  # The unpublished candidate is available only on the temporary file proxy.
  export GONOPROXY=none
  export GOPROXY="file://$scratch/proxy,$GOPROXY"
fi
export GOFLAGS=-modcacherw
lokos="$scratch/source/adapters/lokos"
if "$published"; then
  go -C "$lokos" mod download -json "$module@$version" > "$scratch/published.json"
  published_dir=$(python3 - "$scratch/published.json" <<'PY'
import json
import sys

with open(sys.argv[1]) as source:
    package = json.load(source)
origin = package["Origin"]
assert origin["URL"] == "https://github.com/theflywheel/GRAMStack"
assert origin["Ref"] == "refs/tags/sdk/go/adapter/v0.1.0"
assert origin["Hash"] == "bb7e25952a399f5dcbf4671c322016d048b6a032"
print(package["Dir"])
PY
)
  go -C "$published_dir" test -mod=readonly -race ./...
else
  go -C "$lokos" mod edit -require="$module@$version"
  go -C "$lokos" mod tidy
fi
test "$(go -C "$lokos" list -m -f '{{.Version}}{{if .Replace}} replaced{{end}}' "$module")" = "$version"
go -C "$lokos" test -mod=readonly -race ./...
go -C "$lokos" build -mod=readonly -o "$scratch/lokos-adapter" ./cmd/lokos-adapter

cat > "$scratch/consumer/go.mod" <<MOD
module example.invalid/external-adapter

go 1.24.0

require $module $version
MOD
cat > "$scratch/consumer/adapter_test.go" <<'GO'
package consumer

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	sdk "github.com/theflywheel/GRAMStack/sdk/go/adapter"
	"github.com/theflywheel/GRAMStack/sdk/go/adapter/conformance"
)

type provider struct{ calls int }

func (*provider) Resource() sdk.Resource {
	return sdk.Resource{Name: "example.membership", Version: "1", Fields: []string{"status", "balance"}}
}

func (p *provider) Fetch(context.Context, sdk.Subject, []string) (sdk.Data, error) {
	p.calls++
	return sdk.Data{"status": json.RawMessage(`"active"`), "balance": json.RawMessage("42")}, nil
}

func TestIndependentProviderProjection(t *testing.T) {
	p := &provider{}
	subject := sdk.Subject{Reference: "verified-source-mapping"}
	conformance.CheckProvider(t, p, subject, "status", json.RawMessage(`"active"`), func() int { return p.calls })
}
func TestIndependentAuthorityClient(t *testing.T) {
	conformance.CheckAuthorityClient(t, func(origin string, client *http.Client) (conformance.AuthorityClient, error) {
		return sdk.NewAuthorityClient(origin, client)
	})
}
GO
go -C "$scratch/consumer" mod tidy
go -C "$scratch/consumer" test -race ./...
echo "PASS: $module@$version consumed by independent LokOS and external module from $revision"
