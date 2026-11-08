/* ESO map — interactive secret-flow visualisation. No external dependencies. */
(function () {
  "use strict";

  const G = ESO_GRAPH;
  const nodes = G.nodes || [];
  const edges = (G.edges || []).map((e, idx) =>
    Object.assign({ idx, fromItem: "", toItem: "" }, e),
  );
  const flows = G.flows || [];
  const diagnostics = G.diagnostics || [];

  const COLUMNS = [
    { title: "Providers & generators", types: ["store", "generator"] },
    { title: "Remote paths", types: ["remote"] },
    {
      title: "External Secrets Operator",
      types: ["externalsecret", "pushsecret"],
    },
    { title: "Kubernetes Secrets", types: ["secret"] },
    { title: "Consumers", types: ["workload"] },
  ];
  const EDGE_LABELS = {
    fetch: "fetched by ExternalSecret",
    generate: "generated",
    sync: "synced into Secret",
    consume: "consumed by workload",
    select: "read by PushSecret",
    push: "pushed to provider",
    contains: "lives in store",
  };
  const TYPE_LABELS = {
    store: "Stores",
    generator: "Generators",
    remote: "Remote paths",
    externalsecret: "ExternalSecrets",
    pushsecret: "PushSecrets",
    secret: "Secrets",
    workload: "Consumers",
  };

  const $ = (sel) => document.querySelector(sel);
  const el = (tag, attrs, ...children) => {
    const e = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs || {})) {
      if (v == null || v === false) continue;
      if (k === "class") e.className = v;
      else if (k === "text") e.textContent = v;
      else if (k.startsWith("on")) e.addEventListener(k.slice(2), v);
      else e.setAttribute(k, v);
    }
    for (const c of children.flat()) if (c != null) e.append(c);
    return e;
  };
  const qualified = (n) => (n.namespace ? n.namespace + "/" : "") + n.name;
  const hopKey = (node, item) => node + "\u0000" + (item || "");

  const byId = new Map(nodes.map((n) => [n.id, n]));
  const colOf = new Map();
  nodes.forEach((n) =>
    colOf.set(
      n.id,
      COLUMNS.findIndex((c) => c.types.includes(n.type)),
    ),
  );
  const outE = new Map(),
    inE = new Map();
  for (const e of edges) {
    if (!outE.has(e.from)) outE.set(e.from, []);
    if (!inE.has(e.to)) inE.set(e.to, []);
    outE.get(e.from).push(e);
    inE.get(e.to).push(e);
  }

  // ---------------------------------------------------------------------------
  // Lineage tracing (mirrors the Go flow semantics: empty item = whole node).

  function trace(starts) {
    const hops = new Set(),
      lit = new Set(),
      seenDown = new Set(),
      seenUp = new Set();
    const down = (n, i) => {
      const k = hopKey(n, i);
      if (seenDown.has(k)) return;
      seenDown.add(k);
      hops.add(k);
      for (const e of outE.get(n) || []) {
        if (i && e.fromItem && e.fromItem !== i) continue;
        lit.add(e.idx);
        down(e.to, e.toItem);
        // Show what is pushed into a remote key we reach from its store.
        if (e.type === "contains") up(e.to, "");
      }
    };
    const up = (n, i) => {
      const k = hopKey(n, i);
      if (seenUp.has(k)) return;
      seenUp.add(k);
      hops.add(k);
      for (const e of inE.get(n) || []) {
        if (i && e.toItem && e.toItem !== i) continue;
        lit.add(e.idx);
        up(e.from, e.fromItem);
      }
    };
    for (const [n, i] of starts) {
      down(n, i);
      up(n, i);
    }
    const touched = new Map(); // node -> Set(items) ("" = whole node)
    for (const k of hops) {
      const [n, i] = k.split("\u0000");
      if (!touched.has(n)) touched.set(n, new Set());
      touched.get(n).add(i);
    }
    return { touched, lit };
  }

  // ---------------------------------------------------------------------------
  // Ordering: barycentric sweeps reduce edge crossings between columns.

  function orderColumns() {
    const cols = COLUMNS.map(() => []);
    nodes.forEach((n) => {
      const c = colOf.get(n.id);
      if (c >= 0) cols[c].push(n.id);
    });
    const neighbours = new Map(nodes.map((n) => [n.id, new Set()]));
    for (const e of edges) {
      if (e.from === e.to) continue;
      neighbours.get(e.from)?.add(e.to);
      neighbours.get(e.to)?.add(e.from);
    }
    const pos = new Map();
    const reindex = () =>
      cols.forEach((col) =>
        col.forEach((id, i) => pos.set(id, i / Math.max(1, col.length - 1))),
      );
    reindex();
    for (let iter = 0; iter < 8; iter++) {
      const seq = iter % 2 === 0 ? [1, 2, 3, 4, 0] : [3, 2, 1, 0, 4];
      for (const c of seq) {
        const bary = new Map();
        for (const id of cols[c]) {
          const ns = [...neighbours.get(id)].filter((m) => colOf.get(m) !== c);
          bary.set(
            id,
            ns.length
              ? ns.reduce((s, m) => s + pos.get(m), 0) / ns.length
              : pos.get(id),
          );
        }
        cols[c].sort((a, b) => bary.get(a) - bary.get(b));
        reindex();
      }
    }
    return cols;
  }

  // ---------------------------------------------------------------------------
  // Rendering

  const canvas = $("#canvas");
  const svg = $("#edges");
  const cardEls = new Map(); // node id -> card element
  const rowEls = new Map(); // hopKey -> li element
  const pathEls = new Map(); // edge idx -> path element

  function renderHeader() {
    $("#root").textContent =
      (G.roots || []).join(", ") + " · " + G.files + " YAML files";
    const counts = {};
    nodes.forEach((n) => {
      counts[n.type] = (counts[n.type] || 0) + 1;
    });
    const stats = $("#stats");
    for (const t of Object.keys(TYPE_LABELS)) {
      if (!counts[t]) continue;
      stats.append(
        el(
          "span",
          { class: "stat t-" + t },
          el("i", { style: "background: var(--accent)" }),
          el("b", { text: String(counts[t]) }),
          TYPE_LABELS[t],
        ),
      );
    }
    stats.append(
      el(
        "span",
        { class: "stat" },
        el("b", { text: String(flows.length) }),
        "flows",
      ),
    );

    const nsSel = $("#namespace");
    [...new Set(nodes.map((n) => n.namespace).filter(Boolean))]
      .sort()
      .forEach((ns) => nsSel.append(el("option", { value: ns, text: ns })));

    const warns = diagnostics.filter((d) => d.severity === "warning").length;
    const btn = $("#diag-toggle");
    btn.textContent = warns
      ? `⚠ ${warns} warning${warns === 1 ? "" : "s"}`
      : `${diagnostics.length} notes`;
    btn.classList.toggle("has-warn", warns > 0);
    btn.hidden = diagnostics.length === 0;

    const legend = $("#legend");
    for (const [type, label] of Object.entries(EDGE_LABELS)) {
      const line = document.createElementNS(
        "http://www.w3.org/2000/svg",
        "svg",
      );
      line.setAttribute("width", "26");
      line.setAttribute("height", "8");
      const p = document.createElementNS("http://www.w3.org/2000/svg", "path");
      p.setAttribute("d", "M1 4 H25");
      p.setAttribute("class", type);
      p.setAttribute("stroke", `var(--e-${type})`);
      p.setAttribute("stroke-width", "2");
      if (type === "push" || type === "select")
        p.setAttribute("stroke-dasharray", "5 3");
      if (type === "contains") p.setAttribute("stroke-dasharray", "2 3");
      line.append(p);
      legend.append(el("span", {}, line, label));
    }
    legend.append(
      el("span", {
        class: "muted",
        text: "Click a card or row to trace its lineage · Esc to clear",
      }),
    );
  }

  function renderCard(n) {
    const attrs = n.attributes || {};
    const kindLine = el(
      "div",
      { class: "kind" },
      n.kind,
      attrs.backend
        ? el("span", { class: "backend", text: "· " + attrs.backend })
        : null,
      n.missing
        ? el("span", {
            class: "flag",
            title: "Referenced but not defined in the scanned files",
            text: "not in repo",
          })
        : null,
    );
    const head = el(
      "div",
      {
        class: "head",
        onclick: (ev) => {
          ev.stopPropagation();
          select(n.id, "");
        },
      },
      kindLine,
      el("div", { class: "title", text: n.label }),
      n.namespace
        ? el("div", { class: "ns", text: "ns: " + n.namespace })
        : n.type === "remote"
          ? el("div", { class: "ns", text: attrs.store || "" })
          : null,
    );
    const card = el(
      "div",
      {
        class: `node t-${n.type}` + (n.missing ? " missing" : ""),
        "data-id": n.id,
      },
      head,
    );
    if (n.items && n.items.length) {
      const ul = el("ul");
      for (const it of n.items) {
        const li = el(
          "li",
          {
            title: it.detail || "",
            onclick: (ev) => {
              ev.stopPropagation();
              select(n.id, it.id);
            },
          },
          el("span", { class: "label", text: it.label }),
          it.detail ? el("span", { class: "detail", text: it.detail }) : null,
        );
        rowEls.set(hopKey(n.id, it.id), li);
        ul.append(li);
      }
      card.append(ul);
    }
    cardEls.set(n.id, card);
    return card;
  }

  function renderColumns() {
    const order = orderColumns();
    COLUMNS.forEach((col, c) => {
      const colEl = el(
        "section",
        { class: "column" },
        el("h2", { text: col.title }),
      );
      if (!order[c].length)
        colEl.append(el("div", { class: "empty", text: "none found" }));
      order[c].forEach((id) => colEl.append(renderCard(byId.get(id))));
      canvas.append(colEl);
    });
    const defs = $("#markers");
    for (const type of Object.keys(EDGE_LABELS)) {
      const m = document.createElementNS(
        "http://www.w3.org/2000/svg",
        "marker",
      );
      m.setAttribute("id", "arrow-" + type);
      m.setAttribute("viewBox", "0 0 10 10");
      m.setAttribute("refX", "9");
      m.setAttribute("refY", "5");
      m.setAttribute("markerWidth", "7");
      m.setAttribute("markerHeight", "7");
      m.setAttribute("orient", "auto-start-reverse");
      const p = document.createElementNS("http://www.w3.org/2000/svg", "path");
      p.setAttribute("d", "M0 0 L10 5 L0 10 z");
      p.setAttribute("fill", `var(--e-${type})`);
      m.append(p);
      defs.append(m);
    }
    for (const e of edges) {
      const p = document.createElementNS("http://www.w3.org/2000/svg", "path");
      p.setAttribute("class", e.type);
      p.setAttribute("stroke", `var(--e-${e.type})`);
      p.setAttribute("marker-end", `url(#arrow-${e.type})`);
      const t = document.createElementNS("http://www.w3.org/2000/svg", "title");
      t.textContent = EDGE_LABELS[e.type] || e.type;
      p.append(t);
      svg.append(p);
      pathEls.set(e.idx, p);
    }
  }

  function anchor(nodeId, item) {
    const card = cardEls.get(nodeId);
    if (!card || card.classList.contains("hidden")) return null;
    const target =
      (item && rowEls.get(hopKey(nodeId, item))) || card.querySelector(".head");
    const base = canvas.getBoundingClientRect();
    const c = card.getBoundingClientRect();
    const r = target.getBoundingClientRect();
    return {
      left: c.left - base.left,
      right: c.right - base.left,
      y: r.top + r.height / 2 - base.top,
    };
  }

  function drawEdges() {
    svg.setAttribute("width", canvas.scrollWidth);
    svg.setAttribute("height", canvas.scrollHeight);
    for (const e of edges) {
      const p = pathEls.get(e.idx);
      const a = anchor(e.from, e.fromItem),
        b = anchor(e.to, e.toItem);
      if (!a || !b) {
        p.setAttribute("d", "");
        continue;
      }
      const forward = colOf.get(e.to) >= colOf.get(e.from);
      const x1 = forward ? a.right : a.left;
      const x2 = forward ? b.left - 2 : b.right + 2;
      const dx = Math.max(40, Math.abs(x2 - x1) / 2) * (forward ? 1 : -1);
      p.setAttribute(
        "d",
        `M${x1},${a.y} C${x1 + dx},${a.y} ${x2 - dx},${b.y} ${x2},${b.y}`,
      );
    }
  }

  // ---------------------------------------------------------------------------
  // Selection, highlighting and filtering

  let selected = null; // [nodeId, item]

  function applyHighlight(result) {
    document.body.classList.toggle("focus", !!result);
    for (const [id, card] of cardEls) {
      const items = result && result.touched.get(id);
      card.classList.toggle("dim", !!result && !items);
      card.classList.toggle("selected", !!selected && selected[0] === id);
      const whole = items && items.has("");
      for (const li of card.querySelectorAll("li"))
        li.classList.remove("hl", "dim");
      if (items) {
        for (const it of byId.get(id).items || []) {
          const li = rowEls.get(hopKey(id, it.id));
          if (items.has(it.id)) li.classList.add("hl");
          else if (!whole) li.classList.add("dim");
        }
      }
    }
    for (const [idx, p] of pathEls)
      p.classList.toggle("hl", !!result && result.lit.has(idx));
  }

  function select(nodeId, item) {
    selected = [nodeId, item];
    applyHighlight(trace([[nodeId, item]]));
    showDetails(nodeId, item);
  }

  function clearSelection() {
    selected = null;
    $("#details").hidden = true;
    applyFilter();
  }

  function matches(n, q) {
    const hay = [
      n.label,
      n.name,
      n.namespace,
      n.kind,
      n.provider,
      ...Object.values(n.attributes || {}),
    ];
    return hay.some((s) => s && String(s).toLowerCase().includes(q));
  }

  function applyFilter() {
    const q = $("#search").value.trim().toLowerCase();
    const ns = $("#namespace").value;
    if (!q && !ns) {
      cardEls.forEach((c) => c.classList.remove("hidden"));
      requestAnimationFrame(drawEdges);
      applyHighlight(selected ? trace([selected]) : null);
      return;
    }
    const starts = [];
    for (const n of nodes) {
      if (ns && n.namespace !== ns) continue;
      if (!q || matches(n, q)) {
        starts.push([n.id, ""]);
        continue;
      }
      for (const it of n.items || []) {
        if ((it.label + " " + (it.detail || "")).toLowerCase().includes(q))
          starts.push([n.id, it.id]);
      }
    }
    const result = trace(starts);
    cardEls.forEach((c, id) =>
      c.classList.toggle("hidden", !result.touched.has(id)),
    );
    requestAnimationFrame(() => {
      drawEdges();
      applyHighlight(selected ? trace([selected]) : q ? result : null);
    });
  }

  function showDetails(nodeId, item) {
    const n = byId.get(nodeId);
    const body = $("#details-body");
    body.replaceChildren();
    const it = item ? (n.items || []).find((x) => x.id === item) : null;
    const attrs = Object.assign(
      { kind: n.kind, name: n.name, namespace: n.namespace || "—" },
      n.attributes || {},
    );
    if (n.provider) attrs.provider = n.provider;

    body.append(
      el(
        "div",
        { class: `t-${n.type}` },
        el("span", { class: "tag", text: n.kind }),
      ),
      el("h2", { text: n.label }),
      n.missing
        ? el("p", {
            class: "muted",
            text: "Referenced, but not defined in any scanned file.",
          })
        : null,
      n.source
        ? el("p", {
            class: "mono muted",
            text: `${n.source.file}:${n.source.line}`,
          })
        : null,
      n.source && n.source.embeddedIn
        ? el("p", {
            class: "muted",
            text: "Embedded in " + n.source.embeddedIn,
          })
        : null,
    );
    if (it) {
      body.append(
        el("h3", { text: "Selected entry" }),
        el(
          "dl",
          { class: "kv" },
          el("dt", { text: "entry" }),
          el("dd", { class: "mono", text: it.label }),
          it.detail
            ? [el("dt", { text: "detail" }), el("dd", { text: it.detail })]
            : null,
        ),
      );
    }
    body.append(
      el("h3", { text: "Attributes" }),
      el(
        "dl",
        { class: "kv" },
        Object.entries(attrs).map(([k, v]) => [
          el("dt", { text: k }),
          el("dd", { class: "mono", text: v }),
        ]),
      ),
    );

    const relevant = flows.filter((f) =>
      f.hops.some(
        (h) => h.node === nodeId && (!item || !h.item || h.item === item),
      ),
    );
    body.append(el("h3", { text: `Flows through here (${relevant.length})` }));
    if (!relevant.length)
      body.append(el("p", { class: "muted", text: "No end-to-end flows." }));
    const ul = el("ul", { class: "flows" });
    relevant.slice(0, 200).forEach((f) => {
      const parts = f.summary.split(" → ");
      ul.append(
        el(
          "li",
          {},
          parts.flatMap((p, i) =>
            i ? [el("span", { class: "arrow", text: "→" }), p] : [p],
          ),
        ),
      );
    });
    body.append(ul);

    const diags = diagnostics.filter((d) => d.node === nodeId);
    if (diags.length) {
      body.append(
        el("h3", { text: "Diagnostics" }),
        el(
          "ul",
          { class: "flows" },
          diags.map((d) => el("li", { text: `${d.severity}: ${d.message}` })),
        ),
      );
    }
    $("#details").hidden = false;
  }

  function renderDiagnostics() {
    const list = $("#diag-list");
    const sorted = [...diagnostics].sort((a, b) =>
      a.severity === b.severity ? 0 : a.severity === "warning" ? -1 : 1,
    );
    for (const d of sorted) {
      list.append(
        el(
          "li",
          {
            class: d.severity,
            onclick: () => {
              if (d.node && byId.has(d.node)) {
                $("#diagnostics").hidden = true;
                select(d.node, "");
                scrollToNode(d.node);
              }
            },
          },
          d.message,
          d.file ? el("small", { class: "mono", text: d.file }) : null,
        ),
      );
    }
  }

  function scrollToNode(id) {
    const card = cardEls.get(id);
    if (card)
      card.scrollIntoView({
        behavior: "smooth",
        block: "center",
        inline: "center",
      });
  }

  // ---------------------------------------------------------------------------
  // Wiring

  renderHeader();
  renderColumns();
  renderDiagnostics();
  requestAnimationFrame(drawEdges);
  if (document.fonts && document.fonts.ready)
    document.fonts.ready.then(() => requestAnimationFrame(drawEdges));

  let resizeTimer;
  window.addEventListener("resize", () => {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(drawEdges, 80);
  });
  $("#search").addEventListener("input", applyFilter);
  $("#namespace").addEventListener("change", applyFilter);
  $("#viewport").addEventListener("click", clearSelection);
  $("#details-close").addEventListener("click", clearSelection);
  $("#diag-toggle").addEventListener("click", () => {
    $("#diagnostics").hidden = !$("#diagnostics").hidden;
  });
  $("#diag-close").addEventListener("click", () => {
    $("#diagnostics").hidden = true;
  });
  document.addEventListener("keydown", (ev) => {
    if (ev.key === "Escape") {
      clearSelection();
      $("#diagnostics").hidden = true;
    }
    if (ev.key === "/" && document.activeElement !== $("#search")) {
      ev.preventDefault();
      $("#search").focus();
    }
  });
})();
