package shell

import "testing"

func TestUICheck(t *testing.T) {
	se := UICheck{Mode: UICheckSELinux, Types: DefaultUITypes, Executables: DefaultUIExecutables}
	cases := []struct {
		p  Peer
		ok bool
	}{
		{Peer{Type: "basalt_shell_ui_t", Exe: "/usr/bin/quickshell"}, true},
		// Same user, same program name, outside the shell UI's domain.
		{Peer{Type: "unconfined_t", Exe: "/tmp/x/quickshell"}, false},
		{Peer{Type: "basalt_agent_mcp_t", Exe: "/usr/bin/basalt-shell"}, false},
		// The UI domain running something else than Quickshell.
		{Peer{Type: "basalt_shell_ui_t", Exe: "/usr/bin/python3"}, false},
	}
	for _, c := range cases {
		if err := se.Allows(c.p); (err == nil) != c.ok {
			t.Errorf("selinux %+v: %v", c.p, err)
		}
	}
	exe := UICheck{Mode: UICheckExe, Executables: DefaultUIExecutables}
	// The weakness the SELinux mode removes: any program named quickshell passes.
	if exe.Allows(Peer{Type: "unconfined_t", Exe: "/tmp/x/quickshell"}) != nil {
		t.Error("exe mode")
	}
	if (UICheck{Mode: UICheckNone}).Allows(Peer{Type: "basalt_shell_ui_t", Exe: "/usr/bin/quickshell"}) == nil {
		t.Error("none mode allowed")
	}
	if got := contextType("unconfined_u:unconfined_r:basalt_shell_ui_t:s0-s0:c0.c1023"); got != "basalt_shell_ui_t" {
		t.Errorf("contextType %q", got)
	}
}
