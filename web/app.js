const alerts = new Map();
const root = document.querySelector("#alerts");
const tmpl = document.querySelector("#alert-template");
const queuedAudio = new Set();
const playedAudio = new Set();
const audioQueue = [];
const alertSignatures = new Map();
const flashUntil = new Map();
let gappers = [];
let chartBase = "";
let lastOrdersResponse = { orders: [], realized_pl: 0, unrealized_pl: 0, total_pl: 0 };
let soundEnabled = localStorage.getItem("soundEnabled") !== "false";
let newsOnlySound = localStorage.getItem("newsOnlySound") !== "false";
let gappersRefreshSeconds = 5;
let gappersSort = { key: "percent_change", dir: "desc" };
let alertHoverDepth = 0;
let gapperHoverDepth = 0;
let pendingAlertRender = false;
let pendingGapperRender = false;
let audioPlaying = false;
let audioBlocked = false;
let alertRenderDirty = false;
const unseenAlertIDs = new Set();

function isPanelActive(id) {
  return document.querySelector(`#${id}`)?.classList.contains("active");
}

function fmtTime(value) {
  if (!value) return "";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleTimeString();
}

function fmtNum(value, digits = 2) {
  if (value === undefined || value === null || Number.isNaN(Number(value))) return "-";
  return Number(value).toLocaleString(undefined, { maximumFractionDigits: digits, minimumFractionDigits: digits });
}

function fmtInt(value) {
  if (value === undefined || value === null) return "-";
  return Number(value).toLocaleString();
}

function fmtMoney(value, digits = 2) {
  if (value === undefined || value === null || Number.isNaN(Number(value))) return "-";
  const n = Number(value);
  const formatted = Math.abs(n).toLocaleString(undefined, { maximumFractionDigits: digits, minimumFractionDigits: digits });
  return `${n < 0 ? "-" : ""}$${formatted}`;
}

function plClass(value) {
  const n = Number(value || 0);
  if (n > 0) return "pl-positive";
  if (n < 0) return "pl-negative";
  return "";
}

function chartURL(ticker) {
  return chartBase ? `${chartBase}/api/open-chart/${ticker}/${new Date().toISOString().slice(0, 10)}` : "#";
}

function text(value) {
  return value === undefined || value === null || value === "" ? "-" : String(value);
}

