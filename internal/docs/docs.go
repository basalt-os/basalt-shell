// Package docs is the small local index of the find-files skill: the
// metadata and text of the person's documents in the folders they
// granted, searched by words, file type and time. It runs in its own
// confined process (basalt-skill-index for building, basalt-skill for
// searching); the index never leaves the machine.
//
// Why not localsearch (tracker3): it indexes the whole home by default
// through a session D-Bus service that agent domains cannot reach (and
// should not), its scope is not the per-grant scope the skill needs, and
// it has no notion of content findings (prompt injection in documents).
// Its extractors are the better long-term source of text; see the
// spike report.
package docs

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/basalt-os/basalt-shell/internal/guard"
)

// Doc is one indexed file.
type Doc struct {
	Path     string       `json:"path"`
	Name     string       `json:"name"`
	Kind     string       `json:"kind"` // pdf, document, spreadsheet, presentation, text, web, mail, image, other
	Size     int64        `json:"size"`
	Modified time.Time    `json:"modified"`
	Created  time.Time    `json:"created,omitempty"` // from the document's own metadata
	Title    string       `json:"title,omitempty"`
	Author   string       `json:"author,omitempty"`
	Text     string       `json:"text,omitempty"` // cleaned, clipped
	Hidden   string       `json:"hidden,omitempty"`
	Report   guard.Report `json:"report"`
}

// Index is the whole index.
type Index struct {
	Version int       `json:"version"`
	Built   time.Time `json:"built"`
	Roots   []string  `json:"roots"`
	Docs    []Doc     `json:"docs"`
	Skipped int       `json:"skipped"`
	Errors  []string  `json:"errors,omitempty"`
	MS      int64     `json:"build_ms"`
}

// Never indexed, whatever the grant: credentials, keyrings, browser
// profiles, mail stores and the desktop's own state. (The SELinux domain
// of the indexer cannot read most of these anyway; this keeps it from
// trying.)
var denyDirs = []string{".ssh", ".gnupg", ".pki", ".local/share/keyrings", ".password-store", ".mozilla", ".config/chromium",
	".config/google-chrome", ".thunderbird", ".local/share/basalt-shell", ".local/state", ".config/basalt-shell", ".aws", ".kube", ".docker"}

// Denied reports whether a path is under a denied directory of home.
func Denied(home, p string) bool {
	rel, err := filepath.Rel(home, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return false
	}
	for _, d := range denyDirs {
		if rel == d || strings.HasPrefix(rel, d+"/") {
			return true
		}
	}
	return false
}

var kinds = map[string]string{
	".pdf": "pdf", ".txt": "text", ".md": "text", ".csv": "spreadsheet", ".log": "text", ".json": "text", ".rst": "text",
	".docx": "document", ".odt": "document", ".rtf": "document", ".doc": "document",
	".xlsx": "spreadsheet", ".ods": "spreadsheet", ".xls": "spreadsheet",
	".pptx": "presentation", ".odp": "presentation",
	".html": "web", ".htm": "web", ".eml": "mail",
	".png": "image", ".jpg": "image", ".jpeg": "image", ".webp": "image", ".gif": "image", ".svg": "image", ".heic": "image",
}

// KindOf returns the kind of a file name.
func KindOf(name string) string {
	if k, ok := kinds[strings.ToLower(filepath.Ext(name))]; ok {
		return k
	}
	return "other"
}

// Options of a build.
type Options struct {
	Home               string
	Roots              []string // granted folders
	MaxFiles           int
	MaxText            int // characters of text kept per file
	PDFToText, PDFInfo string
}

