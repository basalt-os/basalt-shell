#!/bin/bash
# Lab VM (root): make the automatic login start the Basalt session on the
# given compositor and restart greetd.
#   switch-session.sh sway|niri
set -eu
which=${1:?sway or niri}
case "$which" in sway|niri) ;; *) echo "sway or niri" >&2; exit 2 ;; esac
sed -i -E "s#^command = \"basalt-session (sway|niri)\"#command = \"basalt-session $which\"#" /etc/greetd/config.toml
sed -i -E "s#--cmd 'basalt-session (sway|niri)'#--cmd 'basalt-session $which'#" /etc/greetd/config.toml
grep -n "basalt-session" /etc/greetd/config.toml
# initial_session runs once per boot (greetd keeps a run file); forget it.
rm -f /run/greetd.run
systemctl restart greetd
