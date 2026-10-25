import { LitElement, html, css, nothing, repeat } from "./vendor/lit-all.min.js";

const ISO = { "Albania": "AL", "Argentina": "AR", "Australia": "AU", "Austria": "AT", "Belgium": "BE", "Brazil": "BR", "Bulgaria": "BG", "Canada": "CA", "Chile": "CL", "Colombia": "CO", "Croatia": "HR", "Cyprus": "CY", "Czech Republic": "CZ", "Denmark": "DK", "Estonia": "EE", "Finland": "FI", "France": "FR", "Georgia": "GE", "Germany": "DE", "Greece": "GR", "Hong Kong": "HK", "Hungary": "HU", "Iceland": "IS", "India": "IN", "Indonesia": "ID", "Ireland": "IE", "Israel": "IL", "Italy": "IT", "Japan": "JP", "Latvia": "LV", "Lithuania": "LT", "Luxembourg": "LU", "Malaysia": "MY", "Mexico": "MX", "Moldova": "MD", "Netherlands": "NL", "New Zealand": "NZ", "North Macedonia": "MK", "Norway": "NO", "Poland": "PL", "Portugal": "PT", "Romania": "RO", "Serbia": "RS", "Singapore": "SG", "Slovakia": "SK", "Slovenia": "SI", "South Africa": "ZA", "South Korea": "KR", "Spain": "ES", "Sweden": "SE", "Switzerland": "CH", "Taiwan": "TW", "Thailand": "TH", "Turkey": "TR", "Ukraine": "UA", "United Kingdom": "GB", "United States": "US", "Vietnam": "VN" };
const flagOf = (name) => { const c = ISO[name]; return c ? String.fromCodePoint(...[...c].map((ch) => 0x1F1A5 + ch.charCodeAt(0))) : "🌐"; };
const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;
const TIMEOUT = 90000;
const isUp = (t) => t.state === "running" && t.vpn === "running";
const keyOf = (m, t) => `${m.name}/${t.id}`;

const tickIcon = html`<svg class="tick" width="16" height="16" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M4 10.5l3.5 3.5L16 6"/></svg>`;

// <vpn-app> owns the data: it fetches every machine, runs the switch state machine and passes
// everything down. Cards only ask for actions, through a tunnel-action event.
class VpnApp extends LitElement {
  static properties = { machines: { state: true }, ops: { state: true }, loaded: { state: true } };

  static styles = css`
    :host { display: block; max-width: 1080px; margin: 0 auto; padding: 24px 16px 48px; }
    h1 { margin: 0 0 6px; display: flex; align-items: center; gap: 12px; }
    /* The logo is drawn in currentColor through a mask, so it follows the light and dark themes. */
    .logo { width: 76px; height: 64px; background: currentColor; flex: none;
      -webkit-mask: url(logo.svg) no-repeat center / contain; mask: url(logo.svg) no-repeat center / contain; }
    .word { font-family: "Anton", var(--font-ui); font-weight: 400; font-size: 46px; line-height: 1; letter-spacing: .02em;
      text-transform: uppercase; transform: skewX(-10deg); }
    .lede { color: var(--muted); font-size: 14px; margin: 0 0 18px; max-width: 65ch; }
    .note { color: var(--muted); font-size: 12px; margin-top: 28px; max-width: 72ch; }
  `;

  constructor() {
    super();
    this.machines = [];
    this.ops = {};
    this.loaded = false;
    this.addEventListener("tunnel-action", (e) => this.run(e.detail));
  }

  connectedCallback() {
    super.connectedCallback();
    this.load();
    // A card mid-switch or with its confirm open must not move under the user.
    this.idle = setInterval(() => { if (!Object.keys(this.ops).length && !this.busyUI) this.load(); }, 15000);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    clearInterval(this.idle);
    clearInterval(this.polling);
  }

  get busyUI() {
    return [...this.renderRoot.querySelectorAll("machine-row")].some((r) => r.busy);
  }

  async load() {
    try {
      const r = await fetch("/api/machines", { cache: "no-store" });
      this.machines = (await r.json()).machines || [];
    } catch (e) { /* keep the last picture on screen */ }
    this.loaded = true;
    this.advance();
  }

