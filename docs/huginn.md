# The test tunnel and huginn

### The test tunnel and huginn

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
