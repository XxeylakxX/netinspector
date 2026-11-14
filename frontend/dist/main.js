"use strict";

// ---- Wails bridge (injected at runtime by the Wails webview) ----------
const go = () => window.go.main.App;
const rt = () => window.runtime;

const $ = (id) => document.getElementById(id);
const esc = (s) =>
  String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));

const ICON_ETH =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4M7 8v4M11 8v4M15 8v4"/></svg>';
const ICON_WIFI =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M2 8.8a15 15 0 0 1 20 0M5 12.5a10 10 0 0 1 14 0M8.5 16a5 5 0 0 1 7 0"/><circle cx="12" cy="19.5" r="1" fill="currentColor"/></svg>';

const state = {
  adapters: [],
  gateways: [],
  target: "8.8.8.8",
  count: 4,
  running: false,
  samples: [], // numbers (ms) or null for loss
  sent: 0,
  recv: 0,
  routerLocations: [],
  editingLocation: null,
  activeRouterUrl: "http://192.168.1.1/html/login_pldt.html",
  activeRouterUser: "admin",
  activeRouterPass: "",
  dnsPresets: [],
  selectedPresetId: null,
  activeTestTab: "ping",
  speedtestRunning: false,
  speedtestEngine: "fast",
  speedtestServers: [],
  fastServers: [],
  ooklaServers: [],
  signalStats: {},
};

// ------------------------------------------------------------- toasts
function toast(msg, err = false) {
  const el = document.createElement("div");
  el.className = "toast" + (err ? " err" : "");
  el.textContent = msg;
  $("toasts").appendChild(el);
  setTimeout(() => {
    el.classList.add("out");
    setTimeout(() => el.remove(), 300);
  }, 2200);
}

async function copy(text, label = "Copied") {
  try {
    await go().CopyText(text);
    toast(label);
  } catch (e) {
    toast("Copy failed", true);
  }
}

// ----------------------------------------------------------- adapters
function getSignalColor(pct) {
  const p = parseInt(pct, 10);
  if (isNaN(p)) return "var(--muted)";
  const clamped = Math.max(0, Math.min(100, p));
  // 100% -> hue 125 (vibrant green), 60% -> hue 60 (yellow), 0% -> hue 0 (red)
  const hue = Math.round((clamped / 100) * 125);
  return `hsl(${hue}, 88%, 52%)`;
}

