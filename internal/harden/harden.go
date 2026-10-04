// Package harden holds process-level hardening shared by the voice
// service and the skill workers.
package harden

import "syscall"

// prSetNoNewPrivs is PR_SET_NO_NEW_PRIVS (linux/prctl.h).
const prSetNoNewPrivs = 38

// NoNewPrivs sets no_new_privs on the calling process and its future
// children: a setuid program they might run (pkexec, newgrp, su) starts
// without its privileges, and no SELinux transition gains rights. It
// backs up the SELinux policy, which already refuses these domains any
// program but their own tools.
func NoNewPrivs() error {
	_, _, e := syscall.RawSyscall6(syscall.SYS_PRCTL, prSetNoNewPrivs, 1, 0, 0, 0, 0)
	if e != 0 {
		return e
	}
	return nil
}