  setOp(k, op) {
    this.ops = op ? { ...this.ops, [k]: op } : Object.fromEntries(Object.entries(this.ops).filter(([key]) => key !== k));
  }

  async run({ machine: m, tunnel: t, kind, target }) {
    const k = keyOf(m, t);
    this.setOp(k, { kind, target, oldIP: t.ip, oldPlace: t.ip ? `${t.city || t.country}, ${t.country}` : "", step: 0, started: Date.now() });
    const url = m.peer
      ? `/api/peers/${encodeURIComponent(m.peer)}/tunnels/${encodeURIComponent(t.id)}/${kind}`
      : `/api/tunnels/${encodeURIComponent(t.id)}/${kind}`;
    try {
      const r = await fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(target ? { country: target } : {}) });
      if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error || `HTTP ${r.status}`);
      this.setOp(k, { ...this.ops[k], step: 1 });
    } catch (e) {
      this.finish(k, `Not accepted: ${e.message}`, true);
    }
    if (!this.polling) {
      this.polling = setInterval(async () => {
        await this.load();
        if (!Object.values(this.ops).some((o) => o.step < 3 && !o.error)) { clearInterval(this.polling); this.polling = null; }
      }, 2000);
    }
  }

  // Steps move on what gluetun reports, never on a timer: the tunnel is running, the exit IP has
  // changed, and it is in the country asked for (and the test container agrees, where there is one).
  advance() {
    for (const m of this.machines) for (const t of m.tunnels || []) {
      const k = keyOf(m, t);
      const op = this.ops[k];
      if (!op || op.step >= 3 || op.error) continue;
      if (Date.now() - op.started > TIMEOUT) { this.finish(k, `Did not settle in 90 s: ${t.error || "gluetun is " + (t.vpn || "unknown")}`, true); continue; }
      if (op.kind === "stop") {
        if (t.vpn === "stopped") this.finish(k, `${t.id} stopped`);
        continue;
      }
      let step = op.step;
      if (step === 1 && isUp(t) && t.ip && (op.kind === "start" || t.ip !== op.oldIP)) step = 2;
      if (step === 2) {
        const inCountry = !op.target || t.country === op.target;
        // huginn's fallback source reports a country code instead of a name.
        const probeAgrees = !t.probe || t.probe.error || !op.target || t.probe.country === op.target || t.probe.country === ISO[op.target];
        if (inCountry && probeAgrees) {
          this.finish(k, op.kind === "another" ? `Now on another server in ${t.country}` : `Now exiting through ${t.city ? t.city + ", " : ""}${t.country}`);
          continue;
        }
      }
      if (step !== op.step) this.setOp(k, { ...op, step });
    }
  }

  finish(k, text, error = false) {
    this.setOp(k, { ...this.ops[k], step: 3, banner: text, error, flash: !error });
    setTimeout(() => this.ops[k] && this.setOp(k, { ...this.ops[k], fading: true, flash: false }), error ? 8000 : 3200);
    setTimeout(() => this.setOp(k, null), error ? 8500 : 3700);
  }

  render() {
    const all = this.machines.flatMap((m) => (m.tunnels || []).map((t) => ({ machine: m.name, id: t.id, country: t.country, up: isUp(t) })));
    return html`
      <h1><span class="logo" aria-hidden="true"></span><span class="word">sleipnir</span></h1>
      <p class="lede">VPN tunnels. Every tunnel on each machine. Pick where each one exits; whatever uses it keeps working through the change.</p>
      ${!this.loaded ? html`<p class="note">Loading tunnels…</p>`
        : !this.machines.length ? html`<p class="note">No machine answered.</p>`
        : repeat(this.machines, (m) => m.name, (m) => html`<machine-row .machine=${m} .ops=${this.ops} .all=${all}></machine-row>`)}
      <p class="note">A switch changes gluetun's settings in memory: if the gluetun container restarts, it goes back to the country set in the machine's .env (VPN_COUNTRY). "Used by" comes from the sleipnir.used-by label on each tunnel; the panel infers nothing.</p>`;
  }
}

// <machine-row> is one machine and the carousel of its tunnels.
class MachineRow extends LitElement {
  static properties = { machine: { attribute: false }, ops: { attribute: false }, all: { attribute: false }, overflow: { state: true } };

