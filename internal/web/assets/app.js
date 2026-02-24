let loadingTimer = null;
let loadingStartedAt = 0;

function isBoostedHtmxNavigation(detail) {
  if (!detail) return false;
  const requestConfig = detail.requestConfig || {};
  return detail.boosted === true || requestConfig.boosted === true;
}

function resolveHtmxNavigationURL(detail) {
  const requestConfig = (detail && detail.requestConfig) || {};
  const pathInfo = (detail && detail.pathInfo) || {};
  const path = pathInfo.finalRequestPath || pathInfo.responsePath || requestConfig.path || window.location.href;
  try {
    return new URL(path, window.location.origin).toString();
  } catch (_) {
    return window.location.href;
  }
}

function getCookieValue(name) {
  const encodedName = `${encodeURIComponent(name)}=`;
  const cookies = document.cookie ? document.cookie.split(";") : [];
  for (const rawCookie of cookies) {
    const cookie = rawCookie.trim();
    if (!cookie.startsWith(encodedName)) continue;
    return decodeURIComponent(cookie.slice(encodedName.length));
  }
  return "";
}

function currentCSRFToken() {
  return getCookieValue("csrf_token");
}

function ensureCSRFField(form) {
  if (!(form instanceof HTMLFormElement)) return;
  const method = (form.getAttribute("method") || "get").toLowerCase();
  if (method === "get" || method === "dialog") return;

  const token = currentCSRFToken();
  if (!token) return;

  let field = form.querySelector('input[name="csrf_token"]');
  if (!(field instanceof HTMLInputElement)) {
    field = document.createElement("input");
    field.type = "hidden";
    field.name = "csrf_token";
    form.appendChild(field);
  }
  field.value = token;
}

function initCSRFProtection() {
  document.querySelectorAll("form").forEach((form) => ensureCSRFField(form));

  document.addEventListener("submit", (event) => {
    if (!(event.target instanceof HTMLFormElement)) return;
    ensureCSRFField(event.target);
  });

  document.body.addEventListener("htmx:beforeRequest", (event) => {
    const token = currentCSRFToken();
    if (!token || !event?.detail?.headers) return;
    event.detail.headers["X-CSRF-Token"] = token;
  });

  document.body.addEventListener("htmx:afterSwap", () => {
    document.querySelectorAll("form").forEach((form) => ensureCSRFField(form));
  });
}

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

function createNav() {
  function normalizePath(rawPath) {
    if (!rawPath) return "/";
    let path = rawPath;
    try {
      path = new URL(rawPath, window.location.origin).pathname;
    } catch (_) {
      path = rawPath;
    }
    if (path.length > 1 && path.endsWith("/")) {
      return path.slice(0, -1);
    }
    return path || "/";
  }

  function updateActiveNav(rawPath) {
    const path = normalizePath(rawPath || window.location.pathname);
    document.querySelectorAll("[data-nav-link]").forEach((link) => {
      const target = normalizePath(link.getAttribute("href") || "/");
      const active = target === "/" ? path === "/" : (path === target || path.startsWith(target + "/"));
      link.classList.toggle("btn-active", active);
    });
  }

  return {
    init() {
      updateActiveNav(window.location.pathname);

      document.body.addEventListener("htmx:afterSettle", (event) => {
        if (!isBoostedHtmxNavigation(event.detail)) return;
        updateActiveNav(resolveHtmxNavigationURL(event.detail));
      });

      document.body.addEventListener("htmx:historyRestore", () => {
        updateActiveNav(window.location.pathname);
      });
    }
  };
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

function createAnalytics() {
  let lastTrackedURL = "";
  let configured = false;

  function normalizeURL(url) {
    try {
      return new URL(url || window.location.href, window.location.origin).toString();
    } catch (_) {
      return window.location.href;
    }
  }

  function trackPageView(url) {
    if (typeof window.gtag !== "function") return;

    const absoluteURL = normalizeURL(url);
    if (absoluteURL === lastTrackedURL) return;
    lastTrackedURL = absoluteURL;

    const parsed = new URL(absoluteURL);
    window.gtag("event", "page_view", {
      page_title: document.title,
      page_location: absoluteURL,
      page_path: `${parsed.pathname}${parsed.search}`
    });
  }

  return {
    init() {
      const googleTagID = (document.body && document.body.dataset.googleTagId || "").trim();
      if (!configured && googleTagID && typeof window.gtag === "function") {
        window.gtag("config", googleTagID, { send_page_view: false });
        configured = true;
      }

      trackPageView(window.location.href);

      document.body.addEventListener("htmx:afterSettle", (event) => {
        if (!isBoostedHtmxNavigation(event.detail)) return;
        trackPageView(resolveHtmxNavigationURL(event.detail));
      });

      document.body.addEventListener("htmx:historyRestore", () => {
        trackPageView(window.location.href);
      });
    }
  };
}

document.addEventListener("DOMContentLoaded", () => {
  createNav().init();
  createAnalytics().init();
  initThemeToggle();
  initCSRFProtection();
  initDemoCharts();
  initHtmxHooks();
});
