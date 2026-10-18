package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type panel struct {
	name        string
	docker      *dockerClient
	apiKey      string
	countries   []country
	peers       []peer
	token       string
	client      *http.Client
	controlPort string
	gluetunBase func(name string) string // tests point this at a fake gluetun
}

type country struct {
	Name    string `json:"name"`
	Servers int    `json:"servers"`
}

type peer struct {
	Name string
	URL  string
}

type probeResult struct {
	Country string `json:"country"`
	City    string `json:"city"`
	IP      string `json:"ip"`
	Org     string `json:"org"`
	Error   string `json:"error,omitempty"`
}

// tunnel is what the page sees. gluetun's own settings never leave this process: they carry the
// VPN credentials.
type tunnel struct {
	ID       string       `json:"id"`
	State    string       `json:"state"` // the container: running, exited…
	VPN      string       `json:"vpn"`   // gluetun's loop: running, stopped, crashed…
	Since    string       `json:"since"` // from Docker's status line
	Country  string       `json:"country"`
	City     string       `json:"city"`
	IP       string       `json:"ip"`
	Org      string       `json:"org"`
	Protocol string       `json:"protocol"`
	Target   string       `json:"target"` // the country gluetun is set to pick from
	UsedBy   []string     `json:"used_by"`
	Probe    *probeResult `json:"probe,omitempty"`
	Error    string       `json:"error,omitempty"`
}

type machine struct {
	Name      string    `json:"name"`
	Peer      string    `json:"peer,omitempty"` // empty for this machine
	Reachable bool      `json:"reachable"`
	Tunnels   []tunnel  `json:"tunnels"`
	Countries []country `json:"countries,omitempty"`
}