  static styles = css`
    :host { display: block; margin: 26px 0 0; }
    :host(:first-of-type) { margin-top: 4px; }
    .head { display: flex; align-items: center; flex-wrap: wrap; gap: 6px 12px; margin-bottom: 10px; }
    h2 { font-size: 15px; margin: 0; }
    .meta { color: var(--muted); font-size: 12px; font-family: var(--font-data); }
    .arrows { margin-left: auto; display: flex; gap: 6px; }
    .arrows[hidden] { display: none; }
    .arrows button { width: 34px; height: 34px; padding: 0; border-radius: 50%; display: grid; place-items: center; font: inherit; font-size: 16px;
      border: 1px solid var(--line); background: var(--surface); color: var(--ink); cursor: pointer; }
    .arrows button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
    .rail { display: grid; grid-auto-flow: column; grid-auto-columns: min(340px, 86%); gap: 14px; overflow-x: auto;
      scroll-snap-type: x proximity; padding-bottom: 6px; scrollbar-width: thin; overscroll-behavior-x: contain; }
    .rail > * { scroll-snap-align: start; }
    .down { background: var(--surface); border: 1px dashed var(--line); border-radius: 14px; padding: 18px; color: var(--muted); font-size: 14px; }
  `;

  get busy() {
    return [...this.renderRoot.querySelectorAll("tunnel-card")].some((c) => c.busy);
  }

  firstUpdated() {
    const rail = this.renderRoot.querySelector(".rail");
    const measure = () => { this.overflow = rail.scrollWidth > rail.clientWidth + 4; };
    new ResizeObserver(measure).observe(rail);
    // A mouse wheel only scrolls vertically: over a rail that overflows, turn it sideways. At either
    // end the wheel goes back to the page, so the rail never traps it.
    rail.addEventListener("wheel", (e) => {
      if (rail.scrollWidth <= rail.clientWidth + 4 || Math.abs(e.deltaX) > Math.abs(e.deltaY)) return;
      const atStart = rail.scrollLeft <= 0, atEnd = rail.scrollLeft + rail.clientWidth >= rail.scrollWidth - 2;
      if ((e.deltaY < 0 && atStart) || (e.deltaY > 0 && atEnd)) return;
      e.preventDefault();
      rail.scrollBy({ left: e.deltaY, behavior: "auto" });
    }, { passive: false });
  }

  updated() {
    const rail = this.renderRoot.querySelector(".rail");
    if (rail) this.overflow = rail.scrollWidth > rail.clientWidth + 4;
  }

  scroll(dir) {
    const rail = this.renderRoot.querySelector(".rail");
    rail.scrollBy({ left: dir * rail.clientWidth * 0.8, behavior: reduced ? "auto" : "smooth" });
  }

