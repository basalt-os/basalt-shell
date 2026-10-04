#!/bin/bash
# Build this worktree and install it in the voice spike VM (from the
# workstation): Go binaries (static), QML, configs, the SELinux module
# sources and the lab files, staged and copied through server-home.
#   lab/voice/deploy.sh [--session]   --session also restarts the desktop session
set -euo pipefail
here=$(cd "$(dirname "$0")/../.." && pwd)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
cd "$here"
CGO_ENABLED=0 make -s build GOFLAGS="-trimpath"
CGO_ENABLED=0 go build -trimpath -o build/labweb ./lab/voice/labweb
make -s install DESTDIR="$stage/root" PREFIX=/usr SYSCONFDIR=/etc LIBEXECDIR=/usr/libexec POLKITDIR=/usr/share/polkit-1 >/dev/null
mkdir -p "$stage/root/usr/local/libexec/lab" "$stage/selinux" "$stage/lab"
cp build/labweb "$stage/root/usr/local/libexec/lab/"
cp selinux/* "$stage/selinux/"
cp -r lab/voice/. "$stage/lab/"
rm -rf "$stage/lab/labweb"
# Never overwrite the VM's voice settings once they exist.
mv "$stage/root/etc/basalt/voice.conf" "$stage/voice.conf.default"
ssh server-home 'rm -rf ~/basalt-voice/stage && mkdir -p ~/basalt-voice/stage'
rsync -a "$stage/" server-home:basalt-voice/stage/
ssh server-home 'cd ~/basalt-voice && rsync -a --delete -e "ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -i lab/vm_ed25519 -p 2251" stage/ root@127.0.0.1:/root/voice-stage/ && ./vssh "bash /root/voice-stage/lab/vm-install.sh '"${1:-}"'"'
