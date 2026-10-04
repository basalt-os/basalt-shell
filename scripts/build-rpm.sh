#!/bin/bash
# Build the basalt-shell RPMs in a clean Fedora container.
#   scripts/build-rpm.sh [FEDORA_RELEASE]   -> build/rpm/*.rpm
# The SELinux subpackage is built against the agent family's base module:
# BASALT_AGENT_SELINUX=DIR with basalt_agent_base.if (default: the
# basalt-os checkout next to this one), used in place of the
# basalt-agent-selinux build dependency.
set -eu
cd "$(dirname "$0")/.."
rel=${1:-44}
ver=$(cat VERSION)
podman=${PODMAN:-podman}
# basalt_agent_base: BASALT_AGENT_SELINUX, else a basalt-os checkout next
# to this repository (github.com/basalt-os/basalt-os; os-src in the lab).
base=${BASALT_AGENT_SELINUX:-}
if [ -z "$base" ]; then
  for d in ../basalt-os ../os-src ../basalt-os-image; do
    [ -f "$d/packages/basalt-agent/selinux/basalt_agent_base.if" ] && { base=$d/packages/basalt-agent/selinux; break; }
  done
fi
[ -f "$base/basalt_agent_base.if" ] || { echo "basalt_agent_base.if not found in $base (set BASALT_AGENT_SELINUX)" >&2; exit 1; }
mkdir -p build/rpm
cp "$base/basalt_agent_base.if" build/rpm/
tar --transform "s,^\.,basalt-shell-$ver," --exclude=./build --exclude=./.git --exclude='./media' --exclude='*.local.md' \
  -czf "build/rpm/basalt-shell-$ver.tar.gz" .
$podman run --rm --net=host -v "$PWD/build/rpm:/out:Z" -v "$PWD/packaging:/spec:ro,Z" \
  "registry.fedoraproject.org/fedora:$rel" bash -euc "
    dnf -y -q install rpm-build golang make systemd-rpm-macros selinux-policy-devel bzip2 >/dev/null
    cp /out/basalt_agent_base.if /usr/share/selinux/devel/include/distributed/
    mkdir -p ~/rpmbuild/SOURCES && cp /out/basalt-shell-$ver.tar.gz ~/rpmbuild/SOURCES/
    rpmbuild -bb --nodeps --define 'basalt_version $ver' /spec/basalt-shell.spec >/out/rpmbuild.log 2>&1 || { tail -40 /out/rpmbuild.log; exit 1; }
    cp ~/rpmbuild/RPMS/*/*.rpm /out/"
ls -1 build/rpm/*.rpm
