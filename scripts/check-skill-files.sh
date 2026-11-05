#!/bin/bash
# Check that the four skill programs are four distinct files.
#   scripts/check-skill-files.sh DIR   (DIR: .../libexec/basalt-shell)
# Each one has its own SELinux type (selinux/basalt_shell.fc). Hard links
# share one inode, and an inode carries one label: setfiles then picks one
# type for all four and the narrower skill domains are never entered.
# Identical copies are not enough either: rpmbuild's hard link pass
# (linkdupes) merges identical files of the build root. So the four must
# differ in content as well (make build embeds each program's name).
set -eu
dir=${1:?usage: check-skill-files.sh DIR}
names="basalt-skill basalt-skill-index basalt-skill-send basalt-skill-files"
fail=0
inodes=""
for n in $names; do
  f=$dir/$n
  [ -f "$f" ] || { echo "check-skill-files: $f is missing" >&2; fail=1; continue; }
  links=$(stat -c %h "$f")
  [ "$links" = 1 ] || { echo "check-skill-files: $f has $links hard links" >&2; fail=1; }
  inodes="$inodes $(stat -c %d:%i "$f")"
done
dups=$(printf '%s\n' $inodes | sort | uniq -d)
[ -z "$dups" ] || { echo "check-skill-files: files share an inode: $dups" >&2; fail=1; }
set -- $names
while [ $# -gt 1 ]; do
  a=$1; shift
  for b in "$@"; do
    if [ -f "$dir/$a" ] && [ -f "$dir/$b" ] && cmp -s "$dir/$a" "$dir/$b"; then
      echo "check-skill-files: $a and $b are identical (rpmbuild would hard link them)" >&2
      fail=1
    fi
  done
done
[ $fail = 0 ] && echo "check-skill-files: four distinct skill programs in $dir"
exit $fail
