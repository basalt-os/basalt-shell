package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMoveInsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(root, "a.pdf"), []byte("a"), 0o644))
	must(os.WriteFile(filepath.Join(root, "b.pdf"), []byte("b"), 0o644))
	must(os.WriteFile(filepath.Join(root, "taken.pdf"), []byte("t"), 0o644))
	must(os.Symlink(outside, filepath.Join(root, "link")))

	// A plain move into a new folder.
	r := move(MoveJob{Root: root, Create: []string{filepath.Join(root, "Bank")},
		Moves: []moveItem{{From: filepath.Join(root, "a.pdf"), To: filepath.Join(root, "Bank", "a.pdf")}}})
	if !r.OK || len(r.Created) != 1 {
		t.Fatalf("move failed: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(root, "Bank", "a.pdf")); err != nil {
		t.Fatal(err)
	}
	// Never replace an existing file.
	r = move(MoveJob{Root: root, Moves: []moveItem{{From: filepath.Join(root, "b.pdf"), To: filepath.Join(root, "taken.pdf")}}})
	if r.OK {
		t.Error("replaced an existing file")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "taken.pdf")); string(b) != "t" {
		t.Error("existing file changed")
	}
	// Never through a symbolic link (it points outside).
	r = move(MoveJob{Root: root, Moves: []moveItem{{From: filepath.Join(root, "b.pdf"), To: filepath.Join(root, "link", "b.pdf")}}})
	if r.OK {
		t.Error("moved through a symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "b.pdf")); err == nil {
		t.Error("file escaped the root")
	}
	// Never outside the root, never hidden.
	for _, to := range []string{filepath.Join(outside, "b.pdf"), filepath.Join(root, ".hidden.pdf"), filepath.Join(root, "..", "b.pdf")} {
		if r := move(MoveJob{Root: root, Moves: []moveItem{{From: filepath.Join(root, "b.pdf"), To: to}}}); r.OK {
			t.Errorf("moved to %s", to)
		}
	}
	// Undo removes the folder it created once empty.
	r = move(MoveJob{Root: root, Remove: []string{filepath.Join(root, "Bank")},
		Moves: []moveItem{{From: filepath.Join(root, "Bank", "a.pdf"), To: filepath.Join(root, "a.pdf")}}})
	if !r.OK {
		t.Fatalf("undo failed: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(root, "Bank")); err == nil {
		t.Error("created folder not removed")
	}
}
