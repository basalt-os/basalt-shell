#!/bin/bash
# The voice and read-only skills spike VM (basalt-voice-vm1): a copy of the
# desktop lab VM's disk, run by the lab user's QEMU with 3D (virgl through
# egl-headless) and its own software TPM (swtpm). Never the host TPM.
#
#   run-voice-vm.sh start | stop | status
#
# Settings come from $LAB_TOP/.env (VOICE_VM_NAME, VOICE_SSH_PORT,
# VOICE_VNC_DISPLAY, VOICE_SPICE_PORT, VOICE_LISTEN, VOICE_VCPUS,
# VOICE_MEMORY_MB, VOICE_MAC). The disk and firmware variables are in
# $LAB_TOP/vm, the TPM state in $LAB_TOP/vm/tpm. QEMU user networking (own
# NAT, isolated from other labs).
set -euo pipefail
top=${LAB_TOP:-$(pwd)}
[ -f "$top/.env" ] && { set -a; . "$top/.env"; set +a; }
name=${VOICE_VM_NAME:-basalt-voice-vm1}
vm=$top/vm
listen=${VOICE_LISTEN:-127.0.0.1}

monitor() { python3 - "$vm/monitor.sock" "$1" <<'PY'
import socket, sys, time
s = socket.socket(socket.AF_UNIX); s.connect(sys.argv[1]); time.sleep(0.2); s.recv(65536)
s.sendall((sys.argv[2] + "\n").encode()); time.sleep(0.5); print(s.recv(65536).decode(errors="replace"))
PY
}

start() {
  mkdir -p "$vm/tpm"
  systemctl --user is-active --quiet "$name-tpm" || systemd-run --user --unit="$name-tpm" --collect \
    swtpm socket --tpm2 --tpmstate dir="$vm/tpm" --ctrl type=unixio,path="$vm/tpm/sock" \
      --log file="$vm/tpm/log",level=1
  for _ in $(seq 20); do [ -S "$vm/tpm/sock" ] && break; sleep 0.25; done
  systemd-run --user --unit="$name" --collect \
    qemu-system-x86_64 -name "$name" \
      -machine q35,accel=kvm,smm=on -cpu host -smp "${VOICE_VCPUS:-4}" -m "${VOICE_MEMORY_MB:-7168}" \
      -global driver=cfi.pflash01,property=secure,value=on \
      -drive if=pflash,format=qcow2,readonly=on,file=/usr/share/edk2/ovmf/OVMF_CODE_4M.secboot.qcow2 \
      -drive if=pflash,format=qcow2,file="$vm/vars.qcow2" \
      -drive file="$vm/disk.qcow2",if=virtio,format=qcow2,discard=unmap \
      -chardev socket,id=chrtpm,path="$vm/tpm/sock" -tpmdev emulator,id=tpm0,chardev=chrtpm -device tpm-tis,tpmdev=tpm0 \
      -device virtio-vga-gl,xres=1920,yres=1080 -display egl-headless,rendernode="${VOICE_RENDERNODE:-/dev/dri/renderD128}" \
      -vnc "$listen:${VOICE_VNC_DISPLAY:-53},password=on" \
      -object secret,id=spicepw,file="$top/lab/display.pass" \
      -spice "port=${VOICE_SPICE_PORT:-5954},addr=$listen,password-secret=spicepw" \
      -device virtio-tablet-pci -device virtio-keyboard-pci \
      -netdev "user,id=n0,hostfwd=tcp:127.0.0.1:${VOICE_SSH_PORT:-2251}-:22" \
      -device virtio-net-pci,netdev=n0,mac="${VOICE_MAC:-52:54:00:93:00:0c}" \
      -audiodev spice,id=snd0 -device ich9-intel-hda -device hda-duplex,audiodev=snd0 \
      -monitor unix:"$vm/monitor.sock",server,nowait \
      -serial file:"$vm/serial.log"
  for _ in $(seq 20); do [ -S "$vm/monitor.sock" ] && break; sleep 0.5; done
  monitor "set_password vnc $(head -c 8 "$top/lab/display.pass")" >/dev/null
  echo "started $name: VNC $listen:$((5900 + ${VOICE_VNC_DISPLAY:-53})), SPICE $listen:${VOICE_SPICE_PORT:-5954}, SSH 127.0.0.1:${VOICE_SSH_PORT:-2251}"
}

case "${1:-}" in
  start) start ;;
  stop) monitor system_powerdown >/dev/null || true ;;
  status) systemctl --user --no-pager status "$name" "$name-tpm" | head -20 ;;
  *) echo "usage: $0 start|stop|status" >&2; exit 2 ;;
esac
