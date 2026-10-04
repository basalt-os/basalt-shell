#!/bin/bash
# Copy lab/voice (scripts, tests, corpus) to the voice VM without a full
# deploy: lab/voice/push.sh
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
rsync -a --exclude labweb "$here/" server-home:basalt-voice/stage/lab/
ssh server-home 'cd ~/basalt-voice && rsync -a -e "ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -i lab/vm_ed25519 -p 2251" stage/lab/ root@127.0.0.1:/root/voice-stage/lab/ && ./vssh "mkdir -p /home/basalt/voice-lab && rsync -a /root/voice-stage/lab/ /home/basalt/voice-lab/ && chown -R basalt:basalt /home/basalt/voice-lab && cp /root/voice-stage/lab/demo-tools/* /usr/local/bin/"'