function signalBars(sig, pctNum) {
  const pct = !isNaN(pctNum) ? pctNum : parseInt(sig, 10);
  const on = isNaN(pct) ? 0 : pct >= 80 ? 4 : pct >= 60 ? 3 : pct >= 35 ? 2 : pct > 0 ? 1 : 0;
  const col = getSignalColor(pct);
  return `<div class="signal" title="${esc(sig)}">${[1, 2, 3, 4]
    .map(
      (i) =>
        `<i class="${i <= on ? "on" : ""}" style="${
          i <= on ? `background:${col};box-shadow:0 0 6px ${col}88;` : ""
        }"></i>`
    )
    .join("")}</div>`;
}

function updateSignalStats(alias, wifi) {
  if (!wifi || !wifi.signal) return null;
  const pct = parseInt(wifi.signal, 10);
  if (isNaN(pct)) return null;

  let stats = state.signalStats[alias];
  if (!stats) {
    stats = {
      minPct: pct,
      maxPct: pct,
      minDbm: wifi.signalDbm || "",
      maxDbm: wifi.signalDbm || ""
    };
    state.signalStats[alias] = stats;
  } else {
    if (pct < stats.minPct) {
      stats.minPct = pct;
      stats.minDbm = wifi.signalDbm || stats.minDbm;
    }
    if (pct > stats.maxPct) {
      stats.maxPct = pct;
      stats.maxDbm = wifi.signalDbm || stats.maxDbm;
    }
  }
  return stats;
}

function valChips(list, cls) {
  return list.map((v) => `<span class="val ${cls}" data-copy="${esc(v)}" title="Click to copy">${esc(v)}</span>`).join("");
}

function row(label, html) {
  return html ? `<dt>${label}</dt><dd>${html}</dd>` : "";
}

function adapterCard(a, i) {
  const wifi = a.isWifi && a.wifi;
  const rows =
    row("IPv4", valChips(a.ipv4, "v4")) +
    row("IPv6", valChips(a.ipv6, "v6")) +
    row("Gateway", valChips(a.gateway, "gw")) +
    row("DNS", valChips(a.dns, "")) +
    row("MAC", a.mac ? valChips([a.mac], "") : "") +
    row("Link speed", a.linkSpeed ? `<span class="val" data-copy="${esc(a.linkSpeed)}">${esc(a.linkSpeed)}</span>` : "");

  let wifiSigCell = "";
  if (wifi) {
    const stats = updateSignalStats(a.alias, wifi);
    const pct = parseInt(wifi.signal, 10);
    const col = getSignalColor(pct);
    const loCol = stats ? getSignalColor(stats.minPct) : col;
    const hiCol = stats ? getSignalColor(stats.maxPct) : col;

    wifiSigCell = `
      <div class="wifi-sig-cell">
        <div class="wifi-sig-head">
          <span>Signal</span>
          ${
            stats
              ? `<span class="sig-extremes" title="Lowest: ${stats.minPct}%${stats.minDbm ? ` (${stats.minDbm})` : ""} · Highest: ${stats.maxPct}%${stats.maxDbm ? ` (${stats.maxDbm})` : ""}">
                   <span class="sig-lo">L: <b style="color:${loCol}">${stats.minPct}%</b></span>
                   <span class="sig-sep">·</span>
                   <span class="sig-hi">H: <b style="color:${hiCol}">${stats.maxPct}%</b></span>
                 </span>`
              : ""
          }
        </div>
        <b class="wifi-sig" style="display:flex;gap:8px;align-items:center;justify-content:space-between">
          <span>
            <span class="sig-val" style="color:${col}">${esc(wifi.signal || "–")}</span>
            ${wifi.signalDbm ? ` <span class="sig-dbm">(${esc(wifi.signalDbm)})</span>` : ""}
          </span>
          ${signalBars(wifi.signal, pct)}
        </b>
        <div class="sig-meter" title="Signal: ${isNaN(pct) ? 0 : pct}% (Low: ${stats ? stats.minPct : 0}%, High: ${stats ? stats.maxPct : 0}%)">
          <div class="sig-meter-track"></div>
          <div class="sig-meter-fill" style="width:${isNaN(pct) ? 0 : pct}%;"></div>
          ${
            stats
              ? `<div class="sig-marker low" style="left:${stats.minPct}%;" title="Lowest: ${stats.minPct}%"></div>
                 <div class="sig-marker high" style="left:${stats.maxPct}%;" title="Highest: ${stats.maxPct}%"></div>`
              : ""
          }
          <div class="sig-marker cur" style="left:${isNaN(pct) ? 0 : pct}%; background:${col};" title="Current: ${isNaN(pct) ? 0 : pct}%"></div>
        </div>
      </div>`;
  }

  const wifiBox = wifi
    ? `<div class="wifi-box">
         <div class="wifi-head">
           <b class="wifi-ssid">${esc(wifi.ssid || "Wi-Fi")}</b>
           <span class="gen wifi-gen">${esc(wifi.standard)}</span>
         </div>
         <div class="wifi-grid">
           <div><span>Frequency</span><b class="wifi-band">${esc(wifi.band || "–")}</b></div>
           <div><span>Channel</span><b class="wifi-channel">${esc(wifi.channel || "–")}</b></div>
           <div><span>Channel Width</span><b class="wifi-width">${esc(wifi.channelWidth || "–")}</b></div>
           <div><span>Receive</span><b class="wifi-rx">${esc(wifi.rxRate || "–")} Mbps</b></div>
           <div><span>Transmit</span><b class="wifi-tx">${esc(wifi.txRate || "–")} Mbps</b></div>
           ${wifiSigCell}
           <div class="wifi-span-all"><span>Security</span><b class="wifi-auth">${esc(wifi.auth || "–")}</b></div>
         </div>
       </div>`
    : "";

  return `<article class="card" data-alias="${esc(a.alias)}" style="animation-delay:${i * 70}ms">
    <div class="card-top">
      <div class="ico ${a.isWifi ? "wifi" : ""}">${a.isWifi ? ICON_WIFI : ICON_ETH}</div>
      <div class="card-title"><b>${esc(a.alias)}</b><span title="${esc(a.desc)}">${esc(a.desc)}</span></div>
      <span class="badge status-badge">${esc(a.status || "Up")}</span>
    </div>
    <dl class="rows" data-rows="${esc(a.alias)}">${rows}</dl>
    ${wifiBox}
    <div class="adapter-actions">
      <button class="btn-xs speed-act" data-action="speedtest" data-alias="${esc(a.alias)}" title="Run Speedtest on ${esc(a.alias)}">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
        Speedtest
      </button>
      <button class="btn-xs dns-act" data-action="dns" data-alias="${esc(a.alias)}" title="Configure DNS for ${esc(a.alias)}">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M2 12h20"/></svg>
        DNS
      </button>
      <button class="btn-xs" data-action="renew" data-alias="${esc(a.alias)}" title="Renew DHCP IP for ${esc(a.alias)}">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 12a9 9 0 1 1-3-6.7"/><path d="M21 3v6h-6"/></svg>
        Renew
      </button>
      <button class="btn-xs" data-action="release" data-alias="${esc(a.alias)}" title="Release DHCP IP for ${esc(a.alias)}">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18.36 6.64a9 9 0 1 1-12.73 0"/><line x1="12" y1="2" x2="12" y2="12"/></svg>
        Release
      </button>
    </div>
  </article>`;
}

let autoRefreshInterval = 3000;
let autoRefreshTimer = null;
let autoRefreshPaused = false;

function setAutoRefresh(interval) {
  autoRefreshInterval = interval;
  if (autoRefreshTimer) {
    clearInterval(autoRefreshTimer);
    autoRefreshTimer = null;
  }
  const btn = $("btnAutoRefresh");
  const lbl = $("autoRefreshLabel");
  if (!btn || !lbl) return;

  if (interval === 0) {
    btn.className = "auto-btn off";
    btn.title = "Live Refresh: OFF (Click to switch to 3s)";
    lbl.textContent = "Live: OFF";
  } else {
    btn.className = "auto-btn on";
    btn.title = `Live Refresh: ${interval / 1000}s (Click to cycle 5s, Off)`;
    lbl.textContent = `Live: ${interval / 1000}s`;
    startAutoRefreshTimer();
  }
}

function cycleAutoRefresh() {
  if (autoRefreshInterval === 3000) {
    setAutoRefresh(5000);
  } else if (autoRefreshInterval === 5000) {
    setAutoRefresh(0);
  } else {
    setAutoRefresh(3000);
  }
}

function startAutoRefreshTimer() {
  if (autoRefreshTimer) clearInterval(autoRefreshTimer);
  if (autoRefreshInterval <= 0) return;

  autoRefreshTimer = setInterval(async () => {
    if (document.hidden || autoRefreshPaused || state.speedtestRunning) return;
    try {
      await refresh(true);
    } catch (_) {}
  }, autoRefreshInterval);
}

async function refresh(isLive = false) {
  const btn = $("btnRefresh");
  if (!isLive) {
    btn.classList.add("spin");
    $("subtitle").textContent = "Scanning your interfaces…";
    $("adapters").innerHTML = '<div class="skeleton"></div><div class="skeleton"></div>';
  }
  try {
    const list = (await go().GetAdapters()) || [];
    const prevAdapters = state.adapters || [];
    state.adapters = list;
    state.gateways = [...new Set(list.flatMap((a) => a.gateway))];
    populateSpeedtestAdapters($("stAdapterSelect") ? $("stAdapterSelect").value : null);
    $("adapterCount").textContent = list.length;
    $("subtitle").textContent = list.length
      ? `${list.length} active adapter${list.length > 1 ? "s" : ""} · ${isLive ? "live " : ""}updated ${new Date().toLocaleTimeString()}`
      : "No active adapters";

    if (isLive && prevAdapters.length === list.length && list.length > 0) {
      let canPatch = true;
      for (const a of list) {
        if (!document.querySelector(`.card[data-alias="${CSS.escape(a.alias)}"]`)) {
          canPatch = false;
          break;
        }
      }
      if (canPatch) {
        // In-place smooth DOM patch
        for (const a of list) {
          const card = document.querySelector(`.card[data-alias="${CSS.escape(a.alias)}"]`);
          if (!card) continue;
          const badge = card.querySelector(".status-badge");
          if (badge && badge.textContent !== (a.status || "Up")) badge.textContent = a.status || "Up";

          const rowsEl = card.querySelector(".rows");
          const newRows =
            row("IPv4", valChips(a.ipv4, "v4")) +
            row("IPv6", valChips(a.ipv6, "v6")) +
            row("Gateway", valChips(a.gateway, "gw")) +
            row("DNS", valChips(a.dns, "")) +
            row("MAC", a.mac ? valChips([a.mac], "") : "") +
            row("Link speed", a.linkSpeed ? `<span class="val" data-copy="${esc(a.linkSpeed)}">${esc(a.linkSpeed)}</span>` : "");
          if (rowsEl && rowsEl.innerHTML !== newRows) {
            rowsEl.innerHTML = newRows;
          }

          if (a.isWifi && a.wifi) {
            const w = a.wifi;
            const stats = updateSignalStats(a.alias, w);
            const pct = parseInt(w.signal, 10);
            const col = getSignalColor(pct);

            const ssidEl = card.querySelector(".wifi-ssid");
            if (ssidEl && ssidEl.textContent !== (w.ssid || "Wi-Fi")) ssidEl.textContent = w.ssid || "Wi-Fi";
            const bandEl = card.querySelector(".wifi-band");
            if (bandEl && bandEl.textContent !== (w.band || "–")) bandEl.textContent = w.band || "–";
            const chanEl = card.querySelector(".wifi-channel");
            if (chanEl && chanEl.textContent !== (w.channel || "–")) chanEl.textContent = w.channel || "–";
            const widthEl = card.querySelector(".wifi-width");
            if (widthEl && widthEl.textContent !== (w.channelWidth || "–")) widthEl.textContent = w.channelWidth || "–";
            const rxEl = card.querySelector(".wifi-rx");
            if (rxEl) rxEl.textContent = `${w.rxRate || "–"} Mbps`;
            const txEl = card.querySelector(".wifi-tx");
            if (txEl) txEl.textContent = `${w.txRate || "–"} Mbps`;

            // Signal value and color
            const sigValEl = card.querySelector(".sig-val");
            if (sigValEl) {
              sigValEl.textContent = w.signal || "–";
              sigValEl.style.color = col;
            }
            const sigDbmEl = card.querySelector(".sig-dbm");
            if (sigDbmEl) {
              sigDbmEl.textContent = w.signalDbm ? `(${w.signalDbm})` : "";
            }
            const sigBarsEl = card.querySelector(".signal");
            if (sigBarsEl) {
              sigBarsEl.outerHTML = signalBars(w.signal, pct);
            }

            // Low / High and meter update
            if (stats) {
              const loCol = getSignalColor(stats.minPct);
              const hiCol = getSignalColor(stats.maxPct);
              const loEl = card.querySelector(".sig-lo b");
              if (loEl) {
                loEl.textContent = `${stats.minPct}%`;
                loEl.style.color = loCol;
              }
              const hiEl = card.querySelector(".sig-hi b");
              if (hiEl) {
                hiEl.textContent = `${stats.maxPct}%`;
                hiEl.style.color = hiCol;
              }
              const extremesEl = card.querySelector(".sig-extremes");
              if (extremesEl) {
                extremesEl.title = `Lowest: ${stats.minPct}%${stats.minDbm ? ` (${stats.minDbm})` : ""} · Highest: ${stats.maxPct}%${stats.maxDbm ? ` (${stats.maxDbm})` : ""}`;
              }

              const fillEl = card.querySelector(".sig-meter-fill");
              if (fillEl) fillEl.style.width = `${isNaN(pct) ? 0 : pct}%`;

              const curMarker = card.querySelector(".sig-marker.cur");
              if (curMarker) {
                curMarker.style.left = `${isNaN(pct) ? 0 : pct}%`;
                curMarker.style.background = col;
                curMarker.title = `Current: ${isNaN(pct) ? 0 : pct}%`;
              }
              const lowMarker = card.querySelector(".sig-marker.low");
              if (lowMarker) {
                lowMarker.style.left = `${stats.minPct}%`;
                lowMarker.title = `Lowest: ${stats.minPct}%`;
              }
              const highMarker = card.querySelector(".sig-marker.high");
              if (highMarker) {
                highMarker.style.left = `${stats.maxPct}%`;
                highMarker.title = `Highest: ${stats.maxPct}%`;
              }
            }
          }
        }
        return;
      }
    }

    $("adapters").innerHTML = list.length
      ? list.map(adapterCard).join("")
      : '<div class="empty">No active UP adapters with IP addresses found.</div>';
  } catch (e) {
    if (!isLive) {
      $("adapterCount").textContent = "0";
      $("subtitle").textContent = "Scan failed";
      $("adapters").innerHTML = `<div class="empty err">${esc(e && e.message ? e.message : e)}</div>`;
    }
  } finally {
    if (!isLive) {
      btn.classList.remove("spin");
    }
    syncChips();
  }
}

function adaptersAsText() {
  if (!state.adapters.length) return "No active adapters.";
  return state.adapters
    .map((a) => {
      const L = [`Adapter: ${a.alias} (${a.desc})`, `Status : ${a.status}`];
      if (a.ipv4.length) L.push(`IPv4   : ${a.ipv4.join(", ")}`);
      if (a.ipv6.length) L.push(`IPv6   : ${a.ipv6.join(", ")}`);
      if (a.gateway.length) L.push(`Gateway: ${a.gateway.join(", ")}`);
      if (a.dns.length) L.push(`DNS    : ${a.dns.join(", ")}`);
      if (a.mac) L.push(`MAC    : ${a.mac}`);
      if (a.isWifi && a.wifi) {
        const w = a.wifi;
        const stats = state.signalStats[a.alias];
        const extremes = stats ? ` [Low: ${stats.minPct}%${stats.minDbm ? ` (${stats.minDbm})` : ""} / High: ${stats.maxPct}%${stats.maxDbm ? ` (${stats.maxDbm})` : ""}]` : "";
        L.push("--- Wi-Fi ---", `SSID      : ${w.ssid}`, `Standard  : ${w.standard}`, `Frequency : ${w.band}`,
          `Channel   : ${w.channel}`, `Width     : ${w.channelWidth || "–"}`, `Speed R/T : ${w.rxRate} / ${w.txRate} Mbps`, `Signal    : ${w.signal}${w.signalDbm ? ` (${w.signalDbm})` : ""}${extremes}`);
      }
      return L.join("\n");
    })
    .join("\n" + "=".repeat(55) + "\n");
}

// --------------------------------------------------------------- ping
function logLine(text, cls = "") {
  const log = $("log");
  const d = document.createElement("div");
  if (cls) d.className = cls;
  d.textContent = text;
  log.appendChild(d);
  while (log.childElementCount > 500) log.firstChild.remove();
  log.scrollTop = log.scrollHeight;
}

function resetStats() {
  state.samples = [];
  state.sent = 0;
  state.recv = 0;
  renderStats();
  $("lastMs").textContent = "–";
  $("lastMs").className = "";
  setVerdict("Starting…", "");
}

function setVerdict(text, cls) {
  const v = $("verdict");
  v.textContent = text;
  v.className = "verdict " + cls;
}

function renderStats() {
  const ok = state.samples.filter((s) => s != null);
  $("sSent").textContent = state.sent;
  $("sRecv").textContent = state.recv;
  $("sLoss").textContent = state.sent ? Math.round(((state.sent - state.recv) / state.sent) * 100) + "%" : "0%";
  $("sMin").textContent = ok.length ? Math.min(...ok).toFixed(0) : "–";
  $("sMax").textContent = ok.length ? Math.max(...ok).toFixed(0) : "–";
  $("sAvg").textContent = ok.length ? (ok.reduce((a, b) => a + b, 0) / ok.length).toFixed(0) : "–";
  drawSpark();
}

function drawSpark() {
  const W = 400, H = 90, pad = 10;
  const pts = state.samples.slice(-40);
  const ok = pts.filter((s) => s != null);
  const svg = $("spark");
  if (!pts.length) { svg.innerHTML = ""; return; }

  const max = Math.max(20, ...ok) * 1.15;
  const step = pts.length > 1 ? (W - pad * 2) / (pts.length - 1) : 0;
  const y = (v) => H - pad - (v / max) * (H - pad * 2);

  let line = "", area = "", started = false, firstX = 0, lastX = 0;
  const lossMarks = [];
  pts.forEach((v, i) => {
    const x = pad + i * step;
    if (v == null) { lossMarks.push(x); return; }
    line += (started ? "L" : "M") + x.toFixed(1) + " " + y(v).toFixed(1) + " ";
    if (!started) { firstX = x; started = true; }
    lastX = x;
  });
  if (started) area = line + `L${lastX.toFixed(1)} ${H} L${firstX.toFixed(1)} ${H} Z`;

  const last = ok[ok.length - 1];
  const lastIdx = pts.lastIndexOf(last);
  svg.innerHTML = `
    <defs>
      <linearGradient id="g1" x1="0" x2="1"><stop offset="0" stop-color="#7c8cff"/><stop offset="1" stop-color="#5eead4"/></linearGradient>
      <linearGradient id="g2" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#7c8cff" stop-opacity=".35"/><stop offset="1" stop-color="#7c8cff" stop-opacity="0"/></linearGradient>
    </defs>
    ${[0.25, 0.5, 0.75].map((f) => `<line x1="0" x2="${W}" y1="${H * f}" y2="${H * f}" stroke="rgba(255,255,255,.05)"/>`).join("")}
    ${area ? `<path d="${area}" fill="url(#g2)"/>` : ""}
    ${line ? `<path d="${line}" fill="none" stroke="url(#g1)" stroke-width="2.2" stroke-linejoin="round" stroke-linecap="round" vector-effect="non-scaling-stroke"/>` : ""}
    ${lossMarks.map((x) => `<line x1="${x}" x2="${x}" y1="${H - 24}" y2="${H - 4}" stroke="#fb7185" stroke-width="2.5" stroke-linecap="round" vector-effect="non-scaling-stroke"/>`).join("")}
    ${last != null ? `<circle cx="${(pad + lastIdx * step).toFixed(1)}" cy="${y(last).toFixed(1)}" r="3.5" fill="#5eead4"/>` : ""}`;
}

function quality(ms) {
  return ms < 60 ? "good" : ms < 150 ? "mid" : "bad";
}

function setRunning(on) {
  state.running = on;
  const b = $("btnPing");
  b.classList.toggle("stop", on);
  b.innerHTML = on
    ? '<svg viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="6" width="12" height="12" rx="2"/></svg><span>Stop</span>'
    : '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M8 5v14l11-7z"/></svg><span>Start</span>';
  const tag = $("liveTag");
  tag.className = "live" + (on ? " run" : "");
  tag.innerHTML = `<i></i>${on ? "Running" : "Idle"}`;
}

async function togglePing() {
  if (state.running) {
    go().StopPing();
    return;
  }
  const host = $("host").value.trim();
  if (!host) return toast("Enter an address first", true);
  resetStats();
  logLine(`> ping ${host}${state.count ? " (" + state.count + "x)" : " (continuous)"}`, "cmd");
  setRunning(true);
  try {
    await go().StartPing(host, state.count);
  } catch (e) {
    setRunning(false);
    setVerdict("Failed to start", "bad");
    logLine(String(e && e.message ? e.message : e), "bad");
    toast(String(e && e.message ? e.message : e), true);
  }
}

function pingTarget(t) {
  if (t === "gateway") {
    if (!state.gateways.length) return toast("No default gateway found", true);
    t = state.gateways[0];
  }
  if (state.running) return toast("Stop the current ping first", true);
  $("host").value = t;
  syncChips();
  togglePing();
}

function syncChips() {
  const cur = $("host").value.trim();
  document.querySelectorAll(".tchip").forEach((c) => {
    const t = c.dataset.target === "gateway" ? state.gateways[0] : c.dataset.target;
    c.classList.toggle("on", !!t && t === cur);
  });
}

// ------------------------------------------------------------- PLDT router
async function loadRouterLocations() {
  try {
    const list = (await go().GetRouterLocations()) || [];
    state.routerLocations = list;
    $("locCount").textContent = `${list.length} location${list.length === 1 ? "" : "s"}`;
    renderRouterLocations();
  } catch (e) {
    toast("Failed to load locations: " + (e.message || e), true);
  }
}

function renderRouterLocations() {
  const container = $("routerLocationsList");
  if (!state.routerLocations.length) {
    container.innerHTML = '<div class="empty">No locations saved. Click "Add Location" above.</div>';
    return;
  }
  container.innerHTML = state.routerLocations
    .map((loc) => {
      const u = loc.url || "http://192.168.1.1/html/login_pldt.html";
      return `
        <article class="loc-card" data-name="${esc(loc.name)}">
          <div class="loc-card-top">
            <div class="loc-card-title">
              <span>🏠</span>
              <b>${esc(loc.name)}</b>
            </div>
            <span class="badge-loc">${esc(u)}</span>
          </div>
          <div class="loc-creds">
            <div class="loc-cred-item">
              <span>User:</span>
              <b class="val-copy" data-copy="${esc(loc.username)}" title="Click to copy username">${esc(loc.username)}</b>
            </div>
            <div class="loc-cred-item">
              <span>Password:</span>
              <b class="val-copy" data-copy="${esc(loc.password)}" title="Click to copy password">••••••••</b>
            </div>
          </div>
          <div class="loc-actions">
            <div class="loc-actions-left">
              <button class="btn primary small btn-launch" data-name="${esc(loc.name)}" title="Open in default browser with password copied to clipboard">
                🌐 Open in Browser
              </button>
              <button class="btn ghost small btn-copy-script" data-name="${esc(loc.name)}" title="Copy Auto-Login JavaScript snippet">
                📋 Copy Script
              </button>
            </div>
            <div class="loc-actions-right">
              <button class="btn ghost small btn-edit-loc" data-name="${esc(loc.name)}" title="Edit Location">✏️ Edit</button>
              <button class="btn ghost small danger btn-del-loc" data-name="${esc(loc.name)}" title="Delete Location">🗑️ Delete</button>
            </div>
          </div>
        </article>
      `;
    })
    .join("");

  container.querySelectorAll(".btn-launch").forEach((btn) => {
    btn.onclick = () => launchRouterLocation(btn.dataset.name);
  });
  container.querySelectorAll(".btn-copy-script").forEach((btn) => {
    btn.onclick = async () => {
      const loc = state.routerLocations.find((l) => l.name === btn.dataset.name);
      if (!loc) return;
      try {
        const script = await go().GetAutoLoginScript(loc.username || "admin", loc.password || "");
        await copy(script, "Auto-login JavaScript copied to clipboard!");
      } catch (e) {
        toast("Failed to copy script", true);
      }
    };
  });
  container.querySelectorAll(".btn-copy-pwd").forEach((btn) => {
    btn.onclick = () => copy(btn.dataset.pwd, "Password copied to clipboard");
  });
  container.querySelectorAll(".val-copy").forEach((el) => {
    el.onclick = () => copy(el.dataset.copy, "Copied " + el.dataset.copy);
  });
  container.querySelectorAll(".btn-edit-loc").forEach((btn) => {
    btn.onclick = () => openEditLocation(btn.dataset.name);
  });
  container.querySelectorAll(".btn-del-loc").forEach((btn) => {
    btn.onclick = () => deleteLocation(btn.dataset.name);
  });
}

async function launchRouterLocation(name) {
  try {
    const res = await go().LaunchRouterPortal(name);
    toast(`Opened ${name} router portal in browser! Password copied to clipboard.`);
  } catch (e) {
    toast(String(e && e.message ? e.message : e), true);
  }
}

function openAddLocation() {
  state.editingLocation = null;
  $("locFormTitle").textContent = "Add New Location";
  $("locName").value = "";
  $("locName").disabled = false;
  $("locUser").value = "admin";
  $("locPass").value = "";
  $("locUrl").value = "http://192.168.1.1/html/login_pldt.html";
  $("locationForm").style.display = "block";
  $("locName").focus();
}

function openEditLocation(name) {
  const loc = state.routerLocations.find((l) => l.name === name);
  if (!loc) return;
  state.editingLocation = name;
  $("locFormTitle").textContent = `Edit Location — ${name}`;
  $("locName").value = loc.name;
  $("locName").disabled = true;
  $("locUser").value = loc.username || "admin";
  $("locPass").value = loc.password || "";
  $("locUrl").value = loc.url || "http://192.168.1.1/html/login_pldt.html";
  $("locationForm").style.display = "block";
  $("locPass").focus();
}

function hideLocationForm() {
  $("locationForm").style.display = "none";
  state.editingLocation = null;
}

async function saveLocationForm() {
  const name = $("locName").value.trim();
  const username = $("locUser").value.trim() || "admin";
  const password = $("locPass").value;
  const url = $("locUrl").value.trim() || "http://192.168.1.1/html/login_pldt.html";

  if (!name) return toast("Location name is required", true);
  if (!password) return toast("Password is required", true);

  try {
    const updated = await go().SaveRouterLocation({ name, username, password, url });
    state.routerLocations = updated || [];
    $("locCount").textContent = `${state.routerLocations.length} location${state.routerLocations.length === 1 ? "" : "s"}`;
    renderRouterLocations();
    hideLocationForm();
    toast(`Location '${name}' saved`);
  } catch (e) {
    toast("Error saving location: " + (e.message || e), true);
  }
}

async function deleteLocation(name) {
  if (!confirm(`Delete location '${name}'?`)) return;
  try {
    const updated = await go().DeleteRouterLocation(name);
    state.routerLocations = updated || [];
    $("locCount").textContent = `${state.routerLocations.length} location${state.routerLocations.length === 1 ? "" : "s"}`;
    renderRouterLocations();
    toast(`Location '${name}' deleted`);
  } catch (e) {
    toast("Error deleting location: " + (e.message || e), true);
  }
}

// ------------------------------------------------------------- DNS & IPConfig
function logTerminal(text) {
  const term = $("terminalOutput");
  if (!term) return;
  const time = new Date().toLocaleTimeString();
  term.textContent += `\n[${time}] ${text}`;
  term.scrollTop = term.scrollHeight;
}

async function openDNSModal(preferredAlias = null) {
  $("dnsModal").style.display = "flex";
  populateDNSAdapters(preferredAlias);
  await loadDNSPresets();
}

function closeDNSModal() {
  $("dnsModal").style.display = "none";
  hidePresetSaveBox();
}

function populateDNSAdapters(selectedAlias = null) {
  const sel = $("dnsAdapterSelect");
  sel.innerHTML = "";

  const optAll = document.createElement("option");
  optAll.value = "";
  optAll.textContent = "-- All Adapters (Global ipconfig) --";
  sel.appendChild(optAll);

  state.adapters.forEach((a) => {
    const opt = document.createElement("option");
    opt.value = a.alias;
    opt.textContent = `${a.alias} (${a.desc})`;
    if (selectedAlias && a.alias === selectedAlias) {
      opt.selected = true;
    }
    sel.appendChild(opt);
  });

  if (!selectedAlias && state.adapters.length > 0) {
    sel.selectedIndex = 1;
  }
}

async function loadDNSPresets() {
  try {
    const list = await go().GetDNSPresets();
    state.dnsPresets = list || [];
    renderDNSPresetGrid();
  } catch (e) {
    toast("Failed to load DNS presets: " + (e.message || e), true);
  }
}

function renderDNSPresetGrid() {
  const grid = $("dnsPresetGrid");
  if (!state.dnsPresets.length) {
    grid.innerHTML = '<div style="color:var(--muted);font-size:12px;padding:8px;">No presets available</div>';
    return;
  }

  grid.innerHTML = state.dnsPresets
    .map((p) => {
      const isAct = state.selectedPresetId === p.id;
      const addrsHtml = p.isDhcp
        ? '<span>Automatic (DHCP)</span>'
        : `<span>${esc(p.primary)}</span>${p.secondary ? `<span>${esc(p.secondary)}</span>` : ""}`;

      const dohBadge = p.dohTemplate
        ? `<span class="badge-doh" title="DoH: ${esc(p.dohTemplate)}">🔒 DoH</span>`
        : "";

      const delBtn = p.isCustom
        ? `<button class="preset-chip-del" data-del-preset="${esc(p.id)}" title="Delete Custom Preset">×</button>`
        : "";

      return `
        <div class="preset-chip ${isAct ? "active" : ""}" data-preset-id="${esc(p.id)}">
          <div class="preset-chip-top">
            <span class="preset-chip-name" title="${esc(p.name)}">${esc(p.name)}</span>
            <div style="display:flex;align-items:center;gap:4px">
              ${dohBadge}
              ${delBtn}
            </div>
          </div>
          <div class="preset-chip-addrs">${addrsHtml}</div>
        </div>
      `;
    })
    .join("");

  grid.querySelectorAll(".preset-chip").forEach((chip) => {
    chip.onclick = (e) => {
      if (e.target.closest(".preset-chip-del")) return;
      selectDNSPreset(chip.dataset.presetId);
    };
  });

  grid.querySelectorAll(".preset-chip-del").forEach((btn) => {
    btn.onclick = async (e) => {
      e.stopPropagation();
      await deleteCustomPreset(btn.dataset.delPreset);
    };
  });
}

function selectDNSPreset(presetId) {
  state.selectedPresetId = presetId;
  const preset = state.dnsPresets.find((p) => p.id === presetId);
  if (!preset) return;

  renderDNSPresetGrid();

  if (preset.isDhcp) {
    $("modeDhcp").checked = true;
    $("modeStatic").checked = false;
    $("dnsPrimary").value = "";
    $("dnsSecondary").value = "";
    $("dnsDohTemplate").value = "";
    $("dnsStaticInputs").style.opacity = "0.45";
    $("dnsStaticInputs").style.pointerEvents = "none";
  } else {
    $("modeStatic").checked = true;
    $("modeDhcp").checked = false;
    $("dnsPrimary").value = preset.primary || "";
    $("dnsSecondary").value = preset.secondary || "";
    $("dnsDohTemplate").value = preset.dohTemplate || "";
    $("dnsStaticInputs").style.opacity = "1";
    $("dnsStaticInputs").style.pointerEvents = "auto";
  }
}

function showPresetSaveBox() {
  $("presetSaveBox").style.display = "block";
  $("customPresetName").value = "";
  $("customPresetDoh").value = $("dnsDohTemplate").value.trim();
  $("customPresetName").focus();
}

function hidePresetSaveBox() {
  $("presetSaveBox").style.display = "none";
}

async function confirmSavePreset() {
  const name = $("customPresetName").value.trim();
  if (!name) return toast("Preset name is required", true);

  const isDhcp = $("modeDhcp").checked;
  const primary = $("dnsPrimary").value.trim();
  const secondary = $("dnsSecondary").value.trim();
  const dohTemplate = $("customPresetDoh").value.trim();

  if (!isDhcp && !primary) {
    return toast("Primary DNS is required for static presets", true);
  }

  try {
    const updated = await go().SaveCustomDNSPreset({
      id: "",
      name,
      primary,
      secondary,
      dohTemplate,
      isDhcp,
      isCustom: true,
    });
    state.dnsPresets = updated || [];
    renderDNSPresetGrid();
    hidePresetSaveBox();
    toast(`Preset '${name}' saved`);
  } catch (e) {
    toast("Failed to save preset: " + (e.message || e), true);
  }
}

async function deleteCustomPreset(id) {
  const p = state.dnsPresets.find((x) => x.id === id);
  const name = p ? p.name : "preset";
  if (!confirm(`Delete custom DNS preset '${name}'?`)) return;

  try {
    const updated = await go().DeleteCustomDNSPreset(id);
    state.dnsPresets = updated || [];
    if (state.selectedPresetId === id) state.selectedPresetId = null;
    renderDNSPresetGrid();
    toast(`Preset '${name}' deleted`);
  } catch (e) {
    toast("Failed to delete preset: " + (e.message || e), true);
  }
}

async function applyDNSSettings() {
  const alias = $("dnsAdapterSelect").value;
  if (!alias) {
    return toast("Please select a specific network interface to apply DNS", true);
  }

  const isDhcp = $("modeDhcp").checked;
  const primary = $("dnsPrimary").value.trim();
  const secondary = $("dnsSecondary").value.trim();
  const dohTemplate = isDhcp ? "" : $("dnsDohTemplate").value.trim();

  if (!isDhcp && !primary) {
    return toast("Please enter a valid Primary DNS address", true);
  }

  const btn = $("btnApplyDNS");
  const origHtml = btn.innerHTML;
  btn.disabled = true;
  btn.textContent = "Applying...";

  const dohInfo = dohTemplate ? ` (DoH: ${dohTemplate})` : "";
  logTerminal(`> Applying ${isDhcp ? "DHCP" : primary + (secondary ? ", " + secondary : "")}${dohInfo} to [${alias}]...`);

  try {
    const res = await go().SetAdapterDNS(alias, isDhcp, primary, secondary, dohTemplate);
    logTerminal(`[SUCCESS] ${res}`);
    toast(`DNS updated for ${alias}`);
    refresh();
  } catch (e) {
    const errText = e && e.message ? e.message : String(e);
    logTerminal(`[ERROR] ${errText}`);
    toast("DNS change failed: " + errText, true);
  } finally {
    btn.disabled = false;
    btn.innerHTML = origHtml;
  }
}

async function runIPConfigCmd(action, explicitAlias = null) {
  const alias = explicitAlias !== null ? explicitAlias : $("dnsAdapterSelect").value;
  const cmdDisplay = `ipconfig /${action}${alias ? " " + alias : ""}`;

  logTerminal(`> ${cmdDisplay}...`);
  toast(`Executing ${cmdDisplay}...`);

  try {
    const out = await go().RunIPConfig(action, alias || "");
    logTerminal(out || "[OK] Completed without output.");
    toast(`${cmdDisplay} completed`);
    if (action === "renew") {
      refresh();
    }
  } catch (e) {
    const errText = e && e.message ? e.message : String(e);
    logTerminal(`[ERROR] ${errText}`);
    toast(`${action} failed: ${errText}`, true);
  }
}

// ------------------------------------------------------------- Ookla Speedtest
let stLogQueue = [];
let stLogRaf = null;

function flushSpeedtestLog() {
  stLogRaf = null;
  const term = $("stTerminalOutput");
  if (!term || !stLogQueue.length) return;

  const chunk = stLogQueue.join("\n");
  stLogQueue = [];

  term.textContent += (term.textContent ? "\n" : "") + chunk;
  // Keep terminal buffer bounded to avoid layout performance degradation
  if (term.textContent.length > 20000) {
    const lines = term.textContent.split("\n");
    if (lines.length > 150) {
      term.textContent = lines.slice(-150).join("\n");
    }
  }
  term.scrollTop = term.scrollHeight;
}

function logSpeedtest(text) {
  const time = new Date().toLocaleTimeString();
  stLogQueue.push(`[${time}] ${text}`);
  if (!stLogRaf) {
    stLogRaf = requestAnimationFrame(flushSpeedtestLog);
  }
}

async function switchTestTab(tab, preferredAlias = null) {
  state.activeTestTab = tab;
  const isSpeedtest = tab === "speedtest";

  const tabPing = $("tabPing");
  const tabSpeedtest = $("tabSpeedtest");
  if (tabPing) tabPing.classList.toggle("active", !isSpeedtest);
  if (tabSpeedtest) tabSpeedtest.classList.toggle("active", isSpeedtest);

  const btnSt = $("btnSpeedtest");
  if (btnSt) btnSt.classList.toggle("active", isSpeedtest);

  const pingView = $("pingView");
  const stView = $("speedtestView");
  if (pingView) {
    pingView.classList.toggle("active", !isSpeedtest);
    pingView.style.display = isSpeedtest ? "none" : "flex";
  }
  if (stView) {
    stView.classList.toggle("active", isSpeedtest);
    stView.style.display = isSpeedtest ? "flex" : "none";
  }

  const liveTag = $("liveTag");
  const stBadge = $("stStatusBadge");
  if (liveTag) liveTag.style.display = isSpeedtest ? "none" : "inline-flex";
  if (stBadge) stBadge.style.display = isSpeedtest ? "inline-flex" : "none";

  if (isSpeedtest) {
    populateSpeedtestAdapters(preferredAlias);
    try {
      const avail = await go().IsSpeedtestAvailable();
      if (!avail && $("stEngineSelect")) {
        $("stEngineSelect").value = "fast";
        state.speedtestEngine = "fast";
      }
    } catch (e) {}

    loadSpeedtestServers();
  }
}

function populateSpeedtestAdapters(selectedAlias = null) {
  const sel = $("stAdapterSelect");
  sel.innerHTML = "";

  const optAll = document.createElement("option");
  optAll.value = "";
  optAll.textContent = "-- Default / Auto Route --";
  sel.appendChild(optAll);

  state.adapters.forEach((a) => {
    const opt = document.createElement("option");
    opt.value = a.alias;
    opt.textContent = `${a.alias} (${a.ipv4.length ? a.ipv4[0] : a.desc})`;
    if (selectedAlias && a.alias === selectedAlias) {
      opt.selected = true;
    }
    sel.appendChild(opt);
  });

  if (selectedAlias) {
    sel.value = selectedAlias;
  }
}

async function loadSpeedtestServers() {
  const engine = $("stEngineSelect") ? $("stEngineSelect").value : (state.speedtestEngine || "fast");
  const sel = $("stServerSelect");
  if (!sel) return;

  if (engine === "fast") {
    sel.innerHTML = '<option value="0">Auto (Nearest Netflix OCA)</option>';
    if (state.fastServers.length) {
      state.fastServers.forEach((s) => {
        const opt = document.createElement("option");
        opt.value = s.id;
        opt.textContent = `${s.name} (${s.location}, ${s.country})`;
        sel.appendChild(opt);
      });
      return;
    }

    try {
      logSpeedtest("Fetching nearby Fast.com (Netflix OCA) servers…");
      const servers = (await go().GetFastServers()) || [];
      state.fastServers = servers;
      sel.innerHTML = '<option value="0">Auto (Nearest Netflix OCA)</option>';
      servers.forEach((s) => {
        const opt = document.createElement("option");
        opt.value = s.id;
        opt.textContent = `${s.name} (${s.location}, ${s.country})`;
        sel.appendChild(opt);
      });
      logSpeedtest(`Found ${servers.length} nearby Fast.com servers.`);
    } catch (e) {
      logSpeedtest(`Notice: Fast.com server lookup: ${e.message || e}. Using auto-selection.`);
    }
  } else {
    sel.innerHTML = '<option value="0">Auto (Nearest Optimal Server)</option>';
    if (state.ooklaServers.length) {
      state.ooklaServers.forEach((s) => {
        const opt = document.createElement("option");
        opt.value = s.id;
        opt.textContent = `${s.name} (${s.location}, ${s.country})`;
        sel.appendChild(opt);
      });
      return;
    }

    try {
      logSpeedtest("Fetching nearby Ookla Speedtest servers…");
      const servers = (await go().GetSpeedtestServers()) || [];
      state.ooklaServers = servers;
      sel.innerHTML = '<option value="0">Auto (Nearest Optimal Server)</option>';
      servers.forEach((s) => {
        const opt = document.createElement("option");
        opt.value = s.id;
        opt.textContent = `${s.name} (${s.location}, ${s.country})`;
        sel.appendChild(opt);
      });
      logSpeedtest(`Found ${servers.length} nearby Ookla servers.`);
    } catch (e) {
      logSpeedtest(`Notice: Ookla server lookup: ${e.message || e}. Using auto-selection.`);
    }
  }
}

function resetSpeedtestUI() {
  $("stDownloadVal").textContent = "–";
  $("stUploadVal").textContent = "–";
  $("stPingVal").textContent = "–";
  $("stJitterVal").textContent = "–";
  $("stLossVal").textContent = "0%";
  $("stDownloadBar").style.width = "0%";
  $("stUploadBar").style.width = "0%";
  $("stServerInfo").textContent = "Auto-selecting…";
  $("stIspInfo").textContent = "–";
  $("stResultRow").style.display = "none";
  $("stCardDownload").classList.remove("active");
  $("stCardUpload").classList.remove("active");
  setSpeedtestStatus("Idle", "");
}

function setSpeedtestStatus(text, cls = "") {
  const badge = $("stStatusBadge");
  badge.textContent = text;
  badge.className = "st-status-badge " + cls;
}

function setSpeedtestRunning(running) {
  state.speedtestRunning = running;
  const btn = $("btnStartSpeedtest");
  if (!btn) return;
  btn.disabled = false;
  if (running) {
    btn.classList.add("stop");
    btn.innerHTML = '<svg viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="6" width="12" height="12" rx="2"/></svg><span>Stop</span>';
  } else {
    btn.classList.remove("stop");
    btn.innerHTML = '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M8 5v14l11-7z"/></svg><span>Start Speedtest</span>';
  }
}

async function toggleSpeedtest() {
  if (state.speedtestRunning) {
    const btn = $("btnStartSpeedtest");
    if (btn) {
      btn.disabled = true;
      btn.innerHTML = '<svg viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="6" width="12" height="12" rx="2"/></svg><span>Stopping…</span>';
    }
    setSpeedtestStatus("Stopping…", "testing");
    logSpeedtest("Stopping Speedtest…");
    try {
      await go().StopSpeedtest();
    } catch (e) {
      console.error("StopSpeedtest error:", e);
    }
    setTimeout(() => {
      if (state.speedtestRunning) {
        setSpeedtestRunning(false);
        setSpeedtestStatus("Stopped", "");
      }
    }, 1500);
    return;
  }

  const engine = $("stEngineSelect") ? $("stEngineSelect").value : (state.speedtestEngine || "fast");
  const iface = $("stAdapterSelect").value;
  const serverID = parseInt($("stServerSelect").value, 10) || 0;

  resetSpeedtestUI();
  setSpeedtestRunning(true);
  setSpeedtestStatus("Connecting…", "testing");

  if (engine === "fast") {
    logSpeedtest(`> fast.com speedtest${iface ? " --interface " + iface : ""}${serverID ? " --server " + serverID : ""}`);
  } else {
    logSpeedtest(`> speedtest --accept-license --accept-gdpr -f jsonl -p yes${iface ? " -i " + iface : ""}${serverID ? " -s " + serverID : ""}`);
  }

  try {
    await go().StartSpeedtest(engine, serverID, iface);
  } catch (e) {
    setSpeedtestRunning(false);
    setSpeedtestStatus("Failed", "");
    const err = e && e.message ? e.message : String(e);
    logSpeedtest(`[ERROR] ${err}`);
    toast("Speedtest failed: " + err, true);
  }
}

// ------------------------------------------------------------- wiring
function init() {
  $("btnRefresh").onclick = () => refresh(false);
  if ($("btnAutoRefresh")) $("btnAutoRefresh").onclick = cycleAutoRefresh;
  $("btnCopyAll").onclick = () => copy(adaptersAsText(), "Adapter info copied");
  $("btnExit").onclick = () => go().Quit();
  $("wClose").onclick = () => go().Quit();
  $("wMin").onclick = () => go().Minimise();
  $("wMax").onclick = () => go().ToggleMaximise();
  $("btnPing").onclick = togglePing;
  $("btnClear").onclick = () => ($("log").innerHTML = "");
  $("host").addEventListener("input", syncChips);
  $("host").addEventListener("keydown", (e) => { if (e.key === "Enter") togglePing(); });

  // PLDT Router modal wiring
  $("btnRouter").onclick = () => {
    $("routerModal").style.display = "flex";
    loadRouterLocations();
  };
  $("btnCloseRouterModal").onclick = () => {
    $("routerModal").style.display = "none";
    hideLocationForm();
  };
  $("routerModal").onclick = (e) => {
    if (e.target === $("routerModal")) {
      $("routerModal").style.display = "none";
      hideLocationForm();
    }
  };
  $("btnAddLocation").onclick = openAddLocation;
  $("btnCancelLocation").onclick = hideLocationForm;
  $("btnSaveLocation").onclick = saveLocationForm;

  // Panel tab switching (Ping vs Speedtest)
  $("tabPing").onclick = () => switchTestTab("ping");
  $("tabSpeedtest").onclick = () => switchTestTab("speedtest");
  $("btnSpeedtest").onclick = () => {
    if (state.activeTestTab === "speedtest") {
      switchTestTab("ping");
    } else {
      switchTestTab("speedtest");
    }
  };
  $("btnStartSpeedtest").onclick = toggleSpeedtest;
  $("btnClearSpeedtestTerminal").onclick = () => { $("stTerminalOutput").textContent = "Cleared."; };
  if ($("stEngineSelect")) {
    $("stEngineSelect").onchange = () => {
      state.speedtestEngine = $("stEngineSelect").value;
      loadSpeedtestServers();
    };
  }

  // DNS & IPConfig modal wiring
  $("btnDNSModal").onclick = () => openDNSModal();
  $("btnCloseDNSModal").onclick = closeDNSModal;
  $("dnsModal").onclick = (e) => {
    if (e.target === $("dnsModal")) closeDNSModal();
  };
  $("btnRefreshAdaptersInModal").onclick = () => populateDNSAdapters($("dnsAdapterSelect").value);
  $("btnToggleSavePreset").onclick = showPresetSaveBox;
  $("btnConfirmSavePreset").onclick = confirmSavePreset;
  $("btnCancelSavePreset").onclick = hidePresetSaveBox;
  $("btnApplyDNS").onclick = applyDNSSettings;

  $("modeDhcp").onchange = () => {
    if ($("modeDhcp").checked) {
      $("dnsStaticInputs").style.opacity = "0.45";
      $("dnsStaticInputs").style.pointerEvents = "none";
    }
  };
  $("modeStatic").onchange = () => {
    if ($("modeStatic").checked) {
      $("dnsStaticInputs").style.opacity = "1";
      $("dnsStaticInputs").style.pointerEvents = "auto";
    }
  };

  $("btnIpRenew").onclick = () => runIPConfigCmd("renew");
  $("btnIpRelease").onclick = () => runIPConfigCmd("release");
  $("btnIpFlushDns").onclick = () => runIPConfigCmd("flushdns");
  $("btnClearTerminal").onclick = () => { $("terminalOutput").textContent = "Cleared."; };

  document.querySelectorAll("#dohChips .doh-chip").forEach((btn) => {
    btn.onclick = () => {
      $("dnsDohTemplate").value = btn.dataset.doh || "";
      if ($("customPresetDoh")) {
        $("customPresetDoh").value = btn.dataset.doh || "";
      }
    };
  });

  // Double-click title bar to maximise
  document.querySelector(".titlebar").addEventListener("dblclick", (e) => {
    if (!e.target.closest(".actions")) go().ToggleMaximise();
  });

  document.querySelectorAll(".tchip").forEach((c) => (c.onclick = () => pingTarget(c.dataset.target)));
  document.querySelectorAll("#countSeg button").forEach((b) => {
    b.onclick = () => {
      state.count = parseInt(b.dataset.count, 10);
      document.querySelectorAll("#countSeg button").forEach((x) => x.classList.toggle("on", x === b));
    };
  });

  // Click any value chip to copy it or adapter action button
  $("adapters").addEventListener("click", (e) => {
    const act = e.target.closest("[data-action]");
    if (act) {
      const alias = act.dataset.alias;
      const action = act.dataset.action;
      if (action === "speedtest") {
        switchTestTab("speedtest", alias);
      } else if (action === "dns") {
        openDNSModal(alias);
      } else if (action === "renew") {
        runIPConfigCmd("renew", alias);
      } else if (action === "release") {
        runIPConfigCmd("release", alias);
      }
      return;
    }
    const v = e.target.closest("[data-copy]");
    if (v) copy(v.dataset.copy, "Copied " + v.dataset.copy);
  });

  document.addEventListener("keydown", (e) => {
    if (e.key === "F5") { e.preventDefault(); refresh(); }
    if (e.ctrlKey && e.key.toLowerCase() === "q") { e.preventDefault(); go().Quit(); }
    if (e.key === "Escape") {
      if ($("routerModal").style.display !== "none") {
        $("routerModal").style.display = "none";
        hideLocationForm();
      }
      if ($("dnsModal").style.display !== "none") {
        closeDNSModal();
      }
    }
  });

  // Events streamed from Go
  rt().EventsOn("speedtest:raw", (line) => {
    logSpeedtest(line);
  });

  rt().EventsOn("speedtest:event", (ev) => {
    if (!ev || !ev.type) return;

    if (ev.type === "testStart") {
      setSpeedtestStatus("Testing…", "testing");
      if (ev.server && ev.server.name) {
        $("stServerInfo").textContent = `${ev.server.name} (${ev.server.location || ""})`;
      }
      if (ev.isp) {
        $("stIspInfo").textContent = ev.isp;
      }
    } else if (ev.type === "ping" && ev.ping) {
      setSpeedtestStatus("Ping", "testing");
      $("stPingVal").textContent = ev.ping.latency ? ev.ping.latency.toFixed(1) : "–";
      $("stJitterVal").textContent = ev.ping.jitter ? ev.ping.jitter.toFixed(1) : "–";
    } else if (ev.type === "download" && ev.download) {
      setSpeedtestStatus("Download", "testing");
      $("stCardDownload").classList.add("active");
      $("stCardUpload").classList.remove("active");
      const mbps = (ev.download.bandwidth * 8) / 1000000;
      $("stDownloadVal").textContent = mbps.toFixed(2);
      const pct = Math.min(100, Math.max(0, (ev.download.progress || 0) * 100));
      $("stDownloadBar").style.width = pct + "%";
    } else if (ev.type === "upload" && ev.upload) {
      setSpeedtestStatus("Upload", "testing");
      $("stCardDownload").classList.remove("active");
      $("stCardUpload").classList.add("active");
      const mbps = (ev.upload.bandwidth * 8) / 1000000;
      $("stUploadVal").textContent = mbps.toFixed(2);
      const pct = Math.min(100, Math.max(0, (ev.upload.progress || 0) * 100));
      $("stUploadBar").style.width = pct + "%";
    } else if (ev.type === "result") {
      setSpeedtestStatus("Completed", "done");
      $("stCardDownload").classList.remove("active");
      $("stCardUpload").classList.remove("active");
      if (ev.download) {
        const mbps = (ev.download.bandwidth * 8) / 1000000;
        $("stDownloadVal").textContent = mbps.toFixed(2);
        $("stDownloadBar").style.width = "100%";
      }
      if (ev.upload) {
        const mbps = (ev.upload.bandwidth * 8) / 1000000;
        $("stUploadVal").textContent = mbps.toFixed(2);
        $("stUploadBar").style.width = "100%";
      }
      if (ev.ping) {
        $("stPingVal").textContent = ev.ping.latency ? ev.ping.latency.toFixed(1) : "–";
        $("stJitterVal").textContent = ev.ping.jitter ? ev.ping.jitter.toFixed(1) : "–";
      }
      if (ev.packetLoss != null) {
        $("stLossVal").textContent = ev.packetLoss.toFixed(1) + "%";
      }
      if (ev.isp) {
        $("stIspInfo").textContent = ev.isp;
      }
      if (ev.server) {
        $("stServerInfo").textContent = `${ev.server.name} (${ev.server.location || ""})`;
      }
      if (ev.result && ev.result.url) {
        const link = $("stResultLink");
        link.href = ev.result.url;
        link.textContent = ev.result.url.includes("fast.com") ? "View on Fast.com ↗" : "View on Speedtest.net ↗";
        $("stResultRow").style.display = "flex";
      }
    }
  });

  rt().EventsOn("speedtest:done", (r) => {
    setSpeedtestRunning(false);
    $("stCardDownload").classList.remove("active");
    $("stCardUpload").classList.remove("active");
    if (r && r.cancelled) {
      setSpeedtestStatus("Stopped", "");
      logSpeedtest("Speedtest stopped by user.");
      toast("Speedtest stopped");
    } else if (r && r.error) {
      setSpeedtestStatus("Error", "");
      logSpeedtest("[ERROR] " + r.error);
      toast("Speedtest error: " + r.error, true);
    } else {
      setSpeedtestStatus("Completed", "done");
      toast("Speedtest finished successfully!");
    }
  });

  // Events streamed from Go
  rt().EventsOn("ping:line", (line) => {
    const low = String(line).toLowerCase();
    logLine(line, /time[=<]/.test(low) ? "ok" : /timed out|unreachable|could not|failure|expired/.test(low) ? "bad" : "dim");
  });
  rt().EventsOn("ping:reply", (ms) => {
    state.sent++; state.recv++;
    state.samples.push(ms);
    const el = $("lastMs");
    el.textContent = ms < 1 ? "<1" : Math.round(ms);
    el.className = "big-num";
    $("lastMs").parentElement.className = "big " + quality(ms);
    setVerdict(ms < 60 ? "Excellent" : ms < 150 ? "Fair" : "Slow", quality(ms));
    renderStats();
  });
  rt().EventsOn("ping:loss", () => {
    state.sent++;
    state.samples.push(null);
    $("lastMs").textContent = "×";
    $("lastMs").parentElement.className = "big bad";
    setVerdict("Packet lost", "bad");
    renderStats();
  });
  rt().EventsOn("ping:done", (r) => {
    setRunning(false);
    if (r.cancelled) {
      logLine("Stopped by user.", "warn");
      setVerdict("Stopped", "");
    } else if (r.ok) {
      logLine(`[OK] ${r.host} is reachable`, "ok");
      setVerdict("Reachable", "good");
      toast(`${r.host} is reachable`);
    } else {
      logLine(`[FAIL] ${r.host} did not respond`, "bad");
      setVerdict("Unreachable", "bad");
      $("lastMs").textContent = "×";
      $("lastMs").parentElement.className = "big bad";
      toast(`${r.host} did not respond`, true);
    }
  });

  document.addEventListener("visibilitychange", () => {
    if (document.hidden) {
      autoRefreshPaused = true;
    } else {
      autoRefreshPaused = false;
      if (autoRefreshInterval > 0) refresh(true);
    }
  });

  switchTestTab("ping");
  syncChips();
  refresh();
  startAutoRefreshTimer();
}

// Wails injects window.go / window.runtime slightly after load.
function boot() {
  if (window.go && window.go.main && window.runtime) init();
  else setTimeout(boot, 30);
}
window.addEventListener("DOMContentLoaded", boot);
