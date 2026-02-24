let loadingTimer = null;
let loadingStartedAt = 0;

function showLoadingBar() {
  const bar = document.getElementById("loading-bar");
  if (!bar) return;
  if (loadingTimer) {
    window.clearTimeout(loadingTimer);
    loadingTimer = null;
  }
  loadingStartedAt = Date.now();
  bar.style.opacity = "1";
  bar.style.width = "70%";
}

function hideLoadingBar() {
  const bar = document.getElementById("loading-bar");
  if (!bar) return;
  const elapsed = Date.now() - loadingStartedAt;
  const wait = elapsed < 180 ? 180 - elapsed : 0;
  loadingTimer = window.setTimeout(() => {
    bar.style.width = "100%";
    window.setTimeout(() => {
      bar.style.opacity = "0";
      bar.style.width = "0%";
    }, 120);
    loadingTimer = null;
  }, wait);
}

function initThemeToggle() {
  const html = document.documentElement;
  const key = "go_starter_theme_mode";
  const modes = ["system", "light", "dark"];

  function apply(mode) {
    const prefersDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
    const active = mode === "system" ? (prefersDark ? "dark" : "light") : mode;
    html.setAttribute("data-theme", active === "dark" ? (html.dataset.darkTheme || "coffee") : (html.dataset.lightTheme || "silk"));
    document.querySelectorAll("[data-theme-icon]").forEach((el) => {
      el.classList.toggle("hidden", el.getAttribute("data-theme-icon") !== mode);
    });
    localStorage.setItem(key, mode);
  }

  const stored = localStorage.getItem(key);
  apply(modes.includes(stored) ? stored : "system");

  document.addEventListener("click", (event) => {
    const button = event.target.closest("[data-theme-toggle]");
    if (!button) return;
    const current = localStorage.getItem(key) || "system";
    const next = modes[(modes.indexOf(current) + 1) % modes.length];
    apply(next);
  });
}

function initDemoCharts() {
  if (typeof window.Chart === "undefined") return;
  document.querySelectorAll("canvas[data-demo-chart]").forEach((canvas) => {
    if (canvas.dataset.chartReady === "true") return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    new window.Chart(ctx, {
      type: "bar",
      data: {
        labels: ["Mon", "Tue", "Wed", "Thu", "Fri"],
        datasets: [{
          label: "Focus Hours",
          data: [2.5, 3.2, 4.1, 2.8, 3.6],
          borderWidth: 0,
          borderRadius: 6,
          backgroundColor: ["#2563eb", "#0ea5e9", "#14b8a6", "#22c55e", "#f59e0b"]
        }]
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        plugins: { legend: { display: false } },
        scales: {
          y: { beginAtZero: true, grid: { color: "rgba(127,127,127,0.15)" } },
          x: { grid: { display: false } }
        }
      }
    });
    canvas.dataset.chartReady = "true";
    canvas.style.height = "220px";
  });
}

function initHtmxHooks() {
  document.body.addEventListener("htmx:beforeRequest", showLoadingBar);
  document.body.addEventListener("htmx:afterSwap", () => {
    hideLoadingBar();
    initDemoCharts();
  });
  document.body.addEventListener("htmx:responseError", hideLoadingBar);
}

document.addEventListener("DOMContentLoaded", () => {
  initThemeToggle();
  initDemoCharts();
  initHtmxHooks();
});
