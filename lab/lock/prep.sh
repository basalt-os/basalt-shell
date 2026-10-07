#!/bin/bash
# As root inside the lock test container (lab/lock/run.sh): the lab
# person's password (lab only: "basalt-lab-pass") and the lock screen's
# PAM service, as the package installs it.
set -euo pipefail
user=${1:?user}
echo "$user:basalt-lab-pass" | chpasswd
install -m644 /src/config/pam/basalt-lock /etc/pam.d/basalt-lock
# A real name for the card.
usermod -c "Ana Basalt" "$user" 2>/dev/null || true
