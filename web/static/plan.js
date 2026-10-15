"use strict";

const views = {
  rendered: document.getElementById("rendered-view"),
  raw: document.getElementById("raw-view"),
};
const fullFmt = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });

function show(view) {
  for (const [name, node] of Object.entries(views)) if (node) node.hidden = name !== view;
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

const copyBtn = document.getElementById("copy");
const copyLabel = copyBtn.textContent;
copyBtn.addEventListener("click", async () => {
  try {
    await navigator.clipboard.writeText(document.getElementById(copyBtn.dataset.copy).textContent);
    copyBtn.textContent = "Copied";
  } catch {
    copyBtn.textContent = "Copy failed";
  }
  setTimeout(() => (copyBtn.textContent = copyLabel), 1500);
});

document.addEventListener("keydown", (e) => {
  if (e.target.closest("input, textarea") || e.metaKey || e.ctrlKey || e.altKey) return;
  if (e.key === "r") document.querySelector(`[data-view="${viewFromHash() === "raw" ? "rendered" : "raw"}"]`)?.click();
  if (e.key === "Escape" || e.key === "Backspace") document.getElementById("back").click();
});

for (const t of document.querySelectorAll("time[datetime]")) {
  t.textContent = fullFmt.format(new Date(t.dateTime));
}

const back = document.getElementById("back");
const listQuery = sessionStorage.getItem(back.dataset.list);
if (listQuery) back.href = back.getAttribute("href") + listQuery;

window.addEventListener("hashchange", () => show(viewFromHash()));
show(viewFromHash());
