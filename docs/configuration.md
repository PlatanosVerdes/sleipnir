# Configuration

| Variable | Default | What it is |
| :--- | :--- | :--- |
| `GLUETUN_API_KEY` | required | The key every gluetun's control server is set to |
| `SLEIPNIR_NAME` | the hostname | This machine's name on the page |
| `SLEIPNIR_PEERS`, `SLEIPNIR_TOKEN`, `PEER_LISTEN_ADDR` | empty | Other machines' panels, the shared token, and where this one answers them |
| `DOCKER_HOST_URL` | `http://docker-socket-proxy:2375` | The read-only Docker API |
| `SERVERS_FILE` | `/servers.json` | gluetun's server list, for the countries and their server counts |
| `LISTEN_ADDR` | `:8090` (sleipnir), `:8080` (huginn) | Where each one serves its page |
