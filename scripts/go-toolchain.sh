# The official Go toolchain the shell is built and tested with. Source it,
# do not execute it.
# shellcheck shell=bash
#
# Fedora's golang can lag behind Go's security releases, so the builds use
# the official release named by go.mod's go line (also the oldest Go that
# may build the shell), downloaded from go.dev once and checked against
# the SHA-256 pinned here; nothing else about the download is trusted. The
# same version and checksum as basalt-os (scripts/go-toolchain.conf there),
# which builds the shell's RPMs. To move to a new Go release, change
# GO_VERSION, GO_SHA256 (from https://go.dev/dl/) and go.mod's go line
# together.

GO_VERSION=1.27.4
GO_SHA256=7226736c2339719cda28fbf3a54d71aa63bcefe376e98cef00034268dab24840

# go_mod_check DIR: fail unless DIR/go.mod names GO_VERSION (its toolchain
# line, or the go line when there is none).
go_mod_check() {
  local mod_go
  mod_go="$(awk '$1 == "toolchain" { sub(/^go/, "", $2); print $2; exit }' "$1/go.mod")"
  [[ -n "$mod_go" ]] || mod_go="$(awk '$1 == "go" { print $2; exit }' "$1/go.mod")"
  if [[ "$mod_go" != "$GO_VERSION" ]]; then
    echo "go.mod names Go $mod_go and scripts/go-toolchain.sh pins $GO_VERSION: they must agree" >&2
    return 1
  fi
}

# go_toolchain CACHE_DIR: download the official linux-amd64 archive of
# GO_VERSION into CACHE_DIR unless a copy with the pinned checksum is
# there, and print its path. A download whose checksum differs is
# refused and removed.
go_toolchain() {
  local cache="$1" tarball
  mkdir -p "$cache"
  tarball="$cache/go$GO_VERSION.linux-amd64.tar.gz"
  if ! echo "$GO_SHA256  $tarball" | sha256sum -c --quiet - >/dev/null 2>&1; then
    echo "downloading Go $GO_VERSION" >&2
    curl -fsSL --proto '=https' --retry 5 --retry-all-errors --retry-delay 5 -o "$tarball.part" \
      "https://go.dev/dl/go$GO_VERSION.linux-amd64.tar.gz" || { rm -f "$tarball.part"; return 1; }
    if ! echo "$GO_SHA256  $tarball.part" | sha256sum -c --quiet - >/dev/null 2>&1; then
      rm -f "$tarball.part"
      echo "Go $GO_VERSION checksum mismatch" >&2
      return 1
    fi
    mv "$tarball.part" "$tarball"
  fi
  printf '%s\n' "$tarball"
}
