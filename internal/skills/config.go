package skills

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Account is a mail account the e-mail skill may read, once granted.
type Account struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	TLS      bool   `json:"tls"`
	User     string `json:"user"`
	PassFile string `json:"-"`
	Mailbox  string `json:"mailbox"`
	// Sending (the reply skill, always confirmed): the person's address
	// and name, and the submission server. The SMTP login defaults to the
	// IMAP one. SMTPTLS is starttls (default), tls (implicit) or none
	// (lab servers on reserved names only).
	Address      string `json:"address,omitempty"`
	DisplayName  string `json:"display_name,omitempty"`
	SMTPHost     string `json:"smtp_host,omitempty"`
	SMTPPort     int    `json:"smtp_port,omitempty"`
	SMTPTLS      string `json:"smtp_tls,omitempty"`
	SMTPUser     string `json:"-"`
	SMTPPassFile string `json:"-"`
}

// CanSend reports whether replies can be sent from this account.
func (a Account) CanSend() bool { return a.Address != "" && a.SMTPHost != "" }

// SMTPPassword reads the submission password (the IMAP one by default).
func (a Account) SMTPPassword() (string, error) {
	if a.SMTPPassFile == "" {
		return a.Password()
	}
	b := a
	b.PassFile = a.SMTPPassFile
	return b.Password()
}

// SubmissionPort is the SMTP port (587, or 465 with implicit TLS).
func (a Account) SubmissionPort() int {
	if a.SMTPPort != 0 {
		return a.SMTPPort
	}
	if a.SMTPTLS == "tls" {
		return 465
	}
	return 587
}

// Site is a web site the person named (a bookmark): a name to say and
// the address to start from.
type Site struct {
	Name  string   `json:"name"`
	URL   string   `json:"url"`
	Host  string   `json:"host"`
	Names []string `json:"names"`
}

// Config of the skills (~/.config/basalt-shell/skills.conf, INI):
//
//	[mail lab]
//	host = imap.example.org
//	port = 143
//	tls = no
//	user = dev
//	password_file = ~/.config/basalt-shell/mail-lab.pass
//	mailbox = INBOX
//
//	[site news]
//	url = http://news.example.org/
//	names = lab news, news site
//
//	[files]
//	folders = Documents Downloads Desktop
type Config struct {
	Accounts []Account
	Sites    []Site
	Folders  []string // default folders offered in a grant, relative to home
	Rerank   bool     // let the model pick the best file among the top hits
	// Contacts are names the person says often ([contacts] names = Ana
	// Souza, Priya Nair): the speech recognition is told their spelling.
	Contacts []string
}

// LoadConfig reads the config file; a missing file gives the defaults.
func LoadConfig(path, home string) Config {
	c := Config{Folders: []string{"Documents", "Downloads", "Desktop"}, Rerank: true}
	f, err := os.Open(path)
	if err != nil {
		return c
	}
	defer f.Close()
	var acc *Account
	var site *Site
	sec := ""
	flush := func() {
		if acc != nil && acc.Host != "" {
			if acc.Mailbox == "" {
				acc.Mailbox = "INBOX"
			}
			c.Accounts = append(c.Accounts, *acc)
		}
		if site != nil && site.URL != "" {
			site.Host = hostOfURL(site.URL)
			c.Sites = append(c.Sites, *site)
		}
		acc, site = nil, nil
	}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || l[0] == '#' || l[0] == ';' {
			continue
		}
		if strings.HasPrefix(l, "[") {
			flush()
			sec = strings.Trim(l, "[]")
			parts := strings.Fields(sec)
			if len(parts) == 2 && parts[0] == "mail" {
				acc = &Account{Name: parts[1]}
			} else if len(parts) == 2 && parts[0] == "site" {
				site = &Site{Name: parts[1]}
			}
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch {
		case acc != nil:
			switch k {
			case "host":
				acc.Host = v
			case "port":
				acc.Port, _ = strconv.Atoi(v)
			case "tls":
				acc.TLS = v == "yes" || v == "true"
			case "user":
				acc.User = v
			case "password_file":
				acc.PassFile = expand(v, home)
			case "mailbox":
				acc.Mailbox = v
			case "address":
				acc.Address = strings.ToLower(v)
			case "name", "display_name":
				acc.DisplayName = v
			case "smtp_host":
				acc.SMTPHost = v
			case "smtp_port":
				acc.SMTPPort, _ = strconv.Atoi(v)
			case "smtp_tls":
				acc.SMTPTLS = v
			case "smtp_user":
				acc.SMTPUser = v
			case "smtp_password_file":
				acc.SMTPPassFile = expand(v, home)
			}
		case site != nil:
			switch k {
			case "url":
				site.URL = v
			case "names":
				for _, n := range strings.Split(v, ",") {
					if n = strings.ToLower(strings.TrimSpace(n)); n != "" {
						site.Names = append(site.Names, n)
					}
				}
			}
		case sec == "files" && k == "folders":
			c.Folders = strings.Fields(v)
		case sec == "files" && k == "rerank":
			c.Rerank = v == "yes" || v == "true"
		case sec == "contacts" && k == "names":
			for _, n := range strings.Split(v, ",") {
				if n = strings.TrimSpace(n); n != "" {
					c.Contacts = append(c.Contacts, n)
				}
			}
		}
	}
	flush()
	return c
}

func expand(p, home string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

func hostOfURL(u string) string {
	s := u
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/:?#"); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}

// Password reads an account's password file (it must be private).
func (a Account) Password() (string, error) {
	st, err := os.Stat(a.PassFile)
	if err != nil {
		return "", err
	}
	if st.Mode().Perm()&0o077 != 0 {
		return "", os.ErrPermission
	}
	b, err := os.ReadFile(a.PassFile)
	return strings.TrimSpace(string(b)), err
}

// privateName reports names reserved for private use or documentation
// (RFC 6761, RFC 2606, RFC 8375 and common lab suffixes): allowlist
// entries for them may resolve to private addresses ("private" in the
// allowlist). No public service can use these names.
func privateName(h string) bool {
	h = strings.ToLower(strings.TrimSuffix(h, "."))
	for _, s := range []string{".test", ".internal", ".lan", ".home.arpa", ".local", ".localdomain", ".lab", ".example", ".invalid"} {
		if strings.HasSuffix(h, s) {
			return true
		}
	}
	for _, d := range []string{"example.com", "example.net", "example.org"} {
		if h == d || strings.HasSuffix(h, "."+d) {
			return true
		}
	}
	return false
}

// AllowEntry is the allowlist entry (basalt-agent/basalt-resolver
// format) for a host and ports.
func AllowEntry(host string, ports ...int) string {
	e := host
	if len(ports) > 0 {
		var ps []string
		for _, p := range ports {
			ps = append(ps, strconv.Itoa(p))
		}
		e += ":" + strings.Join(ps, ",")
	}
	if privateName(host) {
		e += " private"
	}
	return e
}