// Build walks the roots (no symlinks followed, no hidden files, depth
// limited) and extracts each file's metadata and text.
func Build(ctx context.Context, o Options) (*Index, error) {
	if o.MaxFiles == 0 {
		o.MaxFiles = 5000
	}
	if o.MaxText == 0 {
		o.MaxText = 20000
	}
	if o.PDFToText == "" {
		o.PDFToText, _ = exec.LookPath("pdftotext")
	}
	if o.PDFInfo == "" {
		o.PDFInfo, _ = exec.LookPath("pdfinfo")
	}
	start := time.Now()
	ix := &Index{Version: 1, Built: time.Now().UTC(), Roots: o.Roots}
	for _, root := range o.Roots {
		root = filepath.Clean(root)
		if Denied(o.Home, root) {
			ix.Errors = append(ix.Errors, root+": not allowed")
			continue
		}
		base := strings.Count(root, string(os.PathSeparator))
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				ix.Skipped++
				return nil
			}
			name := d.Name()
			if p != root && strings.HasPrefix(name, ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				ix.Skipped++
				return nil
			}
			if Denied(o.Home, p) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				if strings.Count(p, string(os.PathSeparator))-base > 8 {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type()&fs.ModeSymlink != 0 || !d.Type().IsRegular() {
				ix.Skipped++
				return nil
			}
			if len(ix.Docs) >= o.MaxFiles {
				return filepath.SkipAll
			}
			info, err := d.Info()
			if err != nil {
				ix.Skipped++
				return nil
			}
			doc := Doc{Path: p, Name: name, Kind: KindOf(name), Size: info.Size(), Modified: info.ModTime().UTC()}
			if info.Size() <= 50<<20 {
				extract(ctx, &doc, o)
			}
			doc.Report.Add(guard.Scan(doc.Name).Findings...)
			ix.Docs = append(ix.Docs, doc)
			return nil
		})
		if err != nil && !errors.Is(err, filepath.SkipAll) {
			ix.Errors = append(ix.Errors, err.Error())
		}
	}
	ix.MS = time.Since(start).Milliseconds()
	return ix, nil
}

func extract(ctx context.Context, d *Doc, o Options) {
	var text, hidden string
	ext := strings.ToLower(filepath.Ext(d.Name))
	switch ext {
	case ".txt", ".md", ".csv", ".log", ".json", ".rst":
		b, err := readHead(d.Path, 1<<20)
		if err == nil {
			text = string(bytes.ToValidUTF8(b, []byte("�")))
		}
	case ".html", ".htm":
		if b, err := readHead(d.Path, 2<<20); err == nil {
			h := guard.ParseHTML(string(b))
			text, hidden, d.Title = h.Visible, h.Hidden, h.Title
			d.Report.Add(h.Report.Findings...)
		}
	case ".eml":
		if b, err := readHead(d.Path, 2<<20); err == nil {
			text = string(b)
		}
	case ".pdf":
		text, hidden = pdfText(ctx, d, o)
	case ".docx":
		text = zipXMLText(d.Path, "word/document.xml", "t")
		meta := zipXMLText(d.Path, "docProps/core.xml", "title")
		d.Title = strings.TrimSpace(meta)
		d.Author = strings.TrimSpace(zipXMLText(d.Path, "docProps/core.xml", "creator"))
	case ".odt", ".ods", ".odp":
		text = zipXMLText(d.Path, "content.xml", "")
		d.Title = strings.TrimSpace(zipXMLText(d.Path, "meta.xml", "title"))
	case ".xlsx":
		text = zipXMLText(d.Path, "xl/sharedStrings.xml", "t")
	case ".pptx":
		for i := 1; i <= 40; i++ {
			t := zipXMLText(d.Path, fmt.Sprintf("ppt/slides/slide%d.xml", i), "t")
			if t == "" {
				break
			}
			text += t + "\n"
		}
	}
	if text != "" {
		d.Report.Add(guard.Scan(text).Findings...)
		d.Text = guard.Clean(text, o.MaxText)
	}
	if hidden != "" {
		sub := guard.Scan(hidden)
		sev := "low"
		if sub.Suspicious() {
			sev = "high"
		}
		d.Report.Add(guard.Finding{Kind: guard.KindHidden, Severity: sev, Detail: "text a reader would not see", Snippet: clipText(hidden, 140)})
		d.Report.Add(sub.Findings...)
		d.Hidden = clipText(hidden, 2000)
	}
}

func readHead(p string, n int64) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, n))
}

var rePDFField = regexp.MustCompile(`(?m)^(Title|Author|CreationDate|Producer|Creator|Subject):\s*(.*)$`)

