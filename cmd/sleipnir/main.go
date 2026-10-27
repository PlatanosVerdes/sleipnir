// sleipnir lists the gluetun tunnels on this machine and moves them: another exit country,
// another server in the same one, stop and start. It knows nothing about what rides a tunnel
// beyond the labels on the gluetun container, so it works for any tunnel added later.
//
// Two listeners: one for the browser, behind Caddy like every other page here, and one for the
// panels on other machines, which requires a token because it is reachable over Tailscale.
package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

//go:embed web
var webFS embed.FS

// buildVersion is set at build time to the release tag.
var buildVersion = "local"

func main() {
	log.Printf("sleipnir %s", buildVersion)
	name := getenv("SLEIPNIR_NAME", "")
	if name == "" {
		name, _ = os.Hostname()
	}
	p := &panel{
		name:        name,
		docker:      &dockerClient{base: strings.TrimRight(getenv("DOCKER_HOST_URL", "http://docker-socket-proxy:2375"), "/")},
		apiKey:      os.Getenv("GLUETUN_API_KEY"),
		countries:   loadCountries(getenv("SERVERS_FILE", "/servers.json")),
		peers:       withPeerTokens(parsePeers(os.Getenv("SLEIPNIR_PEERS")), os.Getenv("SLEIPNIR_PEER_TOKENS")),
		token:       os.Getenv("SLEIPNIR_TOKEN"),
		client:      &http.Client{Timeout: 8 * time.Second},
		controlPort: getenv("GLUETUN_CONTROL_PORT", "8000"),
	}
	if p.apiKey == "" {
		log.Printf("sleipnir: GLUETUN_API_KEY is empty, gluetun will refuse every call")
	}

	web, _ := fs.Sub(webFS, "web")
	browser := http.NewServeMux()
	browser.Handle("/", http.FileServer(http.FS(web)))
	browser.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	browser.HandleFunc("/api/machines", p.serveMachines)
	browser.HandleFunc("/api/tunnels", p.serveTunnels)
	browser.HandleFunc("/api/tunnels/", p.serveAction)
	browser.HandleFunc("/api/peers/", p.servePeerAction)

	if addr := os.Getenv("PEER_LISTEN_ADDR"); addr != "" {
		if p.token == "" {
			log.Printf("sleipnir: PEER_LISTEN_ADDR set without SLEIPNIR_TOKEN, not opening it")
		} else {
			peer := http.NewServeMux()
			peer.HandleFunc("/api/tunnels", p.requireToken(p.serveTunnels))
			peer.HandleFunc("/api/tunnels/", p.requireToken(p.serveAction))
			go func() { log.Fatal(http.ListenAndServe(addr, peer)) }()
			log.Printf("sleipnir: peers on %s", addr)
		}
	}

	addr := getenv("LISTEN_ADDR", ":8090")
	log.Printf("sleipnir %s: %s, %d countries, %d peers", name, addr, len(p.countries), len(p.peers))
	log.Fatal(http.ListenAndServe(addr, browser))
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
