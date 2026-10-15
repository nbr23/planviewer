"use strict";

const kinds = {
  plans: {
    api: "/api/plans",
    count: "size",
    link: (p) => "/p/" + encodeURIComponent(p.name),
    sub: (p) => p.name,
    countText: (p) => formatSize(p.size),
  },
  sessions: {
    api: "/api/sessions",
    count: "messages",
    link: (s) => "/s/" + encodeURIComponent(s.id),
    sub: (s) => [s.branch, s.id].filter(Boolean).join(" · "),
    countText: (s) => String(s.messages),
  },
};
const noun = document.body.dataset.kind;
const kind = kinds[noun];
const defaultDir = { title: "asc", project: "asc", created: "desc", updated: "desc", [kind.count]: "desc" };
const state = { q: "", sort: "updated", dir: "desc", selected: 0 };
let data = { total: 0, items: [] };
let requestSeq = 0;
let debounce;

const search = document.getElementById("q");
const tbody = document.querySelector("#items tbody");
const count = document.getElementById("count");
const empty = document.getElementById("empty");

const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
const fullFmt = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });
const shortFmt = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });
const yearFmt = new Intl.DateTimeFormat(undefined, { year: "numeric", month: "short", day: "numeric" });

function el(tag, props, ...children) {
  const node = document.createElement(tag);
  Object.assign(node, props);
  node.append(...children);
  return node;
}

function escapeRe(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function highlight(text) {
  const terms = state.q.trim().split(/\s+/).filter(Boolean).map(escapeRe);
  if (!terms.length) return [text];
  const re = new RegExp("(" + terms.join("|") + ")", "gi");
  return text.split(re).map((part, i) => (i % 2 ? el("mark", { textContent: part }) : part));
}

function relative(date) {
  const s = (Date.now() - date) / 1000;
  if (s < 60) return "just now";
  if (s < 3600) return rtf.format(-Math.round(s / 60), "minute");
  if (s < 86400) return rtf.format(-Math.round(s / 3600), "hour");
  if (s < 7 * 86400) return rtf.format(-Math.round(s / 86400), "day");
  return (date.getFullYear() === new Date().getFullYear() ? shortFmt : yearFmt).format(date);
}

function timeCell(iso, exact) {
  const date = new Date(iso);
  const t = el("time", { dateTime: iso, textContent: relative(date), title: fullFmt.format(date) });
  if (!exact) {
    t.textContent = "~" + t.textContent;
    t.title += "\nApproximate: no transcript found, showing last modified time";
  }
  return el("td", { className: "col-date" }, t);
}

function formatSize(bytes) {
  return bytes < 1024 ? bytes + " B" : (bytes / 1024).toFixed(1) + " KB";
}

function compare(a, b) {
  const k = state.sort;
  let r;
  if (k === "title" || k === "project") {
    r = (a[k] || "").localeCompare(b[k] || "", undefined, { sensitivity: "base", numeric: true });
  } else if (k === kind.count) {
    r = a[k] - b[k];
  } else {
    r = Date.parse(a[k]) - Date.parse(b[k]);
  }
  return (state.dir === "asc" ? r : -r) || Date.parse(b.updated) - Date.parse(a.updated);
}

function row(item, index) {
  const title = el("td", { className: "col-title" },
    el("a", { href: kind.link(item) }, ...highlight(item.title)),
    el("div", { className: "slug" }, ...highlight(kind.sub(item))));
  if (item.snippet) title.append(el("div", { className: "snippet" }, ...highlight(item.snippet)));
  const tr = el("tr", {},
    title,
    el("td", { className: "col-project" }, ...highlight(item.project || "")),
    timeCell(item.created, item.createdExact !== false),
    timeCell(item.updated, true),
    el("td", { className: "col-size" }, kind.countText(item)));
  if (index === state.selected) tr.classList.add("selected");
  return tr;
}

function render() {
  const items = [...data.items].sort(compare);
  state.selected = Math.min(state.selected, Math.max(items.length - 1, 0));
  tbody.replaceChildren(...items.map(row));
  empty.hidden = items.length > 0;
  count.textContent = items.length === data.total ? `${data.total} ${noun}` : `${items.length} of ${data.total}`;
  for (const btn of document.querySelectorAll("th button")) {
    const active = btn.dataset.sort === state.sort;
    btn.closest("th").setAttribute("aria-sort", active ? (state.dir === "asc" ? "ascending" : "descending") : "none");
  }
}

function writeURL() {
  const p = new URLSearchParams();
  if (state.q) p.set("q", state.q);
  if (state.sort !== "updated") p.set("sort", state.sort);
  if (state.dir !== defaultDir[state.sort]) p.set("dir", state.dir);
  const qs = p.toString() ? "?" + p : "";
  history.replaceState(null, "", location.pathname + qs);
  sessionStorage.setItem(noun + "-list", qs);
}

function readURL() {
  const p = new URLSearchParams(location.search);
  state.q = p.get("q") || "";
  state.sort = Object.hasOwn(defaultDir, p.get("sort")) ? p.get("sort") : "updated";
  const dir = p.get("dir");
  state.dir = dir === "asc" || dir === "desc" ? dir : defaultDir[state.sort];
}

async function load() {
  const id = ++requestSeq;
  const res = await fetch(kind.api + "?q=" + encodeURIComponent(state.q));
  if (!res.ok || id !== requestSeq) return;
  const body = await res.json();
  data = { total: body.total, items: body[noun] };
  render();
}

function moveSelection(delta) {
  const rows = tbody.rows;
  if (!rows.length) return;
  rows[state.selected]?.classList.remove("selected");
  state.selected = (state.selected + delta + rows.length) % rows.length;
  rows[state.selected].classList.add("selected");
  rows[state.selected].scrollIntoView({ block: "nearest" });
}

search.addEventListener("input", () => {
  state.q = search.value;
  state.selected = 0;
  writeURL();
  clearTimeout(debounce);
  debounce = setTimeout(load, 120);
});

search.addEventListener("keydown", (e) => {
  if (e.key === "ArrowDown" || e.key === "ArrowUp") {
    e.preventDefault();
    moveSelection(e.key === "ArrowDown" ? 1 : -1);
  } else if (e.key === "Enter") {
    tbody.rows[state.selected]?.querySelector("a").click();
  } else if (e.key === "Escape" && search.value) {
    search.value = "";
    search.dispatchEvent(new Event("input"));
  }
});

document.addEventListener("keydown", (e) => {
  if (e.key === "/" && document.activeElement !== search) {
    e.preventDefault();
    search.focus();
    search.select();
  }
});

document.querySelector("thead").addEventListener("click", (e) => {
  const key = e.target.closest("button")?.dataset.sort;
  if (!key) return;
  if (key === state.sort) {
    state.dir = state.dir === "asc" ? "desc" : "asc";
  } else {
    state.sort = key;
    state.dir = defaultDir[key];
  }
  state.selected = 0;
  writeURL();
  render();
});

readURL();
search.value = state.q;
writeURL();
load();
