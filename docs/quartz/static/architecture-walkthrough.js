import "./d3.v7.min.js";

const initialized = new WeakSet();

export function mountArchitectureWalkthrough(root) {
  if (!root?.isConnected || initialized.has(root)) return;
  initialized.add(root);
  const d3 = globalThis.d3;
  const walkthrough = d3.select(root);

  const TYPES = {
    control:  { label: "Control and status", v: "--control" },
    identity: { label: "Identity and ownership", v: "--identity" },
    consent:  { label: "Consent", v: "--consent" },
    data:     { label: "Protected data", v: "--data" },
    evidence: { label: "Evidence (metadata only)", v: "--evidence" },
  };

  const ZONES = [
    { id: "person",   label: "Member",                  x: 10,  y: 110, w: 150, h: 560 },
    { id: "consumer", label: "Consumer: SakhiCredit",   x: 175, y: 110, w: 200, h: 560 },
    { id: "idp",      label: "Identity",                x: 390, y: 10,  w: 470, h: 90 },
    { id: "core",     label: "GRAMStack core (gateway)",x: 390, y: 110, w: 470, h: 560 },
    { id: "registry", label: "Registry boundary",       x: 875, y: 110, w: 315, h: 390 },
    { id: "dedi",     label: "Public evidence",         x: 875, y: 515, w: 315, h: 155 },
  ];

  const NODES = [
    { id: "browser", z: "person", x: 85, y: 390, t: "Member's browser", s: "SakhiCredit tab + portal tab",
      d: "Sunita uses one browser. SakhiCredit runs in one tab; GRAMStack sign-in and consent run in a separate first-party tab. The browser never receives consumer credentials or retrieval grants.", c: "apps/sakhicredit/internal/application/web/" },
    { id: "backend", z: "consumer", x: 275, y: 250, t: "SakhiCredit backend", s: "Go service",
      d: "Independent consumer product. It owns its applications and an encrypted copy of received data. It calls the OAuth control API and the private mTLS data API.", c: "apps/sakhicredit/ (service.go, client.go)" },
    { id: "productdb", z: "consumer", x: 275, y: 520, t: "Product SQL", s: "PRODUCT database", db: true,
      d: "Encrypted application input, the request binding, the fetch claim and the encrypted received result. A reload reads this copy; it does not fetch again.", c: "apps/sakhicredit/internal/application/schema.sql" },
    { id: "hydra", z: "idp", x: 500, y: 58, t: "Ory Hydra", s: "OAuth / OIDC engine",
      d: "Issues consumer access tokens (client credentials) and runs the OIDC protocol for person login. The admin API is private and is used to introspect tokens.", c: "infra/railway (hydra-public, hydra-admin)" },
    { id: "esignet", z: "idp", x: 745, y: 58, t: "eSignet", s: "OIDC identity provider",
      d: "Official MOSIP eSignet with a fictional identity backend and OTP. Login gives an issuer-qualified person; it does not prove that a registry record is hers.", c: "infra/esignet, mocks/identity" },
    { id: "api", z: "core", x: 510, y: 170, t: "API and lifecycle", s: "/v1/access-requests",
      d: "Consumer API perimeter. Validates the request, applies consumer policy and terms, and commits an immutable snapshot. Drives the request FSM (looplab/fsm + SQL version checks).", c: "services/gateway/internal/consumer, access" },
    { id: "authn", z: "core", x: 510, y: 310, t: "AuthN and binding", s: "/auth/*, /interactions",
      d: "Nested OIDC flows with PKCE, state and nonce. After login, asks the registry adapter to verify that the person owns the record (subject binding).", c: "services/gateway/internal/authn, subjectbinding" },
    { id: "consent", z: "core", x: 510, y: 450, t: "Consent portal", s: "Consent Manager",
      d: "Shows the exact immutable English notice and recipient terms. Records approve, reject and withdraw; serves receipts and consent history.", c: "services/gateway/internal/consentportal, consentmanager" },
    { id: "workers", z: "core", x: 510, y: 590, t: "Durable workers", s: "sign · notify · clean",
      d: "SQL-leased background loops. Sign the consent artefact (ES256), activate consent, project status, expire and clean up. At-least-once delivery with reconciliation.", c: "services/gateway/internal/consentworker" },
    { id: "authz", z: "core", x: 745, y: 170, t: "AuthZ and grants", s: "grant + one-use redeem",
      d: "Checks signed source policy, verified subject and ACTIVE consent, then issues a short-lived scoped grant. Private admission redeems it atomically, only once.", c: "services/gateway/internal/authorization, enforcement" },
    { id: "retrieval", z: "core", x: 745, y: 310, t: "Private data listener", s: "TLS 1.3 mTLS",
      d: "Accepts the original consumer certificate plus the grant on GET /v1/access-requests/{id}/data. Dispatches once to the adapter after a discovery check of the pinned descriptor.", c: "services/gateway/internal/retrieval, discovery" },
    { id: "custody", z: "core", x: 745, y: 450, t: "Evidence custody", s: "signed receipts",
      d: "Commits signed receipts, anchor state and outbox in one transaction. Release waits for this durable evidence. A batch worker anchors to DeDi later.", c: "services/gateway/internal/evidence/custody, anchoring" },
    { id: "coredb", z: "core", x: 745, y: 590, t: "Core SQL", s: "CORE database", db: true,
      d: "Requests, snapshots, notices, decisions, signed artefacts, grants, reservations, receipts and outbox. 16 checksummed migrations. Stores no source records.", c: "services/gateway/migrations/" },
    { id: "adapter", z: "registry", x: 960, y: 310, t: "LokOS adapter", s: "Go adapter SDK",
      d: "Runs under registry control. Verifies subject ownership, validates the grant and mTLS caller, redeems once, projects only approved fields and signs a receipt before release.", c: "adapters/lokos, sdk/go/adapter" },
    { id: "lokos", z: "registry", x: 1110, y: 200, t: "LokOS source", s: "SHG records (mock)",
      d: "Fictional LokOS service with demo membership and savings data. A real deployment would use LokOS APIs with approved credentials.", c: "mocks/lokos" },
    { id: "adb", z: "registry", x: 1110, y: 430, t: "Adapter store", s: "ADAPTER database", db: true,
      d: "Durable attempt and context store: concurrent or repeated attempts fetch at most once, and no replay after a crash.", c: "sdk/go/adapter/attempt_store.go" },
    { id: "dedi", z: "dedi", x: 1030, y: 600, t: "DeDi node", s: "transparency log",
      d: "Accepts signed commitments (hashes, not data) and returns an inclusion proof that can be checked offline.", c: "services/gateway/internal/evidence/dedi, infra/railway/dedi" },
  ];

  const STEPS = [
    { f: "browser", t: "backend", k: "control", st: null, title: "Start an application",
      d: "Sunita picks the fields to share: membership status and, optionally, a savings summary. The request uses the product session and a CSRF token.", api: "SakhiCredit web form" },
    { f: "backend", t: "productdb", k: "control", st: null, title: "Save the application first",
      d: "SakhiCredit commits the encrypted application before it calls the platform, so a lost response can be recovered with the same idempotency key.", api: "Product SQL" },
    { f: "backend", t: "hydra", k: "control", st: null, title: "Get a consumer token",
      d: "The backend authenticates as client consumer-one with OAuth client credentials.", api: "Hydra token endpoint" },
    { f: "backend", t: "api", k: "control", st: "AUTHENTICATION_PENDING", title: "Create the access request",
      d: "The gateway introspects the token at the private Hydra admin API, checks purpose, fields and terms, and returns 202 with a first-party interaction URL.", api: "POST /v1/access-requests" },
    { f: "api", t: "coredb", k: "control", st: "AUTHENTICATION_PENDING", title: "Commit an immutable snapshot",
      d: "Snapshot, request, idempotency binding and outbox event commit atomically. No source record is stored.", api: "Core SQL" },
    { f: "browser", t: "authn", k: "identity", st: "AUTHENTICATION_PENDING", title: "Open the GRAMStack tab and sign in",
      d: "SakhiCredit hands Sunita to the gateway's interaction page in a separate tab.", api: "GET /interactions/{id} → /auth/login" },
    { f: "authn", t: "esignet", k: "identity", st: "BINDING_PENDING", title: "Federated login at eSignet",
      d: "OIDC code flow through Hydra with PKCE, state and nonce. Login proves who she is, not that the LokOS record is hers.", api: "/auth/provider/callback" },
    { f: "authn", t: "adapter", k: "identity", st: "BINDING_PENDING", title: "Ask the registry to verify ownership",
      d: "The gateway sends the verified person and the request to the adapter over mTLS.", api: "POST /internal/v1/subject-verifications" },
    { f: "adapter", t: "lokos", k: "identity", st: "CONSENT_PENDING", title: "Registry confirms the member",
      d: "The adapter maps the approved subject to her LokOS membership. Only then can she see the consent notice.", api: "LokOS native lookup" },
    { f: "browser", t: "consent", k: "consent", st: "CONSENT_PENDING", title: "Read the exact notice and approve",
      d: "The portal shows the fields, purpose, recipient terms and processors. She ticks the confirmation and approves, or rejects.", api: "POST /interactions/{id}/decision" },
    { f: "consent", t: "workers", k: "consent", st: "CONSENTED", title: "Sign and activate the consent",
      d: "A worker signs the consent artefact with ES256 and activates it. The receipt binds the notice fingerprint, channel and template version.", api: "Consent workers" },
    { f: "backend", t: "api", k: "control", st: "CONSENTED", title: "Poll the request status",
      d: "SakhiCredit checks progress with its OAuth token. It sees metadata only: ready, rejected, withdrawn, cancelled or expired.", api: "GET /v1/access-requests/{id}" },
    { f: "backend", t: "authz", k: "control", st: "AUTHORIZED", title: "Ask for a retrieval grant",
      d: "The backend asks for a grant with a stable idempotency key. The gateway checks signed source policy, the verified subject and ACTIVE consent, then signs a short-lived scoped grant.", api: "POST /v1/access-requests/{id}/grant" },
    { f: "backend", t: "retrieval", k: "data", st: "AUTHORIZED", title: "Fetch over consumer mTLS",
      d: "SakhiCredit commits one fixed-deadline fetch claim, then calls the private listener with its client certificate and the grant.", api: "GET /v1/access-requests/{id}/data" },
    { f: "retrieval", t: "adapter", k: "data", st: "AUTHORIZED", title: "Dispatch once to the adapter",
      d: "Before dispatch, the gateway checks the adapter's public manifest and keys against the pinned descriptor. Unknown keys or hosts stop here.", api: "POST /internal/v1/retrievals" },
    { f: "adapter", t: "authz", k: "control", st: "REDEEMING", title: "Atomic one-use admission",
      d: "The SDK validates the grant, issuer, audience and caller certificate, then redeems it. Withdrawal and redemption lock the same consent row, so only one can win.", api: "POST /internal/v1/grants/redeem" },
    { f: "adapter", t: "lokos", k: "data", st: "REDEEMING", title: "Read the source and project fields",
      d: "One native read. The adapter keeps only the approved fields; extra source fields cannot leak.", api: "LokOS native read" },
    { f: "adapter", t: "custody", k: "evidence", st: "EVIDENCE_COMMITTED", title: "Store signed evidence before release",
      d: "The adapter's signed receipt is committed to custody with a pending anchor. The data is not released until this succeeds.", api: "POST /internal/v1/evidence" },
    { f: "adapter", t: "retrieval", k: "data", st: "RELEASE_ATTEMPTED", title: "Return the approved fields",
      d: "Data stays in gateway and adapter memory only for this request.", api: "Adapter response" },
    { f: "retrieval", t: "backend", k: "data", st: "COMPLETED", title: "Release to SakhiCredit",
      d: "The approved fields and the receipt reference go back over the same mTLS connection. A second fetch with the same consent is refused.", api: "200 response" },
    { f: "backend", t: "productdb", k: "data", st: "COMPLETED", title: "Store the encrypted result",
      d: "SakhiCredit shows membership status and savings. It does not make a lending decision. A reload reads this encrypted copy.", api: "Product SQL" },
    { f: "custody", t: "dedi", k: "evidence", st: "COMPLETED", title: "Anchor the commitment on DeDi",
      d: "A batch worker publishes a signed commitment and verifies the inclusion proof. Until then the receipt shows \"DeDi anchoring pending\".", api: "DeDi publication API" },
    { f: "backend", t: "custody", k: "evidence", st: "COMPLETED", title: "Report how the data is handled",
      d: "The recipient reports retained, deleted or a retention exception (legal hold). This is recorded as the recipient's claim, not a verified deletion.", api: "PUT /v1/disclosures/{receipt}/handling-reports/{report}" },
    { f: "browser", t: "consent", k: "consent", st: "COMPLETED", title: "Check history, withdraw or correct",
      d: "After a fresh sign-in, she sees each request, the fields used, \"1 of 1 retrievals used\", the receipt, DeDi status, and can raise a correction case.", api: "GET /consent-history/{registry}" },
  ];
  STEPS.forEach((s, i) => s.i = i);

  const STATES = ["AUTHENTICATION_PENDING", "BINDING_PENDING", "CONSENT_PENDING", "CONSENTED", "AUTHORIZED", "REDEEMING", "EVIDENCE_COMMITTED", "RELEASE_ATTEMPTED", "COMPLETED"];
  const NW = 150, NH = 46;
  const byId = Object.fromEntries(NODES.map(n => [n.id, n]));

  const svg = walkthrough.select("#graph").append("svg").attr("viewBox", "0 0 1200 680").attr("role", "img")
    .attr("aria-label", "GRAMStack architecture: member browser, SakhiCredit consumer, identity, GRAMStack core, registry adapter and DeDi evidence");
  const defs = svg.append("defs");
  Object.keys(TYPES).forEach(k => {
    defs.append("marker").attr("id", "m-" + k).attr("viewBox", "0 0 10 10").attr("refX", 9).attr("refY", 5)
      .attr("markerWidth", 7).attr("markerHeight", 7).attr("orient", "auto-start-reverse")
      .append("path").attr("d", "M0,0 L10,5 L0,10 z").attr("class", "mk-" + k);
  });

  svg.append("g").selectAll("g").data(ZONES).join("g").each(function (z) {
    const g = d3.select(this);
    g.append("rect").attr("x", z.x).attr("y", z.y).attr("width", z.w).attr("height", z.h).attr("rx", 10)
      .attr("class", "zone").style("fill", "var(--zone)").style("stroke", "var(--line)");
    g.append("text").attr("class", "zone-label").attr("x", z.x + 12).attr("y", z.y + 20).text(z.label);
  });

  // Edge geometry: from box border to box border, curved; parallel edges between the same pair are offset.
  function border(n, tx, ty) {
    const dx = tx - n.x, dy = ty - n.y, hw = NW / 2 + 2, hh = NH / 2 + 2;
    const s = Math.min(hw / Math.abs(dx || 1e-6), hh / Math.abs(dy || 1e-6));
    return [n.x + dx * s, n.y + dy * s];
  }
  const pairCount = {};
  STEPS.forEach(s => {
    const key = [s.f, s.t].sort().join("|");
    pairCount[key] = (pairCount[key] || 0) + 1;
    s.lane = pairCount[key];
  });
  STEPS.forEach(s => {
    const a = byId[s.f], b = byId[s.t];
    const key = [s.f, s.t].sort().join("|");
    const total = pairCount[key];
    const dir = s.f < s.t ? 1 : -1;
    const bend = (s.lane - (total + 1) / 2) * 34 * dir + (total === 1 ? 18 : 0);
    const mx = (a.x + b.x) / 2, my = (a.y + b.y) / 2;
    const len = Math.hypot(b.x - a.x, b.y - a.y) || 1;
    const nx = -(b.y - a.y) / len, ny = (b.x - a.x) / len;
    const cx = mx + nx * bend, cy = my + ny * bend;
    const p0 = border(a, cx, cy), p1 = border(b, cx, cy);
    s.path = `M${p0[0]},${p0[1]} Q${cx},${cy} ${p1[0]},${p1[1]}`;
    s.lx = 0.25 * p0[0] + 0.5 * cx + 0.25 * p1[0];
    s.ly = 0.25 * p0[1] + 0.5 * cy + 0.25 * p1[1];
  });

  // Storage links: which component writes to which database (not numbered steps).
  const LINKS = [["consent", "coredb"], ["workers", "coredb"], ["authz", "coredb"], ["custody", "coredb"], ["adapter", "adb"], ["authn", "hydra"]];
  svg.append("g").selectAll("line").data(LINKS).join("line").each(function ([a, b]) {
    const A = byId[a], B = byId[b], p0 = border(A, B.x, B.y), p1 = border(B, A.x, A.y);
    d3.select(this).attr("x1", p0[0]).attr("y1", p0[1]).attr("x2", p1[0]).attr("y2", p1[1])
      .style("stroke", "var(--node-line)").style("stroke-width", 1.2).style("stroke-dasharray", "3 4");
  });

  const edges = svg.append("g").selectAll("path").data(STEPS).join("path")
    .attr("class", d => "edge e-" + d.k).attr("d", d => d.path)
    .attr("marker-end", d => `url(#m-${d.k})`);

  const nodes = svg.append("g").selectAll("g").data(NODES).join("g")
    .attr("class", d => "node" + (d.db ? " db" : "")).attr("transform", d => `translate(${d.x - NW / 2},${d.y - NH / 2})`);
  nodes.append("rect").attr("width", NW).attr("height", NH).attr("rx", d => d.db ? 14 : 7);
  nodes.append("text").attr("x", NW / 2).attr("y", 19).attr("text-anchor", "middle").attr("font-weight", 650).text(d => d.t);
  nodes.append("text").attr("class", "sub").attr("x", NW / 2).attr("y", 35).attr("text-anchor", "middle").text(d => d.s);

  const nums = svg.append("g").selectAll("g").data(STEPS).join("g").attr("class", "enum")
    .attr("transform", d => `translate(${d.lx},${d.ly})`);
  nums.append("circle").attr("r", 9);
  nums.append("text").text(d => d.i + 1);

  const dot = svg.append("circle").attr("r", 6).style("fill", "var(--hl)").style("stroke", "var(--text)").style("display", "none");

  function paint() {
    Object.keys(TYPES).forEach(k => {
      const c = `var(${TYPES[k].v})`;
      svg.selectAll(".e-" + k).style("stroke", c);
      svg.selectAll(".mk-" + k).style("fill", c);
    });
    nums.select("circle").style("fill", d => `var(${TYPES[d.k].v})`);
  }

  // Legend with type filters
  const hidden = new Set();
  const legend = walkthrough.select("#legend").selectAll("label").data(Object.entries(TYPES)).join("label");
  legend.append("input").attr("type", "checkbox").property("checked", true).on("change", (e, [k]) => {
    e.target.checked ? hidden.delete(k) : hidden.add(k); render();
  });
  legend.append("span").attr("class", "sw").style("background", ([k, v]) => `var(${v.v})`);
  legend.append("span").text(([, v]) => v.label);

  // States strip
  const states = walkthrough.select("#states").selectAll("span").data(STATES).join("span").text(d => d);

  // Step list
  const list = walkthrough.select("#steps").selectAll("li").data(STEPS).join("li")
    .attr("tabindex", 0)
    .on("click", (e, d) => { stop(); go(d.i); })
    .on("keydown", (e, d) => {
      if (e.key === "Enter" || e.key === " ") { e.preventDefault(); stop(); go(d.i); }
    });
  list.append("span").attr("class", "n").text(d => d.i + 1);
  list.append("span").text(d => d.title);

  let cur = 0, showAll = false, timer = null, nodeFocus = null;

  function render() {
    const s = STEPS[cur];
    const visible = d => !hidden.has(d.k);
    edges.style("display", d => visible(d) ? null : "none")
      .classed("on", d => !showAll && !nodeFocus && d.i === cur || (nodeFocus && (d.f === nodeFocus || d.t === nodeFocus)) || showAll)
      .classed("dim", d => nodeFocus ? !(d.f === nodeFocus || d.t === nodeFocus) : (!showAll && d.i !== cur && d.i > cur));
    nums.style("display", d => visible(d) ? null : "none")
      .classed("on", d => d.i === cur || showAll || (nodeFocus && (d.f === nodeFocus || d.t === nodeFocus)))
      .classed("dim", d => nodeFocus ? !(d.f === nodeFocus || d.t === nodeFocus) : (!showAll && d.i > cur));
    nodes.classed("active", d => nodeFocus ? d.id === nodeFocus : (!showAll && (d.id === s.f || d.id === s.t)));
    list.classed("on", d => d.i === cur).attr("aria-current", d => d.i === cur ? "step" : null);
    const si = s.st ? STATES.indexOf(s.st) : -1;
    states.classed("on", (d, i) => i === si).classed("done", (d, i) => i < si);

    walkthrough.select("#s-badge").text(cur + 1).style("background", `var(${TYPES[s.k].v})`);
    walkthrough.select("#s-title").text(s.title);
    walkthrough.select("#s-detail").text(s.d);
    walkthrough.select("#s-kv").html(
      `<div><b>From → to:</b> ${byId[s.f].t} → ${byId[s.t].t}</div>` +
      `<div><b>Kind:</b> ${TYPES[s.k].label}</div>` +
      `<div><b>Interface:</b> <code>${s.api}</code></div>` +
      (s.st ? `<div><b>Request state:</b> ${s.st}</div>` : `<div><b>Request state:</b> not created yet</div>`));
    const activeStep = list.filter(d => d.i === cur).node();
    const stepList = root.querySelector("#steps");
    if (activeStep && stepList.scrollHeight > stepList.clientHeight) {
      if (activeStep.offsetTop < stepList.scrollTop + stepList.offsetTop ||
          activeStep.offsetTop + activeStep.offsetHeight > stepList.scrollTop + stepList.offsetTop + stepList.clientHeight) {
        stepList.scrollTop = activeStep.offsetTop - stepList.offsetTop;
      }
    }
  }

  function animate() {
    const p = edges.filter(d => d.i === cur).node();
    if (!p || hidden.has(STEPS[cur].k) || matchMedia("(prefers-reduced-motion: reduce)").matches) { dot.style("display", "none"); return; }
    const L = p.getTotalLength();
    dot.style("display", null).interrupt().transition().duration(900).ease(d3.easeCubicInOut)
      .attrTween("transform", () => t => { const q = p.getPointAtLength(t * L); return `translate(${q.x},${q.y})`; })
      .attr("cx", 0).attr("cy", 0);
  }

  function go(i) {
    cur = (i + STEPS.length) % STEPS.length; showAll = false; nodeFocus = null;
    render(); animate();
  }
  function stop() { clearInterval(timer); timer = null; walkthrough.select("#play").text("Play"); }
  walkthrough.select("#next").on("click", () => { stop(); go(cur + 1); });
  walkthrough.select("#prev").on("click", () => { stop(); go(cur - 1); });
  walkthrough.select("#all").on("click", () => { stop(); showAll = !showAll; nodeFocus = null; dot.style("display", "none"); render(); });
  walkthrough.select("#play").on("click", () => {
    if (timer) return stop();
    walkthrough.select("#play").text("Pause");
    if (cur === STEPS.length - 1) go(0); else go(cur);
    timer = setInterval(() => { if (cur === STEPS.length - 1) return stop(); go(cur + 1); }, 2600);
  });
  root.addEventListener("keydown", e => {
    if (e.key === "ArrowRight") { stop(); go(cur + 1); }
    if (e.key === "ArrowLeft") { stop(); go(cur - 1); }
  });

  // Tooltips and node focus
  const tip = walkthrough.select("#tip");
  function showTip(e, html) { tip.html(html).style("display", "block").style("left", Math.min(e.clientX + 14, innerWidth - 340) + "px").style("top", (e.clientY + 14) + "px"); }
  nodes.on("mousemove", (e, d) => showTip(e, `<b>${d.t}</b>${d.d}<div style="margin-top:6px"><code>${d.c}</code></div>`))
    .on("mouseleave", () => tip.style("display", "none"))
    .on("click", (e, d) => { stop(); nodeFocus = nodeFocus === d.id ? null : d.id; showAll = false; dot.style("display", "none"); render(); });
  edges.on("click", (e, d) => { stop(); go(d.i); });
  nums.on("click", (e, d) => { stop(); go(d.i); })
    .on("mousemove", (e, d) => showTip(e, `<b>${d.i + 1}. ${d.title}</b><code>${d.api}</code>`))
    .on("mouseleave", () => tip.style("display", "none"));

  paint(); go(0);
  window.addCleanup?.(() => {
    stop();
    dot.interrupt();
  });
}
