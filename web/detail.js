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
  return `$${n.toLocaleString(undefined, { maximumFractionDigits: digits, minimumFractionDigits: digits })}`;
}

let latestSnapshot = {};

function escapeHTML(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

function renderMarkdown(md) {
  const escaped = escapeHTML(md || "Analysis pending or unavailable.");
  return escaped
    .replace(/^### (.*)$/gm, "<h3>$1</h3>")
    .replace(/\*\*(.*?)\*\*/g, "<strong>$1</strong>")
    .replace(/^\* (.*)$/gm, "<li>$1</li>")
    .replace(/(<li>.*<\/li>)/gs, "<ul>$1</ul>")
    .replace(/\n{2,}/g, "</p><p>")
    .replace(/\n/g, "<br>");
}

function linkifyPlainText(value) {
  const escaped = escapeHTML(value || "No article body available.");
  return escaped.replace(/https?:\/\/[^\s<>"']+/g, raw => {
    const href = raw.replace(/[.,);\]]+$/g, "");
    const trailing = raw.slice(href.length);
    return `<a href="${href}" target="_blank" rel="noreferrer">${href}</a>${trailing}`;
  });
}

function updateDetail(alert) {
  const snap = alert.snapshot || {};
  latestSnapshot = snap;
  document.querySelector("#detail-burst").textContent = alert.burst_status || "";
  document.querySelector("#detail-updated").textContent = alert.updated_at ? new Date(alert.updated_at).toLocaleString() : "";
  document.querySelector("#detail-ticker").textContent = alert.ticker || "";
  document.querySelector("#detail-news-status").textContent = alert.news_status || "";
  document.querySelector("#detail-last").textContent = `$${fmtNum(snap.last_price, 4)}`;
  const bid = Number(snap.bid) > 0 ? Number(snap.bid) : Number(snap.last_price || 0);
  const ask = Number(snap.ask) > 0 ? Number(snap.ask) : Number(snap.last_price || 0);
  document.querySelector("#detail-bid").textContent = bid > 0 ? `$${fmtNum(bid, 4)}` : "-";
  document.querySelector("#detail-ask").textContent = ask > 0 ? `$${fmtNum(ask, 4)}` : "-";
  const change = document.querySelector("#detail-change");
  change.textContent = `${fmtNum(snap.percent_change, 2)}%`;
  change.classList.toggle("positive", Number(snap.percent_change) >= 0);
  change.classList.toggle("negative", Number(snap.percent_change) < 0);
  document.querySelector("#detail-volume").textContent = fmtInt(snap.premarket_cumulative_volume);
  document.querySelector("#detail-hods").textContent = fmtInt(alert.hod_count || 0);

  const article = alert.article || {};
  document.querySelector("#detail-source").textContent = [article.source, article.created_at ? new Date(article.created_at).toLocaleString() : ""].filter(Boolean).join(" ");
  document.querySelector("#detail-headline").textContent = article.title || "No press release headline available";
  document.querySelector("#detail-article").innerHTML = linkifyPlainText(article.article_body);
  const link = document.querySelector("#detail-news-link");
  if (article.url) {
    link.innerHTML = `<a href="${escapeHTML(article.url)}" target="_blank" rel="noreferrer">Open press release</a>`;
  } else {
    link.textContent = "";
  }
  document.querySelector("#detail-analysis").innerHTML = renderMarkdown(alert.llm_markdown || alert.llm_error);
  updateSimOrderLabels();
}

function estimatedLimitPrice(side = "buy") {
  const isSell = side === "sell";
  const quote = isSell
    ? (Number(latestSnapshot.bid) > 0 ? Number(latestSnapshot.bid) : Number(latestSnapshot.last_price || 0))
    : (Number(latestSnapshot.ask) > 0 ? Number(latestSnapshot.ask) : Number(latestSnapshot.last_price || 0));
  if (quote <= 0) return 0;
  return isSell ? quote - 0.10 : quote + 0.10;
}