function renderMarkdown(md) {
  const escaped = text(md)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
  return escaped
    .replace(/^### (.*)$/gm, "<h3>$1</h3>")
    .replace(/\*\*(.*?)\*\*/g, "<strong>$1</strong>")
    .replace(/^\* (.*)$/gm, "<p>&bull; $1</p>")
    .replace(/\n{2,}/g, "</p><p>")
    .replace(/\n/g, "<br>");
}

function alertSignature(alert) {
  const snap = alert.snapshot || {};
  return [
    snap.last_price,
    snap.percent_change,
    snap.premarket_cumulative_volume,
    alert.hod_count,
    alert.news_status,
    alert.llm_status,
    alert.tts_status,
    alert.risk_status,
    alert.article?.title || "",
    alert.llm_markdown || "",
  ].join("|");
}

function newsFreshnessClass(status) {
  switch (String(status || "").toLowerCase()) {
    case "fresh":
      return "news-fresh";
    case "recent":
      return "news-recent";
    case "old":
      return "news-old";
    default:
      return "";
  }
}

function alertDisplayScore(alert) {
  const articleFreshness = alert.article?.freshness || "";
  const freshness = String(alert.news_status || articleFreshness).toLowerCase();
  let score = 0;
  if (alert.article?.title) score += 100;
  if (freshness === "fresh") score += 40;
  if (freshness === "recent") score += 30;
  if (freshness === "old") score += 10;
  if (alert.llm_markdown) score += 25;
  if (alert.tts_status === "complete" && alert.audio_path) score += 15;
  if (alert.burst_status === "full") score += 5;
  if (!alert.cooldown_active) score += 1;
  return score;
}

function preferredTickerAlert(current, candidate) {
  if (!current) return candidate;
  const currentScore = alertDisplayScore(current);
  const candidateScore = alertDisplayScore(candidate);
  if (candidateScore !== currentScore) {
    return candidateScore > currentScore ? candidate : current;
  }
  return new Date(candidate.updated_at) > new Date(current.updated_at) ? candidate : current;
}

function upsert(alert) {
  const previous = alerts.get(alert.id);
  const becameAudible = alert.audio_path && previous && previous.audio_path !== alert.audio_path;
  const gainedNews = alertHasNews(alert) && (!previous || !alertHasNews(previous));
  const gainedAnalysis = Boolean(alert.llm_markdown) && (!previous || previous.llm_markdown !== alert.llm_markdown);
  const nextSignature = alertSignature(alert);
  if (previous && alertSignatures.get(alert.id) !== nextSignature) {
    flashUntil.set(alert.id, Date.now() + 1100);
  }
  alertSignatures.set(alert.id, nextSignature);
  alerts.set(alert.id, alert);
  if ((!previous || becameAudible) && !isPanelActive("alerts-panel")) {
    unseenAlertIDs.add(alert.id);
    updateAlertTabLabel();
  }
  markAlertRenderDirty();
  if (!previous || becameAudible || gainedNews || gainedAnalysis) {
    render({ force: true });
  }
  maybePlayAlertAudio(alert);
}

function updateAlertTabLabel() {
  const tab = document.querySelector("#alerts-tab");
  const count = unseenAlertIDs.size;
  tab.textContent = count > 0 ? `Alerts (${count})` : "Alerts";
}

function updateSoundToggle() {
  const btn = document.querySelector("#sound-toggle");
  if (!soundEnabled) {
    btn.textContent = "Sound off";
  } else if (audioBlocked) {
    btn.textContent = `Sound blocked (${audioQueue.length})`;
  } else if (audioPlaying) {
    btn.textContent = "Sound playing";
  } else if (audioQueue.length > 0) {
    btn.textContent = `Sound queued (${audioQueue.length})`;
  } else {
    btn.textContent = "Sound on";
  }
  btn.classList.toggle("off", !soundEnabled);
  btn.classList.toggle("blocked", audioBlocked);
  btn.setAttribute("aria-pressed", soundEnabled ? "true" : "false");
}

function updateNewsAudioToggle() {
  const input = document.querySelector("#news-audio-only");
  input.checked = newsOnlySound;
}

function alertHasNews(alert) {
  return Boolean(alert.article && alert.article.title);
}

function filterAudioQueueForNewsOnly() {
  if (!newsOnlySound) return;
  for (let i = audioQueue.length - 1; i >= 0; i -= 1) {
    if (!audioQueue[i].hasNews) {
      queuedAudio.delete(audioQueue[i].key);
      audioQueue.splice(i, 1);
    }
  }
  updateSoundToggle();
}

function maybePlayAlertAudio(alert) {
  if (!soundEnabled || !alert.audio_path || alert.tts_status !== "complete") return;
  const hasNews = alertHasNews(alert);
  if (newsOnlySound && !hasNews) return;
  const key = `${alert.id}:${alert.audio_path}`;
  if (playedAudio.has(key) || queuedAudio.has(key)) return;
  queuedAudio.add(key);
  audioQueue.push({ key, url: `/api/alerts/${alert.id}/audio`, ticker: alert.ticker, hasNews });
  drainAudioQueue();
}

function drainAudioQueue() {
  if (!soundEnabled || audioPlaying || audioQueue.length === 0) {
    updateSoundToggle();
    return;
  }
  const item = audioQueue[0];
  const audio = new Audio(item.url);
  audioPlaying = true;
  audioBlocked = false;
  document.querySelector("#health").textContent = `playing ${item.ticker}`;
  updateSoundToggle();

  const finish = () => {
    audioQueue.shift();
    queuedAudio.delete(item.key);
    playedAudio.add(item.key);
    audioPlaying = false;
    if (audioQueue.length === 0) {
      document.querySelector("#health").textContent = "sound ready";
    }
    updateSoundToggle();
    drainAudioQueue();
  };

  audio.addEventListener("ended", finish, { once: true });
  audio.addEventListener("error", () => {
    document.querySelector("#health").textContent = `sound error ${item.ticker}`;
    finish();
  }, { once: true });
  audio.play().catch(() => {
    audioPlaying = false;
    audioBlocked = true;
    document.querySelector("#health").textContent = "sound blocked; click Sound";
    updateSoundToggle();
  });
}

function render(options = {}) {
  if (!options.force && isPanelActive("alerts-panel") && alertHoverDepth > 0) {
    pendingAlertRender = true;
    return;
  }
  pendingAlertRender = false;
  alertRenderDirty = false;
  const latestByTicker = new Map();
  for (const alert of alerts.values()) {
    const ticker = alert.ticker || alert.id;
    latestByTicker.set(ticker, preferredTickerAlert(latestByTicker.get(ticker), alert));
  }
  const list = [...latestByTicker.values()].sort((a, b) => {
    const aChange = Number(a.snapshot?.percent_change || 0);
    const bChange = Number(b.snapshot?.percent_change || 0);
    if (aChange !== bChange) return bChange - aChange;
    return String(a.ticker || "").localeCompare(String(b.ticker || ""));
  });
  root.textContent = "";
  for (const alert of list) {
    const node = tmpl.content.firstElementChild.cloneNode(true);
    if ((flashUntil.get(alert.id) || 0) > Date.now()) {
      node.classList.add("updated-flash");
    }
    const article = alert.article || {};
    const freshnessClass = newsFreshnessClass(alert.news_status) || newsFreshnessClass(article.freshness);
    if (freshnessClass) node.classList.add(freshnessClass);
    node.addEventListener("mouseenter", () => {
      alertHoverDepth += 1;
    });
    node.addEventListener("mouseleave", () => {
      alertHoverDepth = Math.max(0, alertHoverDepth - 1);
      if (alertHoverDepth === 0 && pendingAlertRender) markAlertRenderDirty();
    });
    const snap = alert.snapshot || {};
    const ticker = node.querySelector(".ticker");
    ticker.textContent = alert.ticker;
    ticker.href = chartURL(alert.ticker);
    node.querySelector("time").textContent = fmtTime(alert.updated_at);
    const burst = node.querySelector(".burst");
    burst.textContent = alert.cooldown_active ? "cooldown" : alert.burst_status;
    burst.classList.add(alert.burst_status || "soft");
    node.querySelector(".price").textContent = `$${fmtNum(snap.last_price, 4)}`;
    const change = node.querySelector(".change");
    change.textContent = `${fmtNum(snap.percent_change, 2)}%`;
    change.classList.toggle("positive", Number(snap.percent_change) >= 0);
    change.classList.toggle("negative", Number(snap.percent_change) < 0);
    node.querySelector(".volume").textContent = fmtInt(snap.premarket_cumulative_volume);
    node.querySelector(".hods").textContent = `${alert.hod_count || 0}`;
    const headline = article.title || (alert.news_error ? `News error: ${alert.news_error}` : "No fresh RTPR press release found.");
    node.querySelector(".news").textContent = `${text(alert.news_status)}: ${headline}`;
    const newsLink = node.querySelector(".news-link");
    if (article.url) {
      newsLink.href = article.url;
      newsLink.textContent = `Open press release${article.source ? ` from ${article.source}` : ""}`;
      newsLink.hidden = false;
    } else {
      newsLink.hidden = true;
    }
    node.querySelector(".states").textContent = `LLM ${text(alert.llm_status)} · TTS ${text(alert.tts_status)} · Risk ${text(alert.risk_status)} · ${text(alert.broker_mode)}`;
    node.querySelector(".article-body").textContent = article.article_body || "No article body available.";
    const analysisLink = node.querySelector(".analysis-link");
    if (alert.llm_markdown) {
      analysisLink.href = `/alerts/${encodeURIComponent(alert.id)}`;
      analysisLink.hidden = false;
    } else {
      analysisLink.hidden = true;
    }
    node.querySelector(".analysis").innerHTML = renderMarkdown(alert.llm_markdown || alert.llm_error || "Pending or skipped.");
    node.querySelector(".audio").disabled = !alert.audio_path;
    node.querySelector(".audio").addEventListener("click", () => new Audio(`/api/alerts/${alert.id}/audio`).play());
    for (const btn of node.querySelectorAll("[data-button]")) {
      btn.addEventListener("click", async () => submitTrade(alert.id, btn.dataset.button, node));
    }
    const log = node.querySelector(".trade-log");
    const last = (alert.trade_results || []).at(-1);
    if (last) {
      const allowed = last.risk && last.risk.allowed ? "allowed" : "blocked";
      const broker = last.broker && last.broker.reason ? last.broker.reason : (last.broker && last.broker.accepted ? "accepted" : "not sent");
      log.textContent = `Last intent: ${allowed}; broker ${broker}`;
    }
    root.appendChild(node);
  }
}

function markAlertRenderDirty() {
  alertRenderDirty = true;
}

function flushAlertRenderIfDirty() {
  if (alertRenderDirty) render();
}

function setupTabs() {
  for (const btn of document.querySelectorAll(".tab")) {
    btn.addEventListener("click", () => {
      for (const tab of document.querySelectorAll(".tab")) tab.classList.toggle("active", tab === btn);
      for (const panel of document.querySelectorAll(".panel")) panel.classList.toggle("active", panel.id === btn.dataset.tab);
      if (btn.dataset.tab === "alerts-panel") {
        alertHoverDepth = 0;
        unseenAlertIDs.clear();
        updateAlertTabLabel();
        markAlertRenderDirty();
        flushAlertRenderIfDirty();
      }
      if (btn.dataset.tab === "gappers-panel") {
        gapperHoverDepth = 0;
        renderGappers();
      }
      if (btn.dataset.tab === "orders-panel") {
        renderOrders(lastOrdersResponse);
        refreshOrders();
      }
    });
  }
}

function updateNYClock() {
  const clock = document.querySelector("#ny-clock");
  clock.textContent = new Intl.DateTimeFormat(undefined, {
    timeZone: "America/New_York",
    weekday: "short",
    year: "numeric",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
    timeZoneName: "short"
  }).format(new Date());
}

function sortedGappers() {
  const key = gappersSort.key;
  const dir = gappersSort.dir === "asc" ? 1 : -1;
  return [...gappers].sort((a, b) => {
    const av = a[key] ?? "";
    const bv = b[key] ?? "";
    if (typeof av === "number" && typeof bv === "number") return (av - bv) * dir;
    return String(av).localeCompare(String(bv)) * dir;
  });
}

function renderGappers() {
  if (isPanelActive("gappers-panel") && gapperHoverDepth > 0) {
    pendingGapperRender = true;
    return;
  }
  pendingGapperRender = false;
  const body = document.querySelector("#gappers-body");
  body.textContent = "";
  for (const row of sortedGappers()) {
    const tr = document.createElement("tr");
    const freshnessClass = newsFreshnessClass(row.news_status);
    if (freshnessClass) {
      tr.classList.add(freshnessClass);
      if (row.news_headline) tr.title = row.news_headline;
    }
    tr.addEventListener("mouseenter", () => {
      gapperHoverDepth += 1;
    });
    tr.addEventListener("mouseleave", () => {
      gapperHoverDepth = Math.max(0, gapperHoverDepth - 1);
      if (gapperHoverDepth === 0 && pendingGapperRender) renderGappers();
    });
    const ticker = document.createElement("td");
    const link = document.createElement("a");
    link.href = chartURL(row.ticker);
    link.target = "_blank";
    link.rel = "noreferrer";
    link.textContent = row.ticker;
    ticker.appendChild(link);
    tr.appendChild(ticker);
    tr.appendChild(td(`$${fmtNum(row.last_price, 4)}`, "num"));
    tr.appendChild(td(`${fmtNum(row.percent_change, 2)}%`, "num"));
    tr.appendChild(td(fmtInt(row.premarket_volume), "num"));
    tr.appendChild(td(row.company_name || ""));
    body.appendChild(tr);
  }
}

function td(value, className = "") {
  const cell = document.createElement("td");
  cell.textContent = value;
  if (className) cell.className = className;
  return cell;
}

async function refreshGappers() {
  gappers = await fetch("/api/gappers").then(r => r.json());
  renderGappers();
}

function tdHTML(html, className = "") {
  const cell = document.createElement("td");
  cell.innerHTML = html;
  if (className) cell.className = className;
  return cell;
}

function setPL(id, value) {
  const node = document.querySelector(id);
  node.textContent = fmtMoney(value);
  node.classList.toggle("pl-positive", Number(value || 0) > 0);
  node.classList.toggle("pl-negative", Number(value || 0) < 0);
}

function renderOrders(resp = lastOrdersResponse) {
  lastOrdersResponse = resp || { orders: [], realized_pl: 0, unrealized_pl: 0, total_pl: 0 };
  setPL("#orders-realized", lastOrdersResponse.realized_pl);
  setPL("#orders-unrealized", lastOrdersResponse.unrealized_pl);
  setPL("#orders-total", lastOrdersResponse.total_pl);
  const body = document.querySelector("#orders-body");
  if (!body) return;
  body.textContent = "";
  for (const order of lastOrdersResponse.orders || []) {
    const tr = document.createElement("tr");
    const ticker = document.createElement("td");
    const link = document.createElement("a");
    link.href = chartURL(order.ticker);
    link.target = "_blank";
    link.rel = "noreferrer";
    link.textContent = order.ticker;
    ticker.appendChild(link);
    tr.appendChild(ticker);
    tr.appendChild(td((order.side || "buy").toUpperCase()));
    tr.appendChild(td(fmtInt(order.quantity), "num"));
    tr.appendChild(td(fmtMoney(order.open_price, 4), "num"));
    tr.appendChild(td(order.last_price > 0 ? fmtMoney(order.last_price, 4) : "-", "num"));
    tr.appendChild(td(order.exit_estimate > 0 ? fmtMoney(order.exit_estimate, 4) : "-", "num"));
    tr.appendChild(td(fmtMoney(order.unrealized_pl), `num ${plClass(order.unrealized_pl)}`));
    tr.appendChild(td(fmtMoney(order.realized_pl), `num ${plClass(order.realized_pl)}`));
    tr.appendChild(td(order.status || "-"));
    const action = document.createElement("td");
    if (order.status === "open") {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "close-position";
      btn.textContent = "Close position";
      btn.addEventListener("click", () => closeSimOrder(order.id, btn));
      action.appendChild(btn);
    } else {
      action.textContent = "Closed";
    }
    tr.appendChild(action);
    body.appendChild(tr);
  }
  if ((lastOrdersResponse.orders || []).length === 0) {
    const tr = document.createElement("tr");
    tr.appendChild(tdHTML("No paper trades yet.", "empty-orders"));
    tr.firstElementChild.colSpan = 10;
    body.appendChild(tr);
  }
}

async function refreshOrders() {
  const resp = await fetch("/api/sim/orders").then(r => r.json());
  renderOrders(resp);
}

async function closeSimOrder(id, button) {
  button.disabled = true;
  button.textContent = "Closing...";
  const res = await fetch(`/api/sim/orders/${encodeURIComponent(id)}/close`, { method: "POST" });
  if (!res.ok) {
    button.disabled = false;
    button.textContent = "Close failed";
    return;
  }
  await refreshOrders();
}

function setupGappersSorting() {
  for (const btn of document.querySelectorAll("[data-sort]")) {
    btn.addEventListener("click", () => {
      const key = btn.dataset.sort;
      if (gappersSort.key === key) {
        gappersSort.dir = gappersSort.dir === "asc" ? "desc" : "asc";
      } else {
        gappersSort = { key, dir: key === "ticker" || key === "company_name" ? "asc" : "desc" };
      }
      renderGappers();
    });
  }
}

async function submitTrade(alertID, buttonID, node) {
  const key = `${alertID}:${buttonID}`;
  const res = await fetch(`/api/alerts/${alertID}/trade-intent`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ button_id: buttonID, idempotency_key: key })
  });
  const body = await res.json().catch(() => ({}));
  node.querySelector(".trade-log").textContent = res.ok ? JSON.stringify(body.record.risk.reasons || body.record.broker.reason || "accepted") : JSON.stringify(body);
}

