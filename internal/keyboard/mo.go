package keyboard

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// LocaleDir is where xkeyboard-config installs the translations of its
// layout names (gettext domain "xkeyboard-config").
var LocaleDir = "/usr/share/locale"

// Names translates the registry's layout names into a language, from
// xkeyboard-config's own catalog ("pt_BR", then "pt"). Without a catalog
// the names stay in English.
func Names(locale string) func(string) string {
	locale = strings.SplitN(strings.SplitN(locale, ".", 2)[0], "@", 2)[0]
	if locale == "" || strings.HasPrefix(locale, "en") || locale == "C" || locale == "POSIX" {
		return nil
	}
	for _, l := range []string{locale, strings.SplitN(locale, "_", 2)[0]} {
		m, err := readMO(filepath.Join(LocaleDir, l, "LC_MESSAGES", "xkeyboard-config.mo"))
		if err == nil && len(m) > 0 {
			return func(s string) string {
				if t, ok := m[s]; ok && t != "" {
					return t
				}
				return s
			}
		}
	}
	return nil
}

// readMO reads the singular messages of a GNU gettext .mo file.
func readMO(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 28 {
		return nil, errors.New("short .mo file")
	}
	var bo binary.ByteOrder
	switch binary.LittleEndian.Uint32(b) {
	case 0x950412de:
		bo = binary.LittleEndian
	case 0xde120495:
		bo = binary.BigEndian
	default:
		return nil, errors.New("not a .mo file")
	}
	n := int(bo.Uint32(b[8:]))
	orig, trans := int(bo.Uint32(b[12:])), int(bo.Uint32(b[16:]))
	str := func(table, i int) (string, bool) {
		at := table + i*8
		if at < 0 || at+8 > len(b) {
			return "", false
		}
		l, off := int(bo.Uint32(b[at:])), int(bo.Uint32(b[at+4:]))
		if off < 0 || l < 0 || off+l > len(b) {
			return "", false
		}
		return string(b[off : off+l]), true
	}
	if n < 0 || n > 1<<20 {
		return nil, errors.New("bad .mo header")
	}
	m := make(map[string]string, n)
	for i := 0; i < n; i++ {
		o, ok1 := str(orig, i)
		t, ok2 := str(trans, i)
		if !ok1 || !ok2 || o == "" {
			continue
		}
		// Plural entries: the first form (layout names have none).
		o, _, _ = strings.Cut(o, "\x00")
		t, _, _ = strings.Cut(t, "\x00")
		// A message with a context ("ctx\x04id"): keep the id.
		if _, id, ok := strings.Cut(o, "\x04"); ok {
			o = id
		}
		m[o] = t
	}
	return m, nil
}
