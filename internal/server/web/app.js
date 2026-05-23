"use strict";

const CATEGORIES = [
  { key: "exact_duplicate", label: "Exact duplicates" },
  { key: "near_duplicate", label: "Near duplicates" },
  { key: "low_quality", label: "Low quality" },
  { key: "useless_content", label: "Useless content" },
  { key: "overshoot", label: "Too many similar" },
];

let current = CATEGORIES[0].key;
let counts = {};

async function getJSON(url) {
  const r = await fetch(url);
  if (!r.ok) throw new Error(await r.text());
  return r.json();
}

async function postDecision(imageId, action) {
  await fetch("/api/decision", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ image_id: imageId, action }),
  });
}

function fmtDate(unix) {
  if (!unix) return "";
  return new Date(unix * 1000).toLocaleDateString();
}

function renderTabs() {
  const nav = document.getElementById("tabs");
  nav.innerHTML = "";
  for (const c of CATEGORIES) {
    const b = document.createElement("button");
    b.className = "tab" + (c.key === current ? " active" : "");
    const n = counts[c.key] || 0;
    b.innerHTML = `${c.label}<span class="badge">${n}</span>`;
    b.onclick = () => { current = c.key; renderTabs(); loadGroups(); };
    nav.appendChild(b);
  }
}

function basename(p) {
  const i = Math.max(p.lastIndexOf("/"), p.lastIndexOf("\\"));
  return i >= 0 ? p.slice(i + 1) : p;
}

function makeCard(img) {
  const card = document.createElement("div");
  card.className = "card";
  if (img.decision === "delete") card.classList.add("delete");
  if (img.decision === "keep" || img.is_keeper) card.classList.add("keep");

  const wrap = document.createElement("div");
  wrap.className = "thumb-wrap";
  const im = document.createElement("img");
  im.loading = "lazy";
  im.src = `/api/thumb/${img.id}`;
  im.onclick = () => showLightbox(img.id);
  wrap.appendChild(im);
  card.appendChild(wrap);

  if (img.is_keeper) {
    const flag = document.createElement("div");
    flag.className = "keeper-flag";
    flag.textContent = "Keep best";
    card.appendChild(flag);
  }

  const meta = document.createElement("div");
  meta.className = "meta";
  meta.innerHTML =
    `<div class="name">${basename(img.path)}</div>` +
    `<div>${img.width}×${img.height} · ${fmtDate(img.taken_at)}</div>` +
    `<div class="reason">${img.reason || ""}</div>`;
  card.appendChild(meta);

  const actions = document.createElement("div");
  actions.className = "actions";

  const del = document.createElement("button");
  del.className = "del" + (img.decision === "delete" ? " on" : "");
  del.textContent = "Delete";
  del.onclick = async () => {
    const next = img.decision === "delete" ? "clear" : "delete";
    await postDecision(img.id, next);
    img.decision = next === "clear" ? "" : "delete";
    refreshCardState(card, del, keep, img);
    loadSummary();
  };

  const keep = document.createElement("button");
  keep.className = "keep" + (img.decision === "keep" ? " on" : "");
  keep.textContent = "Keep";
  keep.onclick = async () => {
    const next = img.decision === "keep" ? "clear" : "keep";
    await postDecision(img.id, next);
    img.decision = next === "clear" ? "" : "keep";
    refreshCardState(card, del, keep, img);
    loadSummary();
  };

  actions.appendChild(del);
  actions.appendChild(keep);
  if (img.gphotos_url) {
    const open = document.createElement("a");
    open.className = "open";
    open.href = img.gphotos_url;
    open.target = "_blank";
    open.rel = "noopener";
    open.textContent = "Open ↗";
    actions.appendChild(open);
  }
  card.appendChild(actions);
  return card;
}

function refreshCardState(card, del, keep, img) {
  card.classList.toggle("delete", img.decision === "delete");
  card.classList.toggle("keep", img.decision === "keep" || img.is_keeper);
  del.classList.toggle("on", img.decision === "delete");
  keep.classList.toggle("on", img.decision === "keep");
}

async function loadGroups() {
  const container = document.getElementById("groups");
  const empty = document.getElementById("empty");
  container.innerHTML = "";
  const groups = await getJSON(`/api/groups?category=${encodeURIComponent(current)}`);
  empty.classList.toggle("hidden", groups.length > 0);

  const clustered = current === "exact_duplicate" || current === "near_duplicate" || current === "overshoot";

  for (const g of groups) {
    const div = document.createElement("div");
    div.className = "group";
    if (clustered) {
      const h = document.createElement("h3");
      h.textContent = `Group of ${g.images.length} — keep the highlighted one, delete the rest`;
      div.appendChild(h);
    }
    const grid = document.createElement("div");
    grid.className = "grid";
    for (const img of g.images) grid.appendChild(makeCard(img));
    div.appendChild(grid);
    container.appendChild(div);
  }
}

async function loadSummary() {
  const s = await getJSON("/api/summary");
  counts = s.counts || {};
  renderTabs();
  document.getElementById("count").textContent =
    `${s.candidates} of ${s.total} photos suggested for deletion`;
}

function showLightbox(id) {
  const lb = document.getElementById("lightbox");
  document.getElementById("lightbox-img").src = `/api/image/${id}`;
  lb.classList.remove("hidden");
}

document.getElementById("lightbox").onclick = () => {
  document.getElementById("lightbox").classList.add("hidden");
  document.getElementById("lightbox-img").src = "";
};

document.getElementById("select-all").onclick = async () => {
  const groups = await getJSON(`/api/groups?category=${encodeURIComponent(current)}`);
  const ids = [];
  for (const g of groups)
    for (const img of g.images)
      if (!img.is_keeper) ids.push(img.id);
  await Promise.all(ids.map((id) => postDecision(id, "delete")));
  await loadGroups();
  await loadSummary();
};

(async function init() {
  await loadSummary();
  await loadGroups();
})();
