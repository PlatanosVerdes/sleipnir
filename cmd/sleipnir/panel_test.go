package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeGluetun keeps the state a real one would after the control-server calls the panel makes.
type fakeGluetun struct {
	mu      sync.Mutex
	key     string
	status  string
	country string
	ip      string
	puts    []string
}

func (g *fakeGluetun) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-API-Key") != g.key {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	switch r.Method + " " + r.URL.Path {
	case "GET /v1/vpn/status":
		json.NewEncoder(w).Encode(map[string]string{"status": g.status})
	case "PUT /v1/vpn/status":
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		g.status = b["status"]
		g.puts = append(g.puts, "status="+b["status"])
		json.NewEncoder(w).Encode(map[string]string{"outcome": "ok"})
	case "GET /v1/vpn/settings":
		json.NewEncoder(w).Encode(map[string]any{"type": "wireguard", "wireguard": map[string]string{"private_key": "SECRET"},
			"provider": map[string]any{"name": "nordvpn", "server_selection": map[string]any{"countries": []string{strings.ToLower(g.country)}}}})
	case "PUT /v1/vpn/settings":
		body, _ := io.ReadAll(r.Body)
		g.puts = append(g.puts, "settings="+string(body))
		var b struct {
			Provider struct {
				ServerSelection struct {
					Countries []string `json:"countries"`
				} `json:"server_selection"`
			} `json:"provider"`
		}
		json.Unmarshal(body, &b)
		g.country = b.Provider.ServerSelection.Countries[0]
		w.Write([]byte("settings updated"))
	case "GET /v1/publicip/ip":
		json.NewEncoder(w).Encode(map[string]string{"public_ip": g.ip, "country": g.country, "city": "Somewhere", "organization": "NordVPN"})
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

func newTestPanel(t *testing.T, g *fakeGluetun, labels map[string]string) *panel {
	t.Helper()
	gl := httptest.NewServer(g)
	t.Cleanup(gl.Close)
	dock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "sleipnir.enable") {
			t.Errorf("docker query without the label filter: %s", r.URL.RawQuery)
		}
		json.NewEncoder(w).Encode([]container{{Names: []string{"/gluetun-test"}, State: "running", Status: "Up 5 minutes (healthy)", Labels: labels}})
	}))
	t.Cleanup(dock.Close)
	return &panel{name: "rpi", docker: &dockerClient{base: dock.URL}, apiKey: "k", client: http.DefaultClient,
		countries: []country{{"Portugal", 39}, {"Turkey", 24}}, gluetunBase: func(string) string { return gl.URL }}
}

