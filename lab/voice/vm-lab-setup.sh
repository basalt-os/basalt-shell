#!/bin/bash
# In the voice spike VM, as root, once: the lab around the skills.
#   - a dummy interface lab0 with the lab sites' addresses (10.77.0.0/24)
#   - DNS for *.lab.test (dnsmasq on 127.0.0.2:53, routed by
#     systemd-resolved), not /etc/hosts: basalt-resolver must see the names
#   - labweb: the lab web server (news, shop and the attacker's site),
#     every request logged to /var/log/lab-web.jsonl
#   - dovecot in a container on 10.77.0.20:143 with the seeded mailbox
#   - the corpus files in /home/basalt, the skills config, the mail password
#   - the local model for summaries (qwen3-1.7b, 8k context)
#   - a lab microphone: a PipeWire pipe source fed by lab-say
set -euo pipefail
st=/root/voice-stage/lab
user=basalt
home=/home/$user
dnf -y -q install dnsmasq >/dev/null
# The assistant demo nginx of the desktop lab would hold port 80.
systemctl disable --now nginx >/dev/null 2>&1 || true
# Addresses.
if ! nmcli -t -f NAME connection show | grep -qx lab0; then
  nmcli connection add type dummy ifname lab0 con-name lab0 ipv4.method manual \
    ipv4.addresses 10.77.0.10/24,10.77.0.11/24,10.77.0.20/24,10.77.0.66/24 ipv6.method disabled >/dev/null
fi
nmcli connection up lab0 >/dev/null
# DNS for the lab names.
cat > /etc/systemd/system/lab-dns.service <<'U'
[Unit]
Description=Lab DNS for *.lab.test (voice spike)
After=network.target
[Service]
ExecStart=/usr/sbin/dnsmasq --keep-in-foreground --no-resolv --no-hosts --port=53 --listen-address=127.0.0.2 --bind-interfaces \
  --address=/news.lab.test/10.77.0.10 --address=/shop.lab.test/10.77.0.11 --address=/imap.lab.test/10.77.0.20 \
  --address=/evil.lab.test/10.77.0.66 --address=/lab.test/10.77.0.11 --log-queries --log-facility=/var/log/lab-dns.log
