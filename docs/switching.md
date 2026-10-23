# Switching

### What a switch does

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

### The control-server key

```bash
docker run --rm qmcgaw/gluetun genkey
```

Put the output in `.env` as `GLUETUN_API_KEY`. Every gluetun here sets
`HTTP_CONTROL_SERVER_AUTH_DEFAULT_ROLE` from it. gluetun refuses to start with an empty key, so
compose is written to refuse to run without one (`${GLUETUN_API_KEY:?}`): a missing key fails the
deploy instead of taking a running tunnel down.