// pdfText runs pdftotext (poppler) on the first pages, and pdfinfo for
// the metadata. Text drawn in white or at a tiny size is not detected
// here (pdftotext extracts it like any text); the guard still scans it.
func pdfText(ctx context.Context, d *Doc, o Options) (string, string) {
	if o.PDFInfo != "" {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		out, err := exec.CommandContext(cctx, o.PDFInfo, "-isodates", d.Path).Output()
		cancel()
		if err == nil {
			for _, m := range rePDFField.FindAllStringSubmatch(string(out), -1) {
				v := strings.TrimSpace(m[2])
				switch m[1] {
				case "Title":
					d.Title = v
				case "Author":
					d.Author = v
				case "CreationDate":
					if t, err := time.Parse(time.RFC3339, v); err == nil {
						d.Created = t.UTC()
					}
				case "Subject":
					if d.Title == "" {
						d.Title = v
					}
				}
			}
		}
	}
	if o.PDFToText == "" {
		return "", ""
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, o.PDFToText, "-l", "20", "-enc", "UTF-8", d.Path, "-").Output()
	if err != nil {
		return "", ""
	}
	return string(out), ""
}

// zipXMLText reads one XML member of an office file and returns the text
// of elements with the given local name ("" for all character data).
func zipXMLText(path, member, local string) string {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return ""
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != member {
			continue
		}
		if f.UncompressedSize64 > 20<<20 {
			return ""
		}
		rc, err := f.Open()
		if err != nil {
			return ""
		}
		defer rc.Close()
		dec := xml.NewDecoder(io.LimitReader(rc, 20<<20))
		dec.Strict = false
		var b strings.Builder
		depth := 0
		for {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			switch t := tok.(type) {
			case xml.StartElement:
				if local == "" || t.Name.Local == local {
					depth++
				}
			case xml.EndElement:
				if local == "" || t.Name.Local == local {
					if depth > 0 {
						depth--
					}
				}
				if t.Name.Local == "p" || t.Name.Local == "tr" || t.Name.Local == "si" {
					b.WriteByte('\n')
				}
			case xml.CharData:
				if depth > 0 || local == "" {
					b.Write(t)
				}
			}
			if b.Len() > 2<<20 {
				break
			}
		}
		return b.String()
	}
	return ""
}

// Save writes the index (0600).
func (ix *Index) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(ix); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return os.Rename(tmp, path)
}

// Load reads an index.
func Load(path string) (*Index, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ix Index
	return &ix, json.Unmarshal(b, &ix)
}

// Query is a structured search (built from the person's request, never
// from content).
type Query struct {
	Words  []string  `json:"words"`
	Kinds  []string  `json:"kinds,omitempty"`
	After  time.Time `json:"after,omitempty"`
	Before time.Time `json:"before,omitempty"`
	Within []string  `json:"within,omitempty"` // only these roots (the active grants)
	Limit  int       `json:"limit,omitempty"`
}

// Hit is one result.
type Hit struct {
	Doc
	Score   float64  `json:"score"`
	Snippet string   `json:"snippet"`
	Matched []string `json:"matched,omitempty"`
}

var reTok = regexp.MustCompile(`[\p{L}\p{N}]+`)

func tokens(s string) []string {
	var out []string
	for _, t := range reTok.FindAllString(strings.ToLower(s), -1) {
		if len([]rune(t)) >= 2 {
			out = append(out, stem(t))
		}
	}
	return out
}

// stem is a tiny English suffix stripper (statements -> statement,
// invoices -> invoice, banking -> bank).
func stem(t string) string {
	for _, suf := range []string{"ing", "ies", "es", "s"} {
		if len(t) > len(suf)+3 && strings.HasSuffix(t, suf) {
			if suf == "ies" {
				return t[:len(t)-3] + "y"
			}
			return t[:len(t)-len(suf)]
		}
	}
	return t
}

