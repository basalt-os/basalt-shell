#!/bin/bash
# In the voice spike VM, as root: install what deploy.sh staged in
# /root/voice-stage, rebuild and load the basalt_shell SELinux module,
# and (with --session) restart the desktop session.
set -euo pipefail
st=/root/voice-stage
rsync -a "$st/root/" /
[ -f /etc/basalt/voice.conf ] || install -m 0644 "$st/voice.conf.default" /etc/basalt/voice.conf
# The voice programs and models of the lab (whisper.cpp CPU build, Piper,
# Silero VAD, the voices), from /opt/basalt-voice-lab.
lab=/opt/basalt-voice-lab
if [ -d $lab/whisper ] && [ ! -x /usr/libexec/basalt-voice/whisper/bin/whisper-cli ]; then
  mkdir -p /usr/libexec/basalt-voice /usr/share/basalt-voice/models /usr/share/basalt-voice/voices
  cp -a $lab/whisper /usr/libexec/basalt-voice/
  cp -a $lab/models/piper/piper /usr/libexec/basalt-voice/
  cp $lab/models/whisper/ggml-{tiny.en,base.en,small.en,base.en-q5_1,small.en-q5_1}.bin $lab/models/whisper/ggml-silero-v5.1.2.bin /usr/share/basalt-voice/models/
  cp $lab/models/piper/en_US-{ljspeech-medium,ljspeech-high,kristin-medium,norman-medium,john-medium}.onnx* $lab/models/piper/*.MODEL_CARD /usr/share/basalt-voice/voices/ 2>/dev/null || true
  restorecon -R /usr/libexec/basalt-voice /usr/share/basalt-voice
fi
# SELinux: build the module against the installed agent family interfaces.
rm -rf /root/voice-selinux && mkdir /root/voice-selinux && cp $st/selinux/* /root/voice-selinux/
make -s -C /root/voice-selinux -f /usr/share/selinux/devel/Makefile basalt_shell.pp >/root/voice-selinux/build.log 2>&1 || { tail -30 /root/voice-selinux/build.log; exit 1; }
semodule -i /root/voice-selinux/basalt_shell.pp
restorecon -F /usr/bin/basalt-shell /usr/bin/basalt-shelld /usr/bin/basalt-voiced /usr/libexec/basalt-shell/*
# The programs the voice and skill domains may run have types of their own (0.4.0).
restorecon -F /usr/bin/pw-cat /usr/bin/pdftotext /usr/bin/pdfinfo /usr/lib64/basalt-voice/whisper-cli 2>/dev/null || true
restorecon -RF /usr/libexec/basalt-voice /usr/lib64/chromium-browser 2>/dev/null || true
restorecon -RF /home/basalt/.local/share /run/user/1000 2>/dev/null || true
cp $st/lab/demo-tools/* /usr/local/bin/ 2>/dev/null || true
echo "installed: $(basalt-shelld version) $(basalt-voiced version)"
if [ "${1:-}" = --session ]; then
  rm -f /run/greetd.run   # initial_session (autologin) runs once per boot otherwise
  systemctl restart greetd
  echo "session restarted"
fi
