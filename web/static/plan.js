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

for (const copyBtn of document.querySelectorAll("[data-copy], [data-copy-text]")) {
  const copyLabel = copyBtn.textContent;
  let reset;
  copyBtn.addEventListener("click", async () => {
    clearTimeout(reset);
    try {
      const text = copyBtn.dataset.copyText ?? document.getElementById(copyBtn.dataset.copy).textContent;
      await navigator.clipboard.writeText(text);
      copyBtn.textContent = "Copied";
    } catch {
      copyBtn.textContent = "Copy failed";
    }
    reset = setTimeout(() => (copyBtn.textContent = copyLabel), 1500);
  });
}

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