func TestTunnelsDescribeWithoutLeakingSettings(t *testing.T) {
	g := &fakeGluetun{key: "k", status: "running", country: "Turkey", ip: "1.2.3.4"}
	p := newTestPanel(t, g, map[string]string{"sleipnir.enable": "true", "sleipnir.used-by": "huginn, other"})
	rec := httptest.NewRecorder()
	p.serveTunnels(rec, httptest.NewRequest(http.MethodGet, "/api/tunnels", nil))
	body := rec.Body.String()
	if strings.Contains(body, "SECRET") || strings.Contains(body, "private_key") {
		t.Fatalf("gluetun settings leaked to the page: %s", body)
	}
	var out struct {
		Tunnels []tunnel `json:"tunnels"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	tun := out.Tunnels[0]
	if tun.ID != "gluetun-test" || tun.VPN != "running" || tun.Country != "Turkey" || tun.IP != "1.2.3.4" ||
		tun.Protocol != "WireGuard" || tun.Since != "5 minutes" || len(tun.UsedBy) != 2 || tun.Target != "Turkey" {
		t.Errorf("tunnel = %+v", tun)
	}
}

func TestSwitchSendsOnlyTheCountry(t *testing.T) {
	g := &fakeGluetun{key: "k", status: "running", country: "Turkey", ip: "1.2.3.4"}
	p := newTestPanel(t, g, map[string]string{"sleipnir.enable": "true"})
	rec := httptest.NewRecorder()
	p.serveAction(rec, httptest.NewRequest(http.MethodPost, "/api/tunnels/gluetun-test/switch", strings.NewReader(`{"country":"Portugal"}`)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if len(g.puts) != 1 || g.puts[0] != `settings={"provider":{"server_selection":{"countries":["Portugal"]}}}` {
		t.Errorf("puts = %v", g.puts)
	}
	rec = httptest.NewRecorder()
	p.serveAction(rec, httptest.NewRequest(http.MethodPost, "/api/tunnels/gluetun-test/switch", strings.NewReader(`{"country":"Atlantis"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown country accepted: %d", rec.Code)
	}
}

func TestAnotherServerStopsAndStarts(t *testing.T) {
	g := &fakeGluetun{key: "k", status: "running", country: "Turkey", ip: "1.2.3.4"}
	p := newTestPanel(t, g, map[string]string{"sleipnir.enable": "true"})
	rec := httptest.NewRecorder()
	p.serveAction(rec, httptest.NewRequest(http.MethodPost, "/api/tunnels/gluetun-test/another", nil))
	if strings.Join(g.puts, ",") != "status=stopped,status=running" {
		t.Errorf("puts = %v", g.puts)
	}
}

func TestOnlyLabelledTunnelsCanBeMoved(t *testing.T) {
	g := &fakeGluetun{key: "k", status: "running"}
	p := newTestPanel(t, g, map[string]string{"sleipnir.enable": "true"})
	rec := httptest.NewRecorder()
	p.serveAction(rec, httptest.NewRequest(http.MethodPost, "/api/tunnels/jellyfin/stop", nil))
	if rec.Code != http.StatusBadRequest || len(g.puts) != 0 {
		t.Errorf("an unlabelled container was acted on: %d %v", rec.Code, g.puts)
	}
}

func TestWrongKeyIsReported(t *testing.T) {
	g := &fakeGluetun{key: "other", status: "running"}
	p := newTestPanel(t, g, map[string]string{"sleipnir.enable": "true"})
	ts, _ := p.tunnels()
	if ts[0].Error != "gluetun refused the API key" {
		t.Errorf("error = %q", ts[0].Error)
	}
}

func TestPeerTokenIsRequired(t *testing.T) {
	p := &panel{token: "s3cret"}
	h := p.requireToken(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/tunnels", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/tunnels", nil)
	req.Header.Set("X-Panel-Token", "s3cret")
	rec = httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("right token: %d", rec.Code)
	}
}

func TestEachPeerGetsItsOwnToken(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("X-Panel-Token"))
		w.Write([]byte("{}"))
	}))
	defer srv.Close()
	p := &panel{token: "ours", client: srv.Client()}
	p.peers = withPeerTokens(parsePeers("streamhalla="+srv.URL+",other="+srv.URL), "streamhalla=theirs, unknown=x")
	for _, pr := range p.peers {
		var out map[string]any
		if err := p.peerCall(pr, http.MethodGet, "/api/tunnels", nil, &out); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(got, ",") != "theirs,ours" {
		t.Errorf("tokens sent = %v", got)
	}
}

func TestUnreachablePeerShowsAsNoAnswer(t *testing.T) {
	g := &fakeGluetun{key: "k", status: "running", country: "Turkey", ip: "1.2.3.4"}
	p := newTestPanel(t, g, map[string]string{"sleipnir.enable": "true"})
	p.peers = []peer{{Name: "streamhalla", URL: "http://127.0.0.1:1"}}
	rec := httptest.NewRecorder()
	p.serveMachines(rec, httptest.NewRequest(http.MethodGet, "/api/machines", nil))
	var out struct {
		Machines []machine `json:"machines"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Machines) != 2 || !out.Machines[0].Reachable || out.Machines[1].Reachable || out.Machines[1].Name != "streamhalla" {
		t.Errorf("machines = %+v", out.Machines)
	}
}

func TestCountriesCountWireGuardServers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	os.WriteFile(path, []byte(`{"version":1,"nordvpn":{"servers":[{"vpn":"wireguard","country":"Turkey"},{"vpn":"openvpn","country":"Turkey"},{"vpn":"wireguard","country":"Turkey"},{"vpn":"openvpn","country":"Chad"}]}}`), 0644)
	cs := loadCountries(path)
	if len(cs) != 1 || cs[0].Name != "Turkey" || cs[0].Servers != 2 {
		t.Errorf("countries = %+v", cs)
	}
	if canonicalCountry("Türkiye") != "Turkey" {
		t.Error("Türkiye not mapped")
	}
}