async function boot() {
  setupTabs();
  setupGappersSorting();
  updateSoundToggle();
  updateNewsAudioToggle();
  document.querySelector("#news-audio-only").addEventListener("change", ev => {
    newsOnlySound = ev.target.checked;
    localStorage.setItem("newsOnlySound", newsOnlySound ? "true" : "false");
    filterAudioQueueForNewsOnly();
    if (soundEnabled) {
      for (const alert of alerts.values()) maybePlayAlertAudio(alert);
      drainAudioQueue();
    }
  });
  document.querySelector("#sound-toggle").addEventListener("click", () => {
    soundEnabled = !soundEnabled;
    localStorage.setItem("soundEnabled", soundEnabled ? "true" : "false");
    updateSoundToggle();
    if (soundEnabled) {
      for (const alert of alerts.values()) maybePlayAlertAudio(alert);
      drainAudioQueue();
    }
  });
  const health = await fetch("/api/health").then(r => r.json());
  chartBase = health.chart_opener_base_url || "";
  gappersRefreshSeconds = Math.max(1, Number(health.gappers_refresh_seconds || 5));
  document.querySelector("#mode").textContent = health.dummy_mode ? "DUMMY MODE" : "LIVE MODE";
  document.querySelector("#health").textContent = health.ok ? "healthy" : "degraded";
  updateNYClock();
  setInterval(updateNYClock, 1000);
  setInterval(flushAlertRenderIfDirty, 1000);
  await refreshGappers();
  setInterval(refreshGappers, gappersRefreshSeconds * 1000);
  await refreshOrders();
  setInterval(refreshOrders, 1000);
  const initial = await fetch("/api/alerts").then(r => r.json());
  for (const alert of initial) upsert(alert);
  const es = new EventSource("/api/events");
  es.addEventListener("alert", ev => {
    upsert(JSON.parse(ev.data));
    document.querySelector("#updated").textContent = `updated ${new Date().toLocaleTimeString()}`;
  });
  es.onerror = () => {
    document.querySelector("#health").textContent = "stream reconnecting";
  };
}

boot().catch(err => {
  document.querySelector("#health").textContent = err.message;
});
