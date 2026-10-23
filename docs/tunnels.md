# Tunnels

### How a tunnel shows up

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
