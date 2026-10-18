# sleipnir

A panel for gluetun VPN tunnels: every tunnel on the machine (and, optionally, on other machines),
where it exits, what uses it, and a way to move it to another country or server without restarting
anything. Its companion **huginn** runs inside a tunnel and says where that tunnel really comes out.

> **Why the names.** In Norse myth, Sleipnir is Odin's eight-legged horse, the one that rides between
> the nine worlds; here it rides a tunnel from Lisbon to Warsaw while the containers on it keep
> working. Huginn is one of Odin's two ravens, sent out every morning to fly over the world and come
> back to tell him what is happening. This huginn flies out through the tunnel and reports the
> country it landed in. Both sit next to [bifrost](https://github.com/PlatanosVerdes/bifrost) and a
> server called streamhalla, so the theme was settled long before the code.

## Running it

One image, two programs (`sleipnir`, the default command, and `huginn`):

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

| Variable | Default | What it is |
| :--- | :--- | :--- |
| `GLUETUN_API_KEY` | required | The key every gluetun's control server is set to |
| `SLEIPNIR_NAME` | the hostname | This machine's name on the page |
| `SLEIPNIR_PEERS`, `SLEIPNIR_TOKEN`, `PEER_LISTEN_ADDR` | empty | Other machines' panels, the shared token, and where this one answers them |
| `DOCKER_HOST_URL` | `http://docker-socket-proxy:2375` | The read-only Docker API |
| `SERVERS_FILE` | `/servers.json` | gluetun's server list, for the countries and their server counts |
| `LISTEN_ADDR` | `:8090` (sleipnir), `:8080` (huginn) | Where each one serves its page |

The logo is a winged horse for Sleipnir: the head is Material Design Icons' `horse-variant-fast`
(Pictogrammers, Apache 2.0) without its speed lines, with a five-feather wing drawn to match it. The
title is set in Anton, slanted, from Google Fonts like the rest of the page's type.

The page is built from Lit components (one class per piece), with Lit vendored and served by
sleipnir itself, the same way bifrost does its page.

sleipnir lists every gluetun tunnel on this machine (and, optionally, on other
machines) and moves them: another exit country, another server in the same country, stop and
start. It does not decide which container uses which tunnel; that stays in compose.

## How a tunnel shows up

The panel lists containers carrying the label `sleipnir.enable=true`. Two more labels are optional:

| Label | What it does |
| :--- | :--- |
| `sleipnir.used-by` | Shown as chips under "Used by", comma separated. Free text: the panel reads nothing into it |
| `sleipnir.probe` | A URL returning JSON with `country`, `city`, `ip`, `org`. Shown as "… says", and a switch only counts as done when it agrees |

A tunnel without a probe shows "Not checked", with an ⓘ that prints the compose lines adding huginn
inside that tunnel and the label pointing at it.

So a new tunnel is a new gluetun service in compose with these labels and the same control-server
key; it appears on the page by itself. Each gluetun is one tunnel with one exit country at a time:
two countries at once means two gluetun services.

The panel finds containers through `docker-socket-proxy`, a read-only Docker API that only allows
listing containers (no exec, no start, no images), on an internal network nothing else joins.

## What a switch does

The panel talks to each gluetun's control server (`:8000`, by container name) with the key in
`GLUETUN_API_KEY`:

- **Switch** sends `PUT /v1/vpn/settings` with only `provider.server_selection.countries`. gluetun
  validates it and reconnects in place.
- **Another server** stops and starts the VPN loop; gluetun picks a random server within its filter
  on every start.
- **Stop / Start** set the loop's status.

The container never restarts, so whatever uses the tunnel (its HTTP proxy, or its network
namespace) carries on after a pause of a few seconds.

The page then watches the tunnel and only moves its steps on what gluetun reports: the VPN loop
running, then a new exit IP, then that IP in the country asked for, and the probe agreeing where
there is one. If that has not happened in 90 seconds, the card says so and why.

**What it does not do:** a switch lives in gluetun's memory. If the gluetun container restarts
(a deploy that recreates it, a reboot), it goes back to `SERVER_COUNTRIES` from compose, which reads
`VPN_COUNTRY` / `VPN_TEST_COUNTRY` in `.env`. To make a country permanent, change `.env`.

The page never sees gluetun's settings: they contain the VPN credentials.

## The control-server key

```bash
docker run --rm qmcgaw/gluetun genkey
```

Put the output in `.env` as `GLUETUN_API_KEY`. Every gluetun here sets
`HTTP_CONTROL_SERVER_AUTH_DEFAULT_ROLE` from it. gluetun refuses to start with an empty key, so
compose is written to refuse to run without one (`${GLUETUN_API_KEY:?}`): a missing key fails the
deploy instead of taking a running tunnel down.

## The test tunnel and huginn

`gluetun-test` is a second tunnel to try the panel on, exiting through `VPN_TEST_COUNTRY` (Turkey by
default). It must differ from the main tunnel's country: both use the same WireGuard key, and two
sessions on one server drop each other. The panel warns when a switch would put two tunnels in the
same country.

`huginn` lives in `gluetun-test`'s network namespace and reports, at request time, the exit it
sees, asking ipwho.is (country names) and falling back to ipinfo.io (country codes). Its JSON at
`http://gluetun-test:8080/json` is the test tunnel's probe. It is not published anywhere else.

Its server list starts as a copy of the main tunnel's: before the first start, copy
`appdata/gluetun/servers.json` into `appdata/gluetun-test/`, or gluetun picks from the list baked
into its image, which still names servers NordVPN has retired.

## Other machines

Each machine runs its own panel and only talks to its own gluetuns. To show other machines on one
page, set on the machine whose page you open:

```bash
SLEIPNIR_NAME=Raspberry
SLEIPNIR_PEERS=streamhalla=http://100.120.107.32:8092
SLEIPNIR_TOKEN=<same random string on every machine>
```

The panel asks each peer's panel for its tunnels and relays actions to it; it never talks to
another machine's gluetun. Peers answer on `:8092`, published on the Tailscale address only, and
that listener exists only when `SLEIPNIR_TOKEN` is set; every peer request must carry it. A peer
that does not answer shows as "No answer" and the rest of the page keeps working.


## Development

```sh
go test -race ./...
```

Every push to `main` is tagged with CalVer and published as `ghcr.io/platanosverdes/sleipnir:<tag>`
for arm64 through [PlatanosVerdes/workflows](https://github.com/PlatanosVerdes/workflows), which
then pins the tag in rpi-homeserver's `versions.env`.
