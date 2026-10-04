#!/bin/bash
# In the voice VM, as root: the lab pieces of the acting skills (0.4),
# on top of vm-lab-setup.sh (which runs this at its end).
#   - names under the documentation domains (RFC 2606) for the lab's mail
#     servers and the demo: mail.example.com (SMTP, 10.77.0.21) and
#     imap.example.com (IMAP, 10.77.0.20), served by the lab DNS
#   - Mailpit in a container (host networking, like dovecot): an SMTP
#     sink on 10.77.0.21:587 that accepts any login and keeps every
#     message, with an API the tests read (nothing leaves the VM)
#   - a second mailbox, "alex", for the demo (neutral names; seeded by
#     demo/demo-home.sh)
set -euo pipefail
nmcli connection modify lab0 +ipv4.addresses 10.77.0.21/24 2>/dev/null || true
nmcli connection up lab0 >/dev/null
# DNS: the lab names plus the documentation names the acting tests and the demo use.
sed -i 's|--address=/imap.lab.test/10.77.0.20 |--address=/imap.lab.test/10.77.0.20 --address=/imap.example.com/10.77.0.20 --address=/mail.example.com/10.77.0.21 --address=/attacker.example.net/10.77.0.66 |' /etc/systemd/system/lab-dns.service
grep -q mail.example.com /etc/systemd/system/lab-dns.service || { echo "lab-dns.service not updated" >&2; exit 1; }
printf '[Resolve]\nDNS=127.0.0.2\nDomains=~lab.test ~example.com ~example.net ~example.org\n' > /etc/systemd/resolved.conf.d/lab.conf
systemctl daemon-reload
systemctl restart lab-dns systemd-resolved
# Mailpit (SMTP sink and API), pinned by digest of the image when pulled.
podman rm -f lab-smtp >/dev/null 2>&1 || true
podman run -d --restart=always --name lab-smtp --network host \
  -e MP_SMTP_BIND_ADDR=10.77.0.21:587 -e MP_UI_BIND_ADDR=10.77.0.21:8025 \
  -e MP_SMTP_AUTH_ACCEPT_ANY=1 -e MP_SMTP_AUTH_ALLOW_INSECURE=1 -e MP_MAX_MESSAGES=5000 \
  docker.io/axllent/mailpit:latest >/dev/null
for _ in $(seq 30); do curl -fs http://10.77.0.21:8025/api/v1/info >/dev/null && break; sleep 1; done
curl -fs http://10.77.0.21:8025/api/v1/info >/dev/null && echo "smtp sink ready: $(podman image inspect --format '{{.Digest}}' docker.io/axllent/mailpit:latest)"
# The demo mailbox user (same password file as the lab user, for simplicity).
pw=$(cat /home/basalt/.config/basalt-shell/mail-lab.pass)
grep -q '^alex:' /srv/lab-mail/users || echo "alex:{PLAIN}$pw::::::" >> /srv/lab-mail/users
mkdir -p /srv/lab-mail/alex/Maildir/{cur,new,tmp} && chown -R 5000:5000 /srv/lab-mail/alex
# dovecot also answers on the documentation name (same address).
podman restart lab-dovecot >/dev/null
echo "acting lab ready"