[Install]
WantedBy=multi-user.target
U
mkdir -p /etc/systemd/resolved.conf.d
printf '[Resolve]\nDNS=127.0.0.2\nDomains=~lab.test\n' > /etc/systemd/resolved.conf.d/lab.conf
# Web server.
mkdir -p /srv/lab-web
cat > /etc/systemd/system/lab-web.service <<'U'
[Unit]
Description=Lab web server (voice spike)
After=network-online.target NetworkManager.service
[Service]
ExecStart=/usr/local/libexec/lab/labweb -root /srv/lab-web -log /var/log/lab-web.jsonl -listen 10.77.0.10:80,10.77.0.11:80,10.77.0.66:80,10.77.0.66:443
Restart=on-failure
[Install]
WantedBy=multi-user.target
U
systemctl daemon-reload
systemctl enable --now lab-dns lab-web >/dev/null
systemctl restart systemd-resolved
# ydotool (the lab microphone holds Super+V through it) needs uinput after a reboot.
echo uinput > /etc/modules-load.d/lab-uinput.conf; modprobe uinput || true
# Corpus.
rm -rf /root/corpus && python3 $st/corpus/make-corpus.py /root/corpus >/dev/null
rsync -a --delete /root/corpus/web/ /srv/lab-web/
mkdir -p $home/voice-lab && cp /root/corpus/cases.json $home/voice-lab/cases.json && chown -R $user:$user $home/voice-lab
# Mailbox.
pass_file=$home/.config/basalt-shell/mail-lab.pass
mkdir -p $home/.config/basalt-shell /srv/lab-mail
if [ ! -s $pass_file ]; then head -c 12 /dev/urandom | base64 | tr -d '/+=' > $pass_file; fi
pw=$(cat $pass_file)
echo "dev:{PLAIN}$pw::::::" > /srv/lab-mail/users
rm -rf /srv/lab-mail/dev && mkdir -p /srv/lab-mail/dev/Maildir/{cur,new,tmp}
i=0
for f in /root/corpus/mail/*.eml; do
  i=$((i+1)); cp "$f" "/srv/lab-mail/dev/Maildir/cur/$((1759000000+i)).M$i.lab:2,"
done
chown -R 5000:5000 /srv/lab-mail/dev; chmod 0644 /srv/lab-mail/users
podman build -q -t localhost/lab-dovecot $st/dovecot >/dev/null
podman rm -f lab-dovecot >/dev/null 2>&1 || true
# Host networking: a published port would be DNATed to the container address
# before the session filter sees it (the filter would drop it, fail closed).
podman run -d --restart=always --name lab-dovecot --network host -v /srv/lab-mail:/srv/mail:Z localhost/lab-dovecot >/dev/null
systemctl enable podman-restart.service >/dev/null 2>&1 || true
# Home files of the session user.
rsync -a --no-o --no-g --no-perms --chmod=ugo=rwX,go-w /root/corpus/home/ $home/
chown $user:$user $home $home/.ssh $home/.config/lab-secret
ln -sfn $home/.ssh $home/Documents/keys
chmod 700 $home/.ssh $home/.config/lab-secret; chmod 600 $home/.ssh/id_ed25519 $home/.config/lab-secret/token.txt $pass_file
cat > $home/.config/basalt-shell/skills.conf <<'C'
# Read-only skills (voice spike lab).
[mail lab]
host = imap.lab.test
port = 143
tls = no
user = dev
password_file = ~/.config/basalt-shell/mail-lab.pass
mailbox = INBOX
# Replies (always confirmed) go to the lab SMTP sink (Mailpit).
address = dev@lab.test
name = Dev Lab
smtp_host = mail.example.com
smtp_port = 587
smtp_tls = none

[site news]
url = http://news.lab.test/
names = lab news, news site, news page

[files]
folders = Documents Downloads Desktop
C
chown -R $user:$user $home/Documents $home/Downloads $home/Desktop $home/.config $home/.ssh/id_ed25519
restorecon -RF $home >/dev/null 2>&1 || true
# The model for summaries: the untuned 1.7B (the fine-tuned translators
# are trained for the system assistant's commands), 8k context.
sed -i 's|^MODEL=.*|MODEL=/var/lib/basalt-llm/models/qwen3-1.7b-q8_0.gguf|; s|^CTX_SIZE=.*|CTX_SIZE=8192|; s|^CACHE_RAM=.*|CACHE_RAM=512|' /etc/basalt/llm.conf
systemctl restart basalt-llm
systemctl enable --now basalt-resolver basalt-ledger >/dev/null 2>&1 || true
# The lab microphone (a PipeWire source fed from a pipe) for the session user.
mkdir -p $home/.config/systemd/user
cat > $home/.config/systemd/user/lab-mic.service <<'U'
[Unit]
Description=Lab microphone: a PipeWire source fed by lab-say (voice spike)
After=pipewire-pulse.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c 'rm -f %t/lab-mic.fifo; pactl load-module module-pipe-source source_name=lab_mic file=%t/lab-mic.fifo format=s16le rate=16000 channels=1 source_properties=device.description=Lab-microphone && pactl set-default-source lab_mic'
ExecStop=/bin/sh -c 'pactl unload-module module-pipe-source'
[Install]
WantedBy=default.target
U
chown -R $user:$user $home/.config/systemd
runuser -u $user -- env XDG_RUNTIME_DIR=/run/user/1000 systemctl --user daemon-reload
runuser -u $user -- env XDG_RUNTIME_DIR=/run/user/1000 systemctl --user enable --now lab-mic.service >/dev/null 2>&1 || true
echo "lab ready: $(ls /srv/lab-mail/dev/Maildir/cur | wc -l) messages, $(find $home/Documents $home/Downloads $home/Desktop -type f | wc -l) files"
# The acting skills' lab (SMTP sink, documentation names, demo mailbox).
bash $st/vm-lab-acting.sh

