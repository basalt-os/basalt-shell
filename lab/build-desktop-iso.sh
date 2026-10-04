#!/bin/bash
# Build the Basalt OS live desktop ISO for trying the desktop (lab build host).
#
#   LAB_TOP=DIR lab/build-desktop-iso.sh [keys|rpms|repo|iso|all]
#
# LAB_TOP holds:
#   os-src/     a copy of the basalt-os repository (the live desktop profile,
#               swayfx and basalt-release specs)
#   shell-src/  a copy of this repository (basalt-shell, -niri, -selinux, basalt-desktop)
# and receives lab/ (a development signing key), build/ (RPMs, ISO) and
# repo/ (the signed repository the ISO carries).
#
# The repository on the media holds the packages built from the source
# trees (os-src: release, assistant, installer, prompt and the others, plus
# swayfx; shell-src: the shell packages, not published), completed with the
# rest of https://obpkg.org/basalt (signatures checked against the
# OpenBasalt release key). Everything on the media is re-signed with the
# development key (one key per media repository); the installed system and
# the live system point at https://obpkg.org (basalt, basalt-tools) with the
# release key that basalt-release ships. Never publish this repository or ISO
# as a release: it carries a development key.
set -euo pipefail
top=${LAB_TOP:-$HOME/basalt-live-build}
os=$top/os-src
shell=$top/shell-src
lab=$top/lab
build=$top/build
repo=$top/repo
rel=44
podman=${PODMAN:-sudo podman}
fedora=registry.fedoraproject.org/fedora:$rel
log() { printf '== %s\n' "$*" >&2; }

env_file() {
  cat >"$os/.env" <<EOF
FEDORA_RELEASE=$rel
PODMAN="$podman"
LAB_DIR=$lab
BUILD_DIR=$build
REPO_DIR=$repo
GNUPGHOME_LAB=$lab/gpg
BASALT_GPG_PUBKEY=
EOF
}

keys() {
  [ -s "$lab/gpg/keyid" ] && { log "development key exists"; return; }
  install -d -m 0700 "$lab/gpg"
  (umask 077; openssl rand -base64 24 >"$lab/gpg/passphrase")
  GNUPGHOME=$lab/gpg gpg --batch --pinentry-mode loopback --passphrase-file "$lab/gpg/passphrase" \
    --quick-gen-key "Basalt OS desktop preview (development key, not the release key) <dev@basalt-os.invalid>" ed25519 sign 1y
  GNUPGHOME=$lab/gpg gpg --batch --with-colons --list-keys | awk -F: '/^fpr:/ {print $10; exit}' >"$lab/gpg/keyid"
  gpgconf --homedir "$lab/gpg" --kill gpg-agent 2>/dev/null || true
  log "development key $(cat "$lab/gpg/keyid")"
}

rpms() {
  env_file
  rm -rf "$build/rpms"
  "$os/scripts/build-rpms.sh"
  "$os/packages/swayfx/build.sh"
  (cd "$shell" && rm -f build/rpm/*.rpm && PODMAN="$podman" BASALT_AGENT_SELINUX="$os/packages/basalt-agent/selinux" scripts/build-rpm.sh "$rel")
}

repo() {
  env_file
  local stage=$build/stage obpkg=$build/obpkg
  rm -rf "$stage" "$obpkg"; mkdir -p "$stage" "$obpkg"
  log "download https://obpkg.org/basalt and check every signature against the release key"
  $podman run --rm --network=host --security-opt label=disable -v "$obpkg:/out" -v "$os/packages/basalt-release:/rel:ro" "$fedora" bash -euc "
    dnf -q -y install 'dnf5-command(download)' >/dev/null 2>&1 || true
    rpmkeys --import /rel/RPM-GPG-KEY-basalt
    r=\"-y -q --disablerepo=* --repofrompath=obpkg,https://obpkg.org/basalt/$rel/x86_64/ --setopt=obpkg.gpgcheck=1 --setopt=obpkg.repo_gpgcheck=1 --setopt=obpkg.gpgkey=file:///rel/RPM-GPG-KEY-basalt\"
    names=\$(dnf \$r repoquery --latest-limit=1 --qf '%{name}\\n' | sort -u)
    dnf \$r download --destdir /out \$names >/dev/null
    for f in /out/*.rpm; do rpmkeys --checksig \"\$f\" | grep -q 'digests signatures OK' || { echo \"bad signature: \$f\" >&2; exit 1; }; done
    ls /out | wc -l"
  # Packages built from the source tree win; obpkg.org fills in the rest
  # (basalt-llm and the data packages, which the tree does not rebuild here).
  local rpmdir=$build/rpms/$rel f n
  find "$rpmdir" -maxdepth 1 -name '*.rpm' ! -name '*.src.rpm' ! -name '*-debuginfo-*' ! -name '*-debugsource-*' -exec cp {} "$stage/" \;
  cp "$shell"/build/rpm/*.rpm "$stage/"
  local have; have=$(for f in "$stage"/*.rpm; do rpm -qp --qf '%{name}\n' "$f" 2>/dev/null; done | sort -u)
  for f in "$obpkg"/*.rpm; do
    n=$(rpm -qp --qf '%{name}' "$f" 2>/dev/null)
    grep -qx "$n" <<<"$have" || cp "$f" "$stage/"
  done
  log "strip the signatures (the media repository is signed with one key)"
  $podman run --rm --network=host --security-opt label=disable -v "$stage:/s" "$fedora" bash -euc '
    dnf -q -y install rpm-sign >/dev/null 2>&1 || { echo "dnf install rpm-sign failed" >&2; exit 1; }
    rpmsign --delsign /s/*.rpm >/dev/null'
  sudo rm -rf "$repo"; mkdir -p "$repo"
  "$os/scripts/repo.sh" publish "$stage"/*.rpm
  ls "$repo/$rel/x86_64" | sed 's/^/  /' >&2
}

iso() {
  env_file
  local plans=$build/plans
  mkdir -p "$plans"
  cat >"$plans/default.yaml" <<'EOF'
# Basalt OS desktop edition, from the live desktop. The installer still
# shows the full review and asks for the disk name before writing anything.
apiVersion: basalt-install-plan/v1
edition: desktop
target:
  disk: /dev/vda
  wipe: false
layout:
  mode: automatic
encryption:
  enabled: true
  unlock: tpm2
profile: auto
hostname: basalt
repos:
  basalt:
    url: media
packages:
  extra:
    - basalt-desktop
    - basalt-shell-selinux
finish: reboot
EOF
  LIVE_PROFILE=desktop LIVE_PLANS=$plans "$os/packages/basalt-installer/live/build-live.sh"
}

case "${1:-all}" in
  keys) keys ;;
  rpms) rpms ;;
  repo) repo ;;
  iso) iso ;;
  all) keys; rpms; repo; iso ;;
  *) sed -n 2,20p "$0"; exit 2 ;;
esac
