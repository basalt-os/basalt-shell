#!/bin/bash
# Install a development build of the system assistant's desktop executor
# into a lab VM (run as root; development only): basalt and
# basalt-apply-exec from BIN_DIR, the units and the polkit rule from the
# assistant's source tree (packages/basalt-assistant of basalt-os), and its
# SELinux module (needs selinux-policy-devel). Originals once in
# /root/upd-orig.
#   dev-assistant.sh ASSISTANT_SRC BIN_DIR
set -euo pipefail
src=${1:?assistant source tree}
bin=${2:?directory with the built binaries}
orig=/root/upd-orig
mkdir -p "$orig"
[ -e "$orig/basalt.assistant" ] || cp -a /usr/bin/basalt "$orig/basalt.assistant"
install -m755 "$bin/basalt" /usr/bin/basalt
install -Dm755 "$bin/basalt-apply-exec" /usr/libexec/basalt-assistant/basalt-apply-exec
for u in basalt-apply@.service basalt-updates-check.service basalt-offline-finish.service; do
  install -m644 "$src/dist/$u" /usr/lib/systemd/system/$u
done
install -Dm644 "$src/dist/50-basalt-assistant.rules" /usr/share/polkit-1/rules.d/50-basalt-assistant.rules
work=$(mktemp -d)
cp "$src"/selinux/basalt_assistant.{te,if,fc} "$work/"
make -s -C "$work" -f /usr/share/selinux/devel/Makefile basalt_assistant.pp
semodule -X 400 -i "$work/basalt_assistant.pp"
rm -rf "$work"
restorecon -R /usr/bin/basalt /usr/libexec/basalt-assistant /usr/lib/systemd/system /usr/share/polkit-1/rules.d
systemctl daemon-reload
systemctl enable basalt-offline-finish.service
systemctl restart polkit
echo "installed the assistant's development executor from $src and $bin"
