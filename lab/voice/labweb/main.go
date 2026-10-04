// Command labweb is the voice spike's lab web server: it serves the test
// pages of each lab site from a directory per host name and logs every
// request (host, method, path, query, body size, client) as JSON lines.
// The attacker's sites (evil.lab.test, and any request to an address it
// listens on) log what reached them: the injection tests check that
// nothing did.
//
//	labweb -root /srv/lab-web -log /var/log/lab-web.jsonl -listen 10.77.0.10:80,10.77.0.11:80,10.77.0.66:80
package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func main() {
	root := flag.String("root", "/srv/lab-web", "one directory per host name")
	logPath := flag.String("log", "/var/log/lab-web.jsonl", "request log")
	listen := flag.String("listen", "", "comma separated addresses")
	flag.Parse()
	f, err := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatal(err)
	}
	var mu sync.Mutex
	enc := json.NewEncoder(f)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
		host := strings.ToLower(r.Host)
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		mu.Lock()
		_ = enc.Encode(map[string]any{"time": time.Now().UTC().Format(time.RFC3339Nano), "host": host, "method": r.Method,
			"path": r.URL.Path, "query": r.URL.RawQuery, "body_bytes": n, "remote": r.RemoteAddr, "local": r.Context().Value(http.LocalAddrContextKey).(net.Addr).String(),
			"ua": r.UserAgent()})
		mu.Unlock()
		dir := filepath.Join(*root, filepath.Base(host))
		if _, err := os.Stat(dir); err != nil {
			dir = filepath.Join(*root, "_default")
		}
		http.FileServer(http.Dir(dir)).ServeHTTP(w, r)
	})
	var wg sync.WaitGroup
	for _, a := range strings.Split(*listen, ",") {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		wg.Add(1)
		go func(a string) {
			defer wg.Done()
			log.Printf("listening on %s", a)
			log.Print(http.ListenAndServe(a, h))
		}(a)
	}
	wg.Wait()
}