  render() {
    const m = this.machine;
    const tunnels = m.tunnels || [];
    return html`
      <div class="head">
        <h2>${m.name}</h2>
        <span class="meta">${m.reachable ? `${tunnels.length} tunnel${tunnels.length === 1 ? "" : "s"}` : "no answer"}</span>
        <div class="arrows" ?hidden=${!this.overflow}>
          <button type="button" aria-label="Previous tunnel" @click=${() => this.scroll(-1)}>‹</button>
          <button type="button" aria-label="Next tunnel" @click=${() => this.scroll(1)}>›</button>
        </div>
      </div>
      <div class="rail">
        ${!m.reachable ? html`<div class="down">No answer: this machine's panel did not reply over Tailscale.</div>`
          : !tunnels.length ? html`<div class="down">No tunnel here carries the sleipnir.enable label.</div>`
          : repeat(tunnels, (t) => t.id, (t) => html`<tunnel-card .machine=${m} .tunnel=${t} .op=${this.ops[keyOf(m, t)]} .all=${this.all}></tunnel-card>`)}
      </div>`;
  }
}

// <tunnel-card> shows one tunnel, or its switch while one runs. It keeps only UI state (the
// country picked, the open confirm); the switch itself is <vpn-app>'s.
class TunnelCard extends LitElement {
  static properties = {
    machine: { attribute: false }, tunnel: { attribute: false }, op: { attribute: false }, all: { attribute: false },
    picked: { state: true }, asking: { state: true }, help: { state: true },
  };

  static styles = css`
    :host { display: block; background: var(--surface); border: 1px solid var(--line); border-radius: 14px; padding: 18px; min-width: 0; position: relative; overflow: hidden; }
    :host([down]) { border-style: dashed; }
    :host([flash]) { box-shadow: 0 0 0 2px var(--ok); transition: box-shadow .6s; }
    .top { display: flex; justify-content: space-between; align-items: center; gap: 8px; margin-bottom: 12px; }
    .label { font-size: 13px; font-weight: 600; font-family: var(--font-data); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .pill { display: inline-flex; align-items: center; gap: 6px; font-size: 13px; font-weight: 500; white-space: nowrap; }
    .dot { width: 9px; height: 9px; border-radius: 50%; background: var(--ok); box-shadow: 0 0 0 3px color-mix(in srgb, var(--ok) 22%, transparent); }
    .dot.busy { background: var(--accent); box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 25%, transparent); animation: pulse 1s ease-in-out infinite; }
    .dot.off { background: var(--muted); box-shadow: none; }
    .dot.bad { background: var(--bad); box-shadow: 0 0 0 3px color-mix(in srgb, var(--bad) 22%, transparent); }
    .where { display: flex; align-items: center; gap: 12px; }
    .flag { font-size: 32px; line-height: 1; }
    .city { font-size: 28px; font-weight: 700; letter-spacing: -.02em; line-height: 1.1; }
    .country { color: var(--muted); font-size: 14px; }
    dl { display: grid; grid-template-columns: auto 1fr; gap: 6px 14px; margin: 14px 0; font-size: 14px; }
    dt { color: var(--muted); } dd { margin: 0; min-width: 0; overflow-wrap: anywhere; }
    .mono { font-family: var(--font-data); font-size: 13px; font-variant-numeric: tabular-nums; }
    .chips { display: flex; flex-wrap: wrap; gap: 6px; }
    .chips span { background: var(--chip); border-radius: 6px; padding: 1px 8px; font-size: 12px; font-family: var(--font-data); }
    .whois { font-family: var(--font-data); font-size: 12px; line-height: 1.6; background: var(--chip); border-radius: 10px; padding: 10px 12px; margin-bottom: 12px; white-space: pre-wrap; }
    .whois b { color: var(--accent); font-weight: 500; }
    .unchecked { display: flex; align-items: center; gap: 6px; color: var(--muted); }
    .info { width: 20px; height: 20px; padding: 0; border-radius: 50%; font-size: 12px; font-weight: 700; font-family: var(--font-data); line-height: 1; color: var(--accent); }
    .help { font-size: 13px; line-height: 1.5; background: var(--chip); border-radius: 10px; padding: 10px 12px; margin: -6px 0 12px; }
    .help p { margin: 0 0 8px; }
    .help pre { margin: 0; font-family: var(--font-data); font-size: 12px; white-space: pre-wrap; overflow-wrap: anywhere; }
    .err { color: var(--bad); font-size: 13px; margin: 0 0 10px; }
    .actions { display: grid; grid-template-columns: 1fr auto; gap: 8px; }
    select, button { font: inherit; font-size: 14px; border-radius: 10px; padding: 9px 12px; }
    select { border: 1px solid var(--line); background: var(--surface); color: var(--ink); width: 100%; min-width: 0; }
    button { border: 1px solid var(--line); background: var(--surface); color: var(--ink); cursor: pointer; white-space: nowrap; }
    button.primary { background: var(--accent); color: var(--accent-ink); border-color: var(--accent); font-weight: 600; }
    button:disabled { opacity: .45; cursor: default; }
    button:focus-visible, select:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
    .row2 { display: flex; gap: 8px; margin-top: 8px; } .row2 button { flex: 1; }
    .warnline { color: var(--warn); font-size: 12px; margin-top: 8px; min-height: 16px; }
    .bar { height: 3px; border-radius: 3px; background: var(--chip); overflow: hidden; margin-bottom: 12px; }
    .bar i { display: block; height: 100%; background: var(--accent); transition: width .6s ease; }
    .steps { list-style: none; margin: 0; padding: 0; display: grid; gap: 8px; font-size: 14px; }
    .steps li { display: flex; align-items: center; gap: 10px; color: var(--muted); }
    .steps li.active { color: var(--ink); font-weight: 500; }
    .steps li.done { color: var(--ink); }
    .ic { width: 18px; height: 18px; flex: none; display: grid; place-items: center; }
    .spin { width: 14px; height: 14px; border-radius: 50%; border: 2px solid var(--line); border-top-color: var(--accent); animation: spin .8s linear infinite; }
    .tick { color: var(--ok); }
    .wait { width: 6px; height: 6px; border-radius: 50%; background: var(--line); }
    .banner { display: flex; align-items: center; gap: 10px; padding: 10px 12px; border-radius: 10px; margin-top: 12px; font-weight: 600; font-size: 14px; transition: opacity .4s; }
    .banner.ok { background: color-mix(in srgb, var(--ok) 14%, transparent); color: var(--ok); }
    .banner.bad { background: color-mix(in srgb, var(--bad) 14%, transparent); color: var(--bad); }
    .banner.gone { opacity: 0; }
    @keyframes spin { to { transform: rotate(360deg); } }
    @keyframes pulse { 50% { opacity: .45; } }
    @media (prefers-reduced-motion: reduce) { .spin, .dot.busy { animation: none; } .bar i, .banner, :host([flash]) { transition: none; } }
  `;

  // renderHelp is what to add to compose so huginn checks this tunnel from inside it.
  renderHelp(t) {
    return html`<div class="help">
      <p>Nothing checks where this tunnel really comes out; the exit above is what gluetun reports.
      To have huginn ask from inside the tunnel, add to its compose file:</p>
      <pre>huginn-${t.id}:
  image: ghcr.io/platanosverdes/sleipnir:\${SLEIPNIR_VERSION}
  command: ["huginn"]
  network_mode: "service:${t.id}"

# and on ${t.id} itself:
${t.id}:
  labels:
    sleipnir.probe: "http://${t.id}:8080/json"</pre>
      <p>huginn listens on 8080 inside the tunnel. If something already uses that port there, set
      <code>LISTEN_ADDR=:8081</code> on huginn and use that port in the label.</p>
    </div>`;
  }

  get busy() {
    return !!this.asking || (this.picked && this.picked !== this.current);
  }

  get current() {
    return this.tunnel.target || this.tunnel.country;
  }

  updated() {
    const t = this.tunnel;
    this.toggleAttribute("down", t.state !== "running");
    this.toggleAttribute("flash", !!(this.op && this.op.flash));
  }

  ask(kind, target) {
    const t = this.tunnel;
    const users = (t.used_by || []).length ? t.used_by.join(", ") : "Whatever uses it";
    const from = t.ip ? `${t.city ? t.city + ", " : ""}${t.country}` : "";
    this.asking = kind === "switch" ? {
      kind, target, action: "Switch",
      heading: `Switch ${t.id} to ${target}?`,
      text: `${from ? `It exits through ${from} now. ` : ""}${users} keeps working; open connections drop for a few seconds while it reconnects.`,
    } : kind === "another" ? {
      kind, target, action: "Reconnect",
      heading: `Another server in ${target}?`,
      text: `${t.id} reconnects through a different server in the same country. ${users} may lose open connections for a few seconds.`,
    } : {
      kind, target: "", action: "Stop", danger: true,
      heading: `Stop ${t.id}?`,
      text: `${users} has no internet until the tunnel is started again.`,
    };
  }

  act(kind, target) {
    this.asking = null;
    this.picked = null;
    this.dispatchEvent(new CustomEvent("tunnel-action", { bubbles: true, composed: true, detail: { machine: this.machine, tunnel: this.tunnel, kind, target } }));
  }

  render() {
    const op = this.op;
    if (op && op.step < 3 && !op.error) return this.renderSwitching(op);
    const t = this.tunnel;
    const stopped = t.state === "running" && t.vpn === "stopped";
    const down = t.state !== "running";
    const status = down ? html`<span class="pill"><span class="dot bad"></span>Container ${t.state}</span>`
      : stopped ? html`<span class="pill"><span class="dot off"></span>Stopped</span>`
      : isUp(t) ? html`<span class="pill"><span class="dot"></span>Connected · ${t.since}</span>`
      : html`<span class="pill"><span class="dot busy"></span>${t.vpn || "starting"}</span>`;
    const place = t.ip ? { flag: flagOf(t.country), city: t.city || t.country, country: t.country }
      : { flag: "🌐", city: stopped ? "No exit" : "—", country: stopped ? "tunnel stopped" : "waiting for its exit" };
    const picked = this.picked || this.current;
    const clash = this.all.find((o) => !(o.machine === this.machine.name && o.id === t.id) && o.up && o.country === picked && picked !== this.current);
    return html`
      <div class="top"><span class="label">${t.id}</span>${status}</div>
      ${t.error ? html`<p class="err">${t.error}</p>` : nothing}
      <div class="where"><span class="flag" aria-hidden="true">${place.flag}</span><div><div class="city">${place.city}</div><div class="country">${place.country}</div></div></div>
      <dl>
        <dt>Exit IP</dt><dd class="mono">${t.ip || "—"}</dd>
        <dt>Network</dt><dd>${t.org || "—"}${t.protocol ? ` · ${t.protocol}` : ""}</dd>
        <dt>Used by</dt><dd class="chips">${(t.used_by || []).length ? t.used_by.map((u) => html`<span>${u}</span>`) : "—"}</dd>
        ${t.probe ? nothing : html`<dt>Checked</dt><dd class="unchecked">Not checked
          <button class="info" aria-label="How to check this tunnel" aria-expanded=${!!this.help} @click=${() => { this.help = !this.help; }}>i</button></dd>`}
      </dl>
      ${!t.probe && this.help ? this.renderHelp(t) : nothing}
      ${t.probe ? (t.probe.error
        ? html`<div class="whois">${t.used_by[0] || "probe"}: ${t.probe.error}</div>`
        : html`<div class="whois">${t.used_by[0] || "probe"} says:\n  country  <b>${t.probe.country}</b>\n  city     ${t.probe.city}\n  org      ${t.probe.org}</div>`) : nothing}
      ${down ? nothing : stopped
        ? html`<div class="row2"><button type="button" class="primary" @click=${() => this.act("start", this.current)}>Start</button></div>`
        : html`
          <div class="actions">
            <select aria-label="Exit country for ${t.id}" @change=${(e) => { this.picked = e.target.value; }}>
              ${(this.machine.countries || []).map((c) => html`<option value=${c.name} ?selected=${c.name === picked}>${flagOf(c.name)} ${c.name} · ${c.servers} servers</option>`)}
            </select>
            <button class="primary" type="button" ?disabled=${picked === this.current} @click=${() => this.ask("switch", picked)}>Switch</button>
          </div>
          <div class="row2">
            <button type="button" @click=${() => this.ask("another", t.country || this.current)}>Another server</button>
            <button type="button" @click=${() => this.ask("stop")}>Stop</button>
          </div>
          ${this.asking ? html`<confirm-dialog .heading=${this.asking.heading} .text=${this.asking.text} .action=${this.asking.action} ?danger=${this.asking.danger}
            .warn=${this.asking.kind === "switch" && clash ? `${clash.id} on ${clash.machine} already exits through ${picked} on the same account.` : ""}
            @confirm=${() => this.act(this.asking.kind, this.asking.target)} @cancel=${() => { this.asking = null; }}></confirm-dialog>` : nothing}
          <div class="warnline">${clash ? `${clash.id} on ${clash.machine} already exits through ${picked} on the same account.` : ""}</div>`}
      ${op && op.banner ? html`<div class="banner ${op.error ? "bad" : "ok"} ${op.fading ? "gone" : ""}">${op.error ? nothing : tickIcon}<span>${op.banner}</span></div>` : nothing}`;
  }

  renderSwitching(op) {
    const t = this.tunnel;
    const labels = op.kind === "switch" ? [`Reconnecting through ${op.target}`, "Waiting for the tunnel", t.probe ? "Asking the test container where it is" : "Checking the new exit"]
      : op.kind === "another" ? [`Picking another server in ${op.target}`, "Waiting for the tunnel", "Checking the new exit"]
      : op.kind === "start" ? ["Starting the tunnel", "Waiting for the tunnel", "Checking the exit"]
      : ["Stopping the tunnel", "Waiting for it to stop", "Done"];
    return html`
      <div class="top"><span class="label">${t.id}</span><span class="pill"><span class="dot busy"></span>${op.kind === "stop" ? "Stopping" : "Switching"}</span></div>
      <div class="where"><span class="flag" aria-hidden="true">${flagOf(op.target)}</span>
        <div><div class="city">${op.target || t.country}</div><div class="country">${op.oldPlace ? `was ${op.oldPlace}` : " "}</div></div></div>
      <dl><dt>Used by</dt><dd class="chips">${(t.used_by || []).length ? t.used_by.map((u) => html`<span>${u}</span>`) : "—"}</dd></dl>
      <div class="bar"><i style="width:${Math.round((op.step / 3) * 100)}%"></i></div>
      <ul class="steps">${labels.map((l, i) => html`
        <li class=${i < op.step ? "done" : i === op.step ? "active" : ""}>
          <span class="ic">${i < op.step ? tickIcon : i === op.step ? html`<span class="spin"></span>` : html`<span class="wait"></span>`}</span>${l}
        </li>`)}</ul>`;
  }
}

// <confirm-dialog> is the modal before an action; Esc, Cancel or a click on the backdrop cancel it.
class ConfirmDialog extends LitElement {
  static properties = { heading: {}, text: {}, warn: {}, action: {}, danger: { type: Boolean } };

  static styles = css`
    dialog { width: min(420px, calc(100vw - 32px)); padding: 0; border: 1px solid var(--line); border-radius: 16px;
      background: var(--surface); color: var(--ink); font: 15px/1.5 var(--font-ui); box-shadow: 0 24px 60px rgb(0 0 0 / .28); }
    dialog::backdrop { background: rgb(10 12 18 / .45); backdrop-filter: blur(2px); }
    dialog[open] { animation: rise .16s ease-out; }
    dialog[open]::backdrop { animation: fade .16s ease-out; }
    .box { padding: 22px 22px 18px; }
    h2 { margin: 0 0 8px; font-size: 18px; font-weight: 700; letter-spacing: -.01em; }
    p { margin: 0; color: var(--muted); font-size: 14px; }
    .warn { color: var(--warn); margin-top: 10px; }
    .buttons { display: flex; justify-content: flex-end; gap: 8px; margin-top: 20px; }
    button { font: inherit; font-size: 14px; border-radius: 10px; padding: 9px 16px; border: 1px solid var(--line); background: var(--surface); color: var(--ink); cursor: pointer; }
    button.primary { background: var(--accent); color: var(--accent-ink); border-color: var(--accent); font-weight: 600; }
    button.danger { background: var(--bad); color: var(--accent-ink); border-color: var(--bad); font-weight: 600; }
    button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
    @keyframes rise { from { opacity: 0; transform: translateY(8px) scale(.98); } }
    @keyframes fade { from { opacity: 0; } }
    @media (prefers-reduced-motion: reduce) { dialog[open], dialog[open]::backdrop { animation: none; } }
  `;

  firstUpdated() {
    this.renderRoot.querySelector("dialog").showModal();
  }

  done(name) {
    this.renderRoot.querySelector("dialog").close();
    this.dispatchEvent(new CustomEvent(name));
  }

  render() {
    // The dialog has no padding, so a click whose target is the dialog itself landed on the backdrop.
    return html`<dialog aria-labelledby="h" aria-describedby="d" @cancel=${(e) => { e.preventDefault(); this.done("cancel"); }}
        @click=${(e) => { if (e.target === e.currentTarget) this.done("cancel"); }}>
      <div class="box">
        <h2 id="h">${this.heading}</h2>
        <p id="d">${this.text}</p>
        ${this.warn ? html`<p class="warn">${this.warn}</p>` : nothing}
        <div class="buttons">
          <button type="button" ?autofocus=${this.danger} @click=${() => this.done("cancel")}>Cancel</button>
          <button type="button" class=${this.danger ? "danger" : "primary"} ?autofocus=${!this.danger} @click=${() => this.done("confirm")}>${this.action}</button>
        </div>
      </div>
    </dialog>`;
  }
}

customElements.define("vpn-app", VpnApp);
customElements.define("machine-row", MachineRow);
customElements.define("tunnel-card", TunnelCard);
customElements.define("confirm-dialog", ConfirmDialog);
