#!/bin/bash
# Demo home for the 0.4 recordings (lab VM, as root): neutral names only.
# "on" puts the test corpus aside (~/.lab-corpus, a hidden folder the
# skills never read), writes a small, realistic set of files and a demo
# mailbox (user alex, documentation domains), and switches skills.conf to
# the demo account. "off" puts everything back. Restart the session after
# either (the file index of the running shell is cached).
#   demo-home.sh on|off
set -euo pipefail
user=basalt
home=/home/$user
stash=$home/.lab-corpus
conf=$home/.config/basalt-shell/skills.conf
here=$(cd "$(dirname "$0")" && pwd)
case "${1:-}" in
on)
  if [ ! -d "$stash" ]; then
    mkdir -p "$stash"
    for d in Documents Downloads Desktop; do [ -e "$home/$d" ] && mv "$home/$d" "$stash/$d"; done
    cp -a "$conf" "$stash/skills.conf"
  fi
  rm -rf /root/demo-corpus
  python3 "$here/seed-demo.py" /root/demo-corpus
  rsync -a --no-o --no-g /root/demo-corpus/home/ "$home/"
  chown -R $user:$user "$home/Documents" "$home/Downloads" "$home/Desktop"
  restorecon -RF "$home/Documents" "$home/Downloads" "$home/Desktop"
  rm -rf /srv/lab-mail/alex/Maildir && mkdir -p /srv/lab-mail/alex/Maildir/{cur,new,tmp}
  i=0
  for f in /root/demo-corpus/mail/*.eml; do
    i=$((i+1)); cp "$f" "/srv/lab-mail/alex/Maildir/cur/$((1759100000+i)).M$i.demo:2,"
  done
  chown -R 5000:5000 /srv/lab-mail/alex
  cat > "$conf" <<'C'
# Demo account (documentation domains, lab servers).
[mail work]
host = imap.example.com
port = 143
tls = no
user = alex
password_file = ~/.config/basalt-shell/mail-lab.pass
mailbox = INBOX
address = alex.morgan@example.com
name = Alex Morgan
smtp_host = mail.example.com
smtp_port = 587
smtp_tls = none

[contacts]
names = Ana Souza, Priya Nair, Mark Chen

[files]
folders = Documents Downloads Desktop
C
  chown $user:$user "$conf"
  rm -rf "$home/.local/share/basalt-skills" "$home/.local/state/basalt-shell/moves.jsonl"
  echo "demo home on: $(find $home/Documents $home/Downloads $home/Desktop -type f | wc -l) files, $(ls /srv/lab-mail/alex/Maildir/cur | wc -l) messages"
  ;;
off)
  [ -d "$stash" ] || { echo "nothing stashed"; exit 0; }
  for d in Documents Downloads Desktop; do rm -rf "${home:?}/$d"; [ -e "$stash/$d" ] && mv "$stash/$d" "$home/$d"; done
  cp -a "$stash/skills.conf" "$conf"
  rm -rf "$stash" "$home/.local/share/basalt-skills"
  echo "lab corpus back"
  ;;
*) echo "usage: $0 on|off" >&2; exit 2 ;;
esac
