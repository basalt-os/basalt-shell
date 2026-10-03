#!/bin/bash
# Lab VM (root): put nginx in the "broken config" state the system
# assistant diagnoses (an unknown directive), with a snapshot that holds
# the good copy. Run again to re-break after a demo fixed it.
set -eu
rpm -q nginx >/dev/null || dnf -y -q install nginx
[ -f /root/nginx.conf.orig ] || cp /etc/nginx/nginx.conf /root/nginx.conf.orig
cp /root/nginx.conf.orig /etc/nginx/nginx.conf
systemctl enable --now nginx >/dev/null 2>&1 || true
snapper -c root create --description "lab: nginx good" >/dev/null
sed -i "0,/http {/s//http {\n    bogus_directive on;/" /etc/nginx/nginx.conf
systemctl restart nginx 2>/dev/null || true
systemctl is-failed nginx
for p in $(basalt pending --json | jq -r '.[] | select(.kind=="unit") | .id'); do basalt ignore "$p" --reason "lab reset" >/dev/null || true; done
