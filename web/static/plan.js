"use strict";

const views = {
  rendered: document.getElementById("rendered-view"),
  raw: document.getElementById("raw-view"),
};
const fullFmt = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });

function show(view) {
  for (const [name, node] of Object.entries(views)) node.hidden = name !== view;
  for (const btn of document.querySelectorAll("[data-view]")) {
    btn.setAttribute("aria-pressed", String(btn.dataset.view === view));
  }
}

function viewFromHash() {
  return location.hash === "#raw" ? "raw" : "rendered";
}

for (const btn of document.querySelectorAll("[data-view]")) {
  btn.addEventListener("click", () => {
    const view = btn.dataset.view;
    history.replaceState(null, "", view === "raw" ? "#raw" : location.pathname);
    show(view);
  });
}

document.getElementById("copy").addEventListener("click", async (e) => {
  const btn = e.currentTarget;
  try {
    await navigator.clipboard.writeText(views.raw.textContent);
    btn.textContent = "Copied";
  } catch {
    btn.textContent = "Copy failed";
  }
  setTimeout(() => (btn.textContent = "Copy"), 1500);
});

document.addEventListener("keydown", (e) => {
  if (e.target.closest("input, textarea") || e.metaKey || e.ctrlKey || e.altKey) return;
  if (e.key === "r") document.querySelector(`[data-view="${viewFromHash() === "raw" ? "rendered" : "raw"}"]`).click();
  if (e.key === "Escape" || e.key === "Backspace") document.getElementById("back").click();
});

for (const t of document.querySelectorAll("time[datetime]")) {
  t.textContent = fullFmt.format(new Date(t.dateTime));
}

const listQuery = sessionStorage.getItem("plans-list");
if (listQuery) document.getElementById("back").href = "/" + listQuery;

window.addEventListener("hashchange", () => show(viewFromHash()));
show(viewFromHash());