func (p *panel) tunnels() ([]tunnel, error) {
	containers, err := p.docker.gluetuns()
	if err != nil {
		return nil, err
	}
	out := make([]tunnel, len(containers))
	var wg sync.WaitGroup
	for i, c := range containers {
		wg.Add(1)
		go func(i int, c container) {
			defer wg.Done()
			out[i] = p.describe(c)
		}(i, c)
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (p *panel) describe(c container) tunnel {
	// "Up 5 minutes (healthy)": the dot already says healthy; an unhealthy suffix is kept.
	since := strings.TrimSuffix(strings.TrimPrefix(c.Status, "Up "), " (healthy)")
	t := tunnel{ID: c.name(), State: c.State, Since: since, UsedBy: splitList(c.Labels["sleipnir.used-by"])}
	if c.State != "running" {
		return t
	}
	g := p.gluetun(t.ID)
	var status struct {
		Status string `json:"status"`
	}
	if err := g.get("/v1/vpn/status", &status); err != nil {
		t.Error = err.Error()
		return t
	}
	t.VPN = status.Status
	var settings struct {
		Type     string `json:"type"`
		Provider struct {
			ServerSelection struct {
				Countries []string `json:"countries"`
			} `json:"server_selection"`
		} `json:"provider"`
	}
	if err := g.get("/v1/vpn/settings", &settings); err == nil {
		t.Protocol = protocolName(settings.Type)
		names := make([]string, len(settings.Provider.ServerSelection.Countries))
		for i, c := range settings.Provider.ServerSelection.Countries {
			names[i] = p.countryName(c)
		}
		t.Target = strings.Join(names, ", ")
	}
	var ip struct {
		IP           string `json:"public_ip"`
		Country      string `json:"country"`
		City         string `json:"city"`
		Organization string `json:"organization"`
	}
	if err := g.get("/v1/publicip/ip", &ip); err == nil && ip.IP != "" && ip.IP != "invalid IP" {
		t.IP, t.Country, t.City, t.Org = ip.IP, canonicalCountry(ip.Country), ip.City, ip.Organization
	}
	if probe := c.Labels["sleipnir.probe"]; probe != "" {
		t.Probe = p.probe(probe)
	}
	return t
}

func (p *panel) probe(u string) *probeResult {
	r := &probeResult{}
	resp, err := p.client.Get(u)
	if err != nil {
		r.Error = "no answer"
		return r
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(r); err != nil {
		r.Error = "unreadable answer"
	}
	r.Country = canonicalCountry(r.Country)
	return r
}

// act moves one tunnel. Each call returns as soon as gluetun has accepted it; the page then
// watches /api/tunnels until the new exit is real, since reconnecting takes gluetun a few seconds.
func (p *panel) act(id, action string, body io.Reader) error {
	containers, err := p.docker.gluetuns()
	if err != nil {
		return err
	}
	found := false
	for _, c := range containers {
		if c.name() == id {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("%s is not a tunnel this panel manages", id)
	}
	g := p.gluetun(id)
	switch action {
	case "switch":
		var req struct {
			Country string `json:"country"`
		}
		if err := json.NewDecoder(body).Decode(&req); err != nil || req.Country == "" {
			return fmt.Errorf("a country is needed")
		}
		if !p.knownCountry(req.Country) {
			return fmt.Errorf("no servers in %q", req.Country)
		}
		settings := map[string]any{"provider": map[string]any{"server_selection": map[string]any{"countries": []string{req.Country}}}}
		return g.put("/v1/vpn/settings", settings)
	case "another":
		// gluetun picks a server at random within its filters on every start.
		if err := g.put("/v1/vpn/status", map[string]string{"status": "stopped"}); err != nil {
			return err
		}
		return g.put("/v1/vpn/status", map[string]string{"status": "running"})
	case "stop":
		return g.put("/v1/vpn/status", map[string]string{"status": "stopped"})
	case "start":
		return g.put("/v1/vpn/status", map[string]string{"status": "running"})
	}
	return fmt.Errorf("unknown action %q", action)
}

// countryName gives a country as the list spells it: gluetun stores its filters lowercased.
func (p *panel) countryName(name string) string {
	for _, c := range p.countries {
		if strings.EqualFold(c.Name, name) {
			return c.Name
		}
	}
	return name
}

func (p *panel) knownCountry(name string) bool {
	if len(p.countries) == 0 {
		return true
	}
	for _, c := range p.countries {
		if c.Name == name {
			return true
		}
	}
	return false
}

// --- HTTP handlers ---

func (p *panel) serveTunnels(w http.ResponseWriter, r *http.Request) {
	t, err := p.tunnels()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": p.name, "tunnels": t, "countries": p.countries})
}

// serveMachines is the page's one call: this machine, then every peer, asked in parallel.
func (p *panel) serveMachines(w http.ResponseWriter, r *http.Request) {
	machines := make([]machine, 1+len(p.peers))
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t, err := p.tunnels()
		machines[0] = machine{Name: p.name, Reachable: err == nil, Tunnels: t, Countries: p.countries}
	}()
	for i, pr := range p.peers {
		wg.Add(1)
		go func(i int, pr peer) {
			defer wg.Done()
			m := machine{Name: pr.Name, Peer: pr.Name}
			var body struct {
				Name      string    `json:"name"`
				Tunnels   []tunnel  `json:"tunnels"`
				Countries []country `json:"countries"`
			}
			if err := p.peerCall(pr, http.MethodGet, "/api/tunnels", nil, &body); err == nil {
				m.Reachable, m.Tunnels, m.Countries = true, body.Tunnels, body.Countries
			}
			machines[1+i] = m
		}(i, pr)
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, map[string]any{"machines": machines})
}

// serveAction is POST /api/tunnels/<id>/<action>.
func (p *panel) serveAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/tunnels/"), "/"), "/")
	if len(parts) != 2 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "expected /api/tunnels/<id>/<action>"})
		return
	}
	if err := p.act(parts[0], parts[1], r.Body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"ok": "accepted"})
}

// servePeerAction is POST /api/peers/<peer>/tunnels/<id>/<action>, relayed to that peer's panel.
func (p *panel) servePeerAction(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/peers/")
	name, path, ok := strings.Cut(rest, "/")
	if !ok || r.Method != http.MethodPost {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "expected POST /api/peers/<peer>/tunnels/<id>/<action>"})
		return
	}
	for _, pr := range p.peers {
		if pr.Name == name {
			body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
			var out map[string]string
			if err := p.peerCall(pr, http.MethodPost, "/api/"+path, body, &out); err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusAccepted, out)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown peer " + name})
}

