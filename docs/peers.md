# Other machines

### Other machines

Each machine runs its own panel and only talks to its own gluetuns. To show other machines on one
page, set on the machine whose page you open:

```bash
SLEIPNIR_NAME=Raspberry
SLEIPNIR_PEERS=streamhalla=http://100.120.107.32:8092
SLEIPNIR_TOKEN=<this machine's random string>
SLEIPNIR_PEER_TOKENS=streamhalla=<streamhalla's SLEIPNIR_TOKEN>
```

Give every machine its own `SLEIPNIR_TOKEN`, so one that leaks does not open the others. A peer
missing from `SLEIPNIR_PEER_TOKENS` is sent this machine's own token, which is how a single token
shared by every machine works.

The panel asks each peer's panel for its tunnels and relays actions to it; it never talks to
another machine's gluetun. Peers answer on `:8092`, published on the Tailscale address only, and
that listener exists only when `SLEIPNIR_TOKEN` is set; every peer request must carry it. A peer
that does not answer shows as "No answer" and the rest of the page keeps working.