// Search ranks documents with BM25 over name, title, author and text
// (name and title count three times), filtered by kind, time and roots.
// The time filter uses the document's own creation date when it has one,
// else the file's modification time.
func Search(ix *Index, q Query) []Hit {
	if q.Limit == 0 {
		q.Limit = 8
	}
	var qt []string
	seen := map[string]bool{}
	for _, w := range q.Words {
		for _, t := range tokens(w) {
			if !seen[t] && !stop[t] {
				seen[t] = true
				qt = append(qt, t)
			}
		}
	}
	type docTok struct {
		tf  map[string]float64
		len float64
	}
	var cand []int
	var dts []docTok
	df := map[string]float64{}
	var total float64
	for i, d := range ix.Docs {
		if len(q.Within) > 0 && !under(d.Path, q.Within) {
			continue
		}
		if len(q.Kinds) > 0 && !contains(q.Kinds, d.Kind) {
			continue
		}
		when := d.Modified
		if !d.Created.IsZero() {
			when = d.Created
		}
		if !q.After.IsZero() && when.Before(q.After) {
			continue
		}
		if !q.Before.IsZero() && !when.Before(q.Before) {
			continue
		}
		tf := map[string]float64{}
		n := 0.0
		for _, t := range tokens(d.Name + " " + d.Title + " " + d.Author) {
			tf[t] += 3
			n += 3
		}
		for _, t := range tokens(d.Text) {
			tf[t]++
			n++
		}
		for t := range tf {
			df[t]++
		}
		cand = append(cand, i)
		dts = append(dts, docTok{tf, n})
		total += n
	}
	if len(cand) == 0 {
		return nil
	}
	avg := total / float64(len(cand))
	N := float64(len(cand))
	var hits []Hit
	for k, i := range cand {
		dt := dts[k]
		score := 0.0
		var matched []string
		for _, t := range qt {
			f := dt.tf[t]
			if f == 0 {
				continue
			}
			matched = append(matched, t)
			idf := math.Log(1 + (N-df[t]+0.5)/(df[t]+0.5))
			score += idf * f * 2.2 / (f + 1.2*(0.25+0.75*dt.len/math.Max(avg, 1)))
		}
		if len(qt) > 0 && score == 0 {
			continue
		}
		d := ix.Docs[i]
		h := Hit{Doc: d, Score: math.Round(score*100) / 100, Matched: matched}
		h.Snippet = snippet(d.Text, matched)
		h.Text, h.Hidden = "", ""
		hits = append(hits, h)
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].Score != hits[b].Score {
			return hits[a].Score > hits[b].Score
		}
		return hits[a].Modified.After(hits[b].Modified)
	})
	if len(hits) > q.Limit {
		hits = hits[:q.Limit]
	}
	return hits
}

var stop = map[string]bool{"the": true, "a": true, "an": true, "of": true, "to": true, "from": true, "my": true, "me": true, "for": true,
	"and": true, "or": true, "in": true, "on": true, "with": true, "that": true, "this": true, "file": true, "find": true, "show": true,
	"sent": true, "last": true, "month": true, "week": true, "year": true, "is": true, "it": true, "where": true, "document": true}

func under(p string, roots []string) bool {
	for _, r := range roots {
		r = filepath.Clean(r)
		if p == r || strings.HasPrefix(p, r+"/") {
			return true
		}
	}
	return false
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func snippet(text string, matched []string) string {
	if text == "" {
		return ""
	}
	lt := strings.ToLower(text)
	at := -1
	for _, m := range matched {
		if i := strings.Index(lt, m); i >= 0 && (at < 0 || i < at) {
			at = i
		}
	}
	if at < 0 {
		at = 0
	}
	a := at - 80
	if a < 0 {
		a = 0
	}
	for a > 0 && a < len(text) && !isBoundary(text, a) {
		a--
	}
	b := a + 240
	if b > len(text) {
		b = len(text)
	}
	for b < len(text) && !isBoundary(text, b) {
		b++
	}
	return strings.Join(strings.Fields(text[a:b]), " ")
}

func isBoundary(s string, i int) bool {
	if i <= 0 || i >= len(s) {
		return true
	}
	r := rune(s[i])
	return s[i] < 0x80 && (unicode.IsSpace(r) || unicode.IsPunct(r))
}

func clipText(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		cut := n
		for cut > 0 && s[cut]&0xC0 == 0x80 {
			cut--
		}
		return s[:cut] + "..."
	}
	return s
}
