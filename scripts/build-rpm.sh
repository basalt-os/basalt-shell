#!/bin/bash
# Build the basalt-shell RPM in a clean Fedora container.
#   scripts/build-rpm.sh [FEDORA_RELEASE]   -> build/rpm/*.rpm
set -eu
cd "$(dirname "$0")/.."
rel=${1:-44}
ver=$(cat VERSION)
podman=${PODMAN:-podman}
mkdir -p build/rpm
tar --transform "s,^\.,basalt-shell-$ver," --exclude=./build --exclude=./.git --exclude='./media' --exclude='*.local.md' \
  -czf "build/rpm/basalt-shell-$ver.tar.gz" .
$podman run --rm --net=host -v "$PWD/build/rpm:/out:Z" -v "$PWD/packaging:/spec:ro,Z" \
  "registry.fedoraproject.org/fedora:$rel" bash -euc "
    dnf -y -q install rpm-build golang make systemd-rpm-macros >/dev/null
    mkdir -p ~/rpmbuild/SOURCES && cp /out/basalt-shell-$ver.tar.gz ~/rpmbuild/SOURCES/
    rpmbuild -bb --define 'basalt_version $ver' /spec/basalt-shell.spec >/out/rpmbuild.log 2>&1 || { tail -40 /out/rpmbuild.log; exit 1; }
    cp ~/rpmbuild/RPMS/*/*.rpm /out/"
ls -1 build/rpm/*.rpm
