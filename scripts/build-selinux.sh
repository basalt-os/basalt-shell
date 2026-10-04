#!/bin/bash
# Build the basalt_shell SELinux module in a Fedora container with
# selinux-policy-devel, against the agent family's base module
# basalt_agent_base (basalt-os repository, packages/basalt-agent/selinux; the
# RPM build takes it from basalt-agent-selinux). The base module is built
# too, for lab installs.
#   scripts/build-selinux.sh [FEDORA_RELEASE]   -> build/selinux/*.pp
#   BASALT_AGENT_SELINUX=DIR   where basalt_agent_base.{te,if,fc} are
set -eu
cd "$(dirname "$0")/.."
rel=${1:-44}
podman=${PODMAN:-podman}
# basalt_agent_base: BASALT_AGENT_SELINUX, else a basalt-os checkout next
# to this repository (github.com/basalt-os/basalt-os; os-src in the lab).
base=${BASALT_AGENT_SELINUX:-}
if [ -z "$base" ]; then
  for d in ../basalt-os ../os-src ../basalt-os-image; do
    [ -f "$d/packages/basalt-agent/selinux/basalt_agent_base.if" ] && { base=$d/packages/basalt-agent/selinux; break; }
  done
fi
[ -f "$base/basalt_agent_base.if" ] || { echo "basalt_agent_base not found in $base (set BASALT_AGENT_SELINUX)" >&2; exit 1; }
rm -rf build/selinux && mkdir -p build/selinux
cp selinux/*.te selinux/*.if selinux/*.fc "$base"/basalt_agent_base.{te,if,fc} build/selinux/
$podman run --rm --net=host -v "$PWD/build/selinux:/m:Z" "registry.fedoraproject.org/fedora:$rel" bash -euc '
  dnf -y -q install selinux-policy-devel make >/dev/null
  cd /m && make -f /usr/share/selinux/devel/Makefile basalt_agent_base.pp basalt_shell.pp
  rpm -q selinux-policy-devel > policy-version.txt'
ls -1 build/selinux/*.pp
