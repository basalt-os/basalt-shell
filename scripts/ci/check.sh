#!/usr/bin/env bash
# Checks for CI and for local use.
#
#   scripts/ci/check.sh              run the checks here (needs go, make; node,
#                                    qmllint, shellcheck and actionlint optional)
#   scripts/ci/check.sh --container  run them in a throwaway Fedora container that
#                                    has every tool (what CI does)
#
# Checks: go vet and go test (make test: the translation catalogs, the QML
# label and model checks, the command bar's chips), the JavaScript tests
# (Node's test runner), qmllint on the shell's and the greeter's QML (syntax
# and structure: Quickshell is not installed, so unresolved types are
# warnings and do not fail), ShellCheck on the CI scripts, actionlint on the
# workflows.
set -euo pipefail

FEDORA_RELEASE=${FEDORA_RELEASE:-44}
FEDORA_IMAGE=${FEDORA_IMAGE:-registry.fedoraproject.org/fedora:$FEDORA_RELEASE}
PODMAN=${PODMAN:-podman}
REPO_ROOT=$(cd "$(dirname "$0")/../.." && pwd)

# actionlint, pinned by version and SHA-256 of the release archive (the same
# as basalt-os's CI).
ACTIONLINT_VERSION=1.7.12
ACTIONLINT_SHA256=8aca8db96f1b94770f1b0d72b6dddcb1ebb8123cb3712530b08cc387b349a3d8

if [[ "${1:-}" == --container ]]; then
  echo "running the checks in $FEDORA_IMAGE" >&2
  # shellcheck disable=SC2086 # PODMAN may be "sudo podman"
  $PODMAN run --rm --network=host --security-opt label=disable -v "$REPO_ROOT:/src:ro" -w /src \
    -e CI -e ACTIONLINT_VERSION="$ACTIONLINT_VERSION" -e ACTIONLINT_SHA256="$ACTIONLINT_SHA256" \
    "$FEDORA_IMAGE" bash -euc '
      dnf -q -y install make golang nodejs qt6-qtdeclarative-devel ShellCheck tar gzip git >/dev/null 2>&1 ||
        { echo "dnf install failed" >&2; exit 1; }
      tmp=$(mktemp -d)
      url="https://github.com/rhysd/actionlint/releases/download/v${ACTIONLINT_VERSION}/actionlint_${ACTIONLINT_VERSION}_linux_amd64.tar.gz"
      if curl -fsSL --retry 5 --retry-all-errors --retry-delay 5 -o "$tmp/a.tgz" "$url" && echo "$ACTIONLINT_SHA256  $tmp/a.tgz" | sha256sum -c --quiet -; then
        tar -xzf "$tmp/a.tgz" -C /usr/local/bin actionlint
      else
        echo "actionlint download or checksum failed" >&2; exit 1
      fi
      # The tree is mounted read-only: work on a copy (go test and make write).
      cp -a /src /tmp/src && cd /tmp/src && scripts/ci/check.sh'
  exit $?
fi

cd "$REPO_ROOT"
fail=0
step() { printf '\n--- %s\n' "$*" >&2; }

step "go vet, go test, JavaScript tests (make test)"
if ! command -v node >/dev/null 2>&1 && [[ -n "${CI:-}" ]]; then
  echo "node not found: the JavaScript tests would be skipped" >&2; fail=1
fi
make --no-print-directory test || fail=1

step "qmllint (shell, greeter)"
qmllint=$(command -v qmllint qmllint-qt6 /usr/lib64/qt6/bin/qmllint 2>/dev/null | head -1 || true)
if [[ -n "$qmllint" ]]; then
  # Only errors fail: without Quickshell's type information every
  # Quickshell type and property is an unresolved warning.
  for dir in shell greeter; do
    out=$("$qmllint" --max-warnings -1 "$dir"/*.qml 2>&1) || { printf '%s\n' "$out" | grep -E -B1 -A2 '\[syntax\]|Error' >&2 || printf '%s\n' "$out" | tail -20 >&2; fail=1; }
  done
else
  echo "qmllint not found: skipped" >&2
  [[ -z "${CI:-}" ]] || fail=1
fi

step "ShellCheck: CI scripts"
if command -v shellcheck >/dev/null 2>&1; then
  shellcheck -x -S warning scripts/ci/*.sh || fail=1
else
  echo "shellcheck not found: skipped" >&2
fi

step "actionlint: workflows"
if command -v actionlint >/dev/null 2>&1; then
  actionlint || fail=1
else
  echo "actionlint not found: skipped" >&2
fi

if (( fail )); then
  echo "checks failed" >&2
  exit 1
fi
echo "all checks passed" >&2
