<p align="center">
  <img src="cmd/sleipnir/web/icon.svg" width="96" alt="">
</p>

<h1 align="center">sleipnir</h1>

<p align="center">
  <a href="https://github.com/PlatanosVerdes/sleipnir/tags"><img src="https://img.shields.io/github/v/tag/PlatanosVerdes/sleipnir?label=release&color=2f7d3a" alt="Latest release"></a>
  <img src="https://img.shields.io/github/go-mod/go-version/PlatanosVerdes/sleipnir" alt="Go version">
</p>

A panel for gluetun VPN tunnels: every tunnel on the machine (and, optionally, on other machines),
where it exits, what uses it, and a way to move it to another country or server without restarting
anything. It does not decide which container uses which tunnel; that stays in compose. Its companion
**huginn** runs inside a tunnel and says where that tunnel really comes out.

## Running it

One image, two programs (`sleipnir`, the default command, and `huginn`). Label a gluetun with
`sleipnir.enable: "true"` and it shows up:

```yaml
docker-socket-proxy:            # read-only Docker API: listing containers only
  image: tecnativa/docker-socket-proxy:${DOCKER_SOCKET_PROXY_VERSION}
  environment: [CONTAINERS=1]
  volumes: ["/var/run/docker.sock:/var/run/docker.sock:ro"]
  networks: [sleipnir-internal]

sleipnir:
  image: ghcr.io/platanosverdes/sleipnir:${SLEIPNIR_VERSION}
  environment:
    - GLUETUN_API_KEY=${GLUETUN_API_KEY}
    - SLEIPNIR_NAME=Raspberry
  volumes: ["./appdata/gluetun/servers.json:/servers.json:ro"]   # country list and server counts
  networks: [media-network, sleipnir-internal]

gluetun:
  labels:
    sleipnir.enable: "true"
    sleipnir.used-by: "aceserve (HTTPS)"
  environment:
    - HTTP_CONTROL_SERVER_AUTH_DEFAULT_ROLE={"auth":"apikey","apikey":"${GLUETUN_API_KEY}"}

huginn:
  image: ghcr.io/platanosverdes/sleipnir:${SLEIPNIR_VERSION}
  command: ["huginn"]
  network_mode: "service:gluetun-test"
```

Generate the key with `docker run --rm qmcgaw/gluetun genkey` and put it in `.env` as
`GLUETUN_API_KEY`; the other settings are in [Configuration](docs/configuration.md).

## Docs

| | |
| :--- | :--- |
| [Configuration](docs/configuration.md) | Every environment variable and its default |
| [Tunnels](docs/tunnels.md) | The labels that make a tunnel show up, and the read-only Docker access |
| [Switching](docs/switching.md) | What a switch does, what it does not, and the control-server key |
| [huginn](docs/huginn.md) | The test tunnel and the exit check that runs inside it |
| [Other machines](docs/peers.md) | Showing several machines' tunnels on one page |
| [About](docs/about.md) | The names, the logo and how the page is built |

## Development

```sh
go test -race ./...
```

Every push to `main` is tagged with CalVer and published as `ghcr.io/platanosverdes/sleipnir:<tag>`
for arm64 through [PlatanosVerdes/workflows](https://github.com/PlatanosVerdes/workflows), which
then pins the tag in rpi-homeserver's `versions.env`.