func (p *panel) requireToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Panel-Token")
		if subtle.ConstantTimeCompare([]byte(got), []byte(p.token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "bad token"})
			return
		}
		next(w, r)
	}
}

func (p *panel) peerCall(pr peer, method, path string, body []byte, out any) error {
	req, err := http.NewRequest(method, strings.TrimRight(pr.URL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-Panel-Token", p.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e map[string]string
		json.NewDecoder(resp.Body).Decode(&e)
		return fmt.Errorf("%s: HTTP %d %s", pr.Name, resp.StatusCode, e["error"])
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// --- gluetun control server ---

type gluetunClient struct {
	base   string
	apiKey string
	client *http.Client
}

func (p *panel) gluetun(name string) *gluetunClient {
	base := "http://" + name + ":" + p.controlPort
	if p.gluetunBase != nil {
		base = p.gluetunBase(name)
	}
	return &gluetunClient{base: base, apiKey: p.apiKey, client: p.client}
}

func (g *gluetunClient) get(path string, out any) error {
	return g.do(http.MethodGet, path, nil, out)
}

func (g *gluetunClient) put(path string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return g.do(http.MethodPut, path, data, nil)
}

func (g *gluetunClient) do(method, path string, body []byte, out any) error {
	req, err := http.NewRequest(method, g.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", g.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("gluetun does not answer")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("gluetun refused the API key")
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("gluetun: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// --- country list ---

// loadCountries counts the WireGuard servers per country in gluetun's servers.json, the list its
// updater keeps current. Countries with no WireGuard server are left out: a tunnel could not use them.
func loadCountries(path string) []country {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	// The top level mixes a numeric "version" with one object per provider.
	var file map[string]json.RawMessage
	if json.Unmarshal(data, &file) != nil {
		return nil
	}
	counts := map[string]int{}
	for _, raw := range file {
		var provider struct {
			Servers []struct {
				VPN     string `json:"vpn"`
				Country string `json:"country"`
			} `json:"servers"`
		}
		if json.Unmarshal(raw, &provider) != nil {
			continue
		}
		for _, s := range provider.Servers {
			if s.VPN == "wireguard" && s.Country != "" {
				counts[s.Country]++
			}
		}
	}
	out := make([]country, 0, len(counts))
	for name, n := range counts {
		out = append(out, country{Name: name, Servers: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// canonicalCountry maps the names IP-info services use onto gluetun's, so "is it in the country
// I asked for" compares like with like.
func canonicalCountry(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "türkiye", "turkiye":
		return "Turkey"
	case "czechia":
		return "Czech Republic"
	case "united states of america", "usa":
		return "United States"
	case "uk", "great britain":
		return "United Kingdom"
	case "the netherlands":
		return "Netherlands"
	}
	return strings.TrimSpace(name)
}

func protocolName(t string) string {
	switch strings.ToLower(t) {
	case "wireguard":
		return "WireGuard"
	case "openvpn":
		return "OpenVPN"
	}
	return t
}

func parsePeers(s string) []peer {
	var out []peer
	for _, item := range strings.Split(s, ",") {
		name, raw, ok := strings.Cut(strings.TrimSpace(item), "=")
		if !ok || name == "" {
			continue
		}
		if _, err := url.Parse(raw); err == nil {
			out = append(out, peer{Name: name, URL: raw})
		}
	}
	return out
}

func splitList(s string) []string {
	out := []string{}
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// --- Docker, through the socket proxy ---

type dockerClient struct {
	base string
}

type container struct {
	Names  []string          `json:"Names"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
}

func (c container) name() string {
	if len(c.Names) == 0 {
		return ""
	}
	return strings.TrimPrefix(c.Names[0], "/")
}

// gluetuns lists containers carrying sleipnir.enable=true, stopped ones included, so a stopped
// container still shows instead of vanishing from the page.
func (d *dockerClient) gluetuns() ([]container, error) {
	filters := url.QueryEscape(`{"label":["sleipnir.enable=true"]}`)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(d.base + "/containers/json?all=1&filters=" + filters)
	if err != nil {
		return nil, fmt.Errorf("docker does not answer")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker: HTTP %d", resp.StatusCode)
	}
	var out []container
	return out, json.NewDecoder(resp.Body).Decode(&out)
}