function maxTransactionValue(quantity, side = "buy") {
  const limit = estimatedLimitPrice(side);
  return limit > 0 && quantity > 0 ? limit * quantity : 0;
}

function simOrderLabel(side, quantity) {
  const value = maxTransactionValue(quantity, side);
  const valueText = value > 0 ? fmtMoney(value) : "-";
  return `${side === "sell" ? "Sell" : "Buy"} ${fmtInt(quantity)} · ${side === "sell" ? "min" : "max"} ${valueText}`;
}

function updateSimOrderLabels() {
  for (const button of document.querySelectorAll(".detail-sim-order[data-qty]")) {
    const side = button.dataset.side === "sell" ? "sell" : "buy";
    const quantity = Number(button.dataset.qty || 0);
    button.textContent = simOrderLabel(side, quantity);
    const limit = estimatedLimitPrice(side);
    button.title = limit > 0 ? `Limit price ${fmtMoney(limit, 4)} x ${fmtInt(quantity)} shares` : "";
  }
  const customQty = Math.trunc(Number(document.querySelector("#detail-custom-qty")?.value || 0));
  for (const customButton of document.querySelectorAll("#detail-buy-custom, #detail-sell-custom")) {
    const side = customButton.dataset.side === "sell" ? "sell" : "buy";
    const value = maxTransactionValue(customQty, side);
    customButton.textContent = `${side === "sell" ? "Sell" : "Buy"} custom · ${side === "sell" ? "min" : "max"} ${value > 0 ? fmtMoney(value) : "-"}`;
    const limit = estimatedLimitPrice(side);
    customButton.title = limit > 0 && customQty > 0 ? `Limit price ${fmtMoney(limit, 4)} x ${fmtInt(customQty)} shares` : "";
  }
}

async function submitSimOrder(alertID, side, quantity) {
  const log = document.querySelector("#detail-trade-log");
  if (!Number.isFinite(quantity) || quantity <= 0) {
    log.textContent = "Enter a positive share count.";
    return;
  }
  const orderSide = side === "sell" ? "sell" : "buy";
  log.textContent = `Simulating ${orderSide} ${quantity.toLocaleString()}...`;
  const res = await fetch("/api/sim/orders", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ alert_id: alertID, side: orderSide, quantity })
  });
  const raw = await res.text();
  let body = {};
  try {
    body = raw ? JSON.parse(raw) : {};
  } catch {
    body = { error: raw };
  }
  if (!res.ok) {
    log.textContent = body.error || raw || `Simulated ${orderSide} failed.`;
    return;
  }
  log.textContent = `${orderSide === "sell" ? "Sold" : "Bought"} ${fmtInt(body.quantity)} ${body.ticker} @ $${fmtNum(body.open_price, 4)} limit. Track it in Orders.`;
}

async function bootDetail() {
  const shell = document.querySelector(".detail-shell");
  const alertID = shell?.dataset.alertId;
  if (!alertID) return;
  document.querySelector("#detail-custom-qty")?.addEventListener("input", updateSimOrderLabels);
  for (const button of document.querySelectorAll(".detail-sim-order")) {
    button.addEventListener("click", () => {
      const fixedQty = Number(button.dataset.qty || 0);
      const quantity = fixedQty > 0 ? fixedQty : Number(document.querySelector("#detail-custom-qty")?.value || 0);
      submitSimOrder(alertID, button.dataset.side, Math.trunc(quantity));
    });
  }
  const initial = await fetch(`/api/alerts/${encodeURIComponent(alertID)}`).then(r => r.json());
  updateDetail(initial);
  const es = new EventSource("/api/events");
  es.addEventListener("alert", ev => {
    const alert = JSON.parse(ev.data);
    if (alert.id === alertID) updateDetail(alert);
  });
}

bootDetail().catch(err => {
  document.querySelector("#detail-news-status").textContent = err.message;
});
