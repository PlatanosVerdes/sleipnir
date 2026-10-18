// huginn says where it reaches the internet from, as an IP-info service sees it at request time.
// It runs inside a tunnel's network namespace, so its answer is that tunnel's exit: a quick check
// that a country switch in sleipnir really took effect.
package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

type place struct {
	Country string    `json:"country"`
	City    string    `json:"city"`
	IP      string    `json:"ip"`
	Org     string    `json:"org"`
	Source  string    `json:"source"`
	At      time.Time `json:"at"`
	Error   string    `json:"error,omitempty"`
}

// lookup asks ipwho.is first, which gives country names, and falls back to ipinfo.io, which only
// gives codes. Neither needs a key. The answer is kept a few seconds so a polling page does not
// hammer them, and short enough that a switch shows within one poll.
type lookup struct {
	client    *http.Client
	ttl       time.Duration
	ipwhoURL  string
	ipinfoURL string

	mu   sync.Mutex
	last place
}

func (l *lookup) get() place {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.last.At.IsZero() && time.Since(l.last.At) < l.ttl && l.last.Error == "" {
		return l.last
	}
	p, err := l.ipwho()
	if err != nil {
		p, err = l.ipinfo()
	}
	if err != nil {
		p = place{Error: err.Error()}
	}
	p.At = time.Now()
	l.last = p
	return p
}

func (l *lookup) ipwho() (place, error) {
	var r struct {
		Success    bool   `json:"success"`
		Message    string `json:"message"`
		IP         string `json:"ip"`
		Country    string `json:"country"`
		City       string `json:"city"`
		Connection struct {
			Org string `json:"org"`
			ISP string `json:"isp"`
		} `json:"connection"`
	}
	if err := l.fetch(l.ipwhoURL, &r); err != nil {
		return place{}, err
	}
	if !r.Success {
		return place{}, fmt.Errorf("ipwho.is: %s", r.Message)
	}
	org := r.Connection.Org
	if org == "" {
		org = r.Connection.ISP
	}
	return place{Country: r.Country, City: r.City, IP: r.IP, Org: org, Source: "ipwho.is"}, nil
}

func (l *lookup) ipinfo() (place, error) {
	var r struct {
		IP      string `json:"ip"`
		Country string `json:"country"`
		City    string `json:"city"`
		Org     string `json:"org"`
	}
	if err := l.fetch(l.ipinfoURL, &r); err != nil {
		return place{}, err
	}
	return place{Country: r.Country, City: r.City, IP: r.IP, Org: r.Org, Source: "ipinfo.io"}, nil
}

func (l *lookup) fetch(url string, out any) error {
	resp, err := l.client.Get(url)
	if err != nil {
		return fmt.Errorf("no way out: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

var page = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>huginn</title>
<style>
  :root { --bg:#f3f4f6; --ink:#14171f; --muted:#626a78; --accent:#2f5bea; --bad:#c43535; }
  @media (prefers-color-scheme: dark) { :root { --bg:#0f1218; --ink:#e8ebf1; --muted:#9aa2b1; --accent:#6d8dff; --bad:#ff7a7a; color-scheme: dark; } }
  body { margin:0; min-height:100vh; display:grid; place-items:center; background:var(--bg); color:var(--ink);
    font:16px/1.5 ui-monospace, "SF Mono", Menlo, monospace; padding:16px; }
  main { max-width: 34rem; }
  h1 { font-size: 15px; color: var(--muted); font-weight: 500; margin: 0 0 6px; }
  .country { font: 700 44px/1.1 system-ui, -apple-system, sans-serif; letter-spacing: -.02em; margin: 0 0 4px; }
  .city { font-size: 18px; margin: 0 0 18px; }
  dl { display: grid; grid-template-columns: auto 1fr; gap: 4px 16px; margin: 0; font-size: 14px; }
  dt { color: var(--muted); } dd { margin: 0; }
  .err { color: var(--bad); }
  small { display:block; margin-top: 18px; color: var(--muted); font-size: 12px; }
</style></head><body><main>
<h1>huginn · this container reaches the internet from</h1>
{{if .Error}}<p class="country err">nowhere</p><p class="city err">{{.Error}}</p>{{else}}
<p class="country">{{.Country}}</p><p class="city">{{.City}}</p>
<dl><dt>IP</dt><dd>{{.IP}}</dd><dt>network</dt><dd>{{.Org}}</dd><dt>source</dt><dd>{{.Source}}</dd></dl>{{end}}
<small>Asked at {{.At.Format "15:04:05"}}. JSON at /json.</small>
</main></body></html>`))

// buildVersion is set at build time to the release tag.
var buildVersion = "local"

func main() {
	log.Printf("huginn %s", buildVersion)
	l := &lookup{client: &http.Client{Timeout: 8 * time.Second}, ttl: 5 * time.Second,
		ipwhoURL: "https://ipwho.is/", ipinfoURL: "https://ipinfo.io/json"}
	http.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(l.get())
	})
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		page.Execute(w, l.get())
	})
	addr := ":8080"
	if v := os.Getenv("LISTEN_ADDR"); v != "" {
		addr = v
	}
	log.Printf("huginn on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
