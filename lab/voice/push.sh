#!/bin/bash
# Copy lab/voice (scripts, tests, corpus) to the voice VM without a full
# deploy: lab/voice/push.sh
set -euo pipefail
LAB_HOST=${LAB_HOST:?set LAB_HOST to the ssh destination of the lab host}
here=$(cd "$(dirname "$0")" && pwd)
rsync -a --exclude labweb "$here/" "$LAB_HOST":basalt-voice/stage/lab/
ssh "$LAB_HOST" 'cd ~/basalt-voice && rsync -a -e "ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -i lab/vm_ed25519 -p 2251" stage/lab/ root@127.0.0.1:/root/voice-stage/lab/ && ./vssh "mkdir -p /home/basalt/voice-lab && rsync -a /root/voice-stage/lab/ /home/basalt/voice-lab/ && chown -R basalt:basalt /home/basalt/voice-lab && cp /root/voice-stage/lab/demo-tools/* /usr/local/bin/"'
