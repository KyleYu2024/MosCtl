const $ = (s) => document.querySelector(s);
const state = { rules: [], current: null, original: "", loading: false };
async function api(path, options = {}) {
  const res = await fetch(path, {
    ...options,
    signal:
      options.signal ||
      AbortSignal.timeout(
        options.method === "PUT" || path === "/api/restart" ? 65000 : 15000,
      ),
    headers: { "Content-Type": "application/json", ...(options.headers || {}) },
  });
  let data = {};
  try {
    data = await res.json();
  } catch {}
  if (res.status === 401) {
    showLogin();
    throw new Error(data.error || "请重新登录");
  }
  if (!res.ok) throw new Error(data.error || "请求失败");
  return data;
}
let healthTimer = null,
  healthBusy = false;
function showLogin() {
  closeUserMenu();
  stopLogs();
  stopStats();
  clearTimeout(kernelTimer);
  clearInterval(healthTimer);
  $("#app").classList.add("hidden");
  $("#login").classList.remove("hidden");
  setTimeout(() => $("#username").focus(), 30);
}
function showApp() {
  $("#login").classList.add("hidden");
  $("#app").classList.remove("hidden");
  selectPage(pageFromURL(), false);
  clearInterval(healthTimer);
  loadHealth();
  healthTimer = setInterval(() => {
    if (!document.hidden) loadHealth();
  }, 10000);
}
function setConnection(text, state, title = "") {
  const el = $("#connectionStatus");
  el.classList.toggle("offline", state === "offline");
  el.classList.toggle("warning", state === "warning");
  el.title = title;
  $("#connectionText").textContent = text;
}
function updateConnection() {
  if (!navigator.onLine) setConnection("网络离线", "offline");
  else if (!$("#app").classList.contains("hidden")) loadHealth();
}
async function loadHealth() {
  if (healthBusy) return;
  healthBusy = true;
  try {
    const d = await api("/api/health");
    const stale =
      d.checked_at && Date.now() - new Date(d.checked_at).getTime() > 90000;
    setConnection(
      !d.running
        ? "DNS 未运行"
        : !d.checked_at
          ? "DNS 检查中"
          : stale
            ? "检查已过期"
            : d.dns_ok
              ? "DNS 正常"
              : "DNS 异常",
      !d.running
        ? "offline"
        : !d.checked_at || stale || !d.dns_ok
          ? "warning"
          : "online",
      d.checked_at
        ? "检查时间：" +
            new Date(d.checked_at).toLocaleString("zh-CN") +
            (d.error ? " · " + d.error : "")
        : "尚未完成解析检查",
    );
  } catch (e) {
    setConnection("服务不可达", "offline", e.message);
  } finally {
    healthBusy = false;
  }
}
let logTimer = null,
  logBusy = false,
  logLastContent = null;
function stopLogs() {
  clearInterval(logTimer);
  logTimer = null;
}
function scheduleLogs() {
  stopLogs();
  if (
    $("#autoLogs").checked &&
    !$("#logsPage").classList.contains("hidden") &&
    !$("#app").classList.contains("hidden")
  )
    logTimer = setInterval(() => {
      if (!document.hidden) loadLogs();
    }, 3000);
}
const logTimePrefix =
  /^\[?(\d{4}[-/]\d{2}[-/]\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?)\]?\s*/;
function parseLogEntries(content) {
  const entries = [];
  for (const raw of content
    .replace(/\x1b\[[0-?]*[ -/]*[@-~]/g, "")
    .replace(/\r\n?/g, "\n")
    .split("\n")) {
    if (!raw.trim()) continue;
    if (
      entries.length &&
      /^\s+\S/.test(raw) &&
      !logTimePrefix.test(raw.trimStart()) &&
      !raw.trimStart().startsWith("{")
    ) {
      entries[entries.length - 1].message += "\n" + raw;
      continue;
    }
    let message = raw,
      time = "",
      level = "info",
      label = "系统",
      source = "";
    try {
      const data = JSON.parse(raw);
      if (
        data &&
        typeof data === "object" &&
        typeof (data.msg ?? data.message) === "string"
      ) {
        message = data.msg ?? data.message;
        time = String(data.ts ?? data.time ?? "");
        label = String(data.level ?? "系统").toUpperCase();
        source = [data.logger, data.caller]
          .filter((v) => v !== undefined && v !== null && v !== "")
          .map(String)
          .join(" · ");
        const extra = Object.fromEntries(
          Object.entries(data).filter(
            ([key]) =>
              ![
                "msg",
                "message",
                "ts",
                "time",
                "level",
                "logger",
                "caller",
              ].includes(key),
          ),
        );
        if (Object.keys(extra).length)
          message += "\n" + JSON.stringify(extra, null, 2);
      }
    } catch {}
    if (message === raw) {
      const stamp = message.match(logTimePrefix);
      if (stamp) {
        time = stamp[1];
        message = message.slice(stamp[0].length);
      }
      const severity = message.match(
        /^(DEBUG|INFO|WARN(?:ING)?|ERROR|FATAL|PANIC)\b[:\s]*/i,
      );
      if (severity) {
        label = severity[1].toUpperCase();
        message = message.slice(severity[0].length);
        const fields = message.split("\t");
        if (fields.length > 1) {
          source = fields.shift();
          message = fields.join("\n");
        }
      }
    }
    if (/^(WARN|WARNING)$/.test(label) || /^⚠/.test(message)) level = "warn";
    else if (/^(ERROR|FATAL|PANIC)$/.test(label) || /^❌/.test(message))
      level = "error";
    else if (/^(✅|🟢)/.test(message)) level = "success";
    else if (label === "DEBUG") level = "debug";
    if (label === "系统" && level !== "info")
      label = { warn: "警告", error: "错误", success: "成功", debug: "DEBUG" }[
        level
      ];
    entries.push({ time, level, label, source, message });
  }
  return entries;
}
function formatLogTime(raw) {
  if (!raw) return "";
  let value = raw;
  if (/^\d+(?:\.\d+)?$/.test(value)) {
    const n = Number(value);
    const parsed = new Date(n < 1e11 ? n * 1000 : n);
    if (!Number.isNaN(parsed.getTime())) value = parsed.toISOString();
  }
  if (/(?:Z|[+-]\d{2}:?\d{2})$/.test(value)) {
    const date = new Date(value.replace(/([+-]\d{2})(\d{2})$/, "$1:$2"));
    if (!Number.isNaN(date.getTime())) {
      const parts = Object.fromEntries(
        new Intl.DateTimeFormat("en-CA", {
          timeZone: "Asia/Shanghai",
          year: "numeric",
          month: "2-digit",
          day: "2-digit",
          hour: "2-digit",
          minute: "2-digit",
          second: "2-digit",
          hourCycle: "h23",
        })
          .formatToParts(date)
          .map((p) => [p.type, p.value]),
      );
      return (
        parts.year +
        "-" +
        parts.month +
        "-" +
        parts.day +
        " " +
        parts.hour +
        ":" +
        parts.minute +
        ":" +
        parts.second
      );
    }
  }
  return value
    .replaceAll("/", "-")
    .replace("T", " ")
    .replace(/\.\d+$/, "");
}
function logSourceColor(source) {
  const palette = [
    "#eea253",
    "#b8a7d0",
    "#91bd9d",
    "#51b1db",
    "#9aade0",
    "#cd9cbc",
  ];
  let hash = 0;
  for (const char of source) hash = (hash * 31 + char.charCodeAt(0)) >>> 0;
  return palette[hash % palette.length];
}
function renderLogs(content) {
  const entries = parseLogEntries(content);
  return entries.length
    ? entries
        .map((entry) => {
          const time = formatLogTime(entry.time),
            source = entry.source || "MosCtl",
            parts = time.match(/^(\d{4}-\d{2}-\d{2}) (\d{2}:\d{2}:\d{2})$/),
            label = entry.label === "系统" ? "INFO" : entry.label;
          return `<div class="log-entry" role="listitem" data-level="${entry.level}"><span class="log-level">${esc(label)}</span><span class="log-time">${parts ? `<span class="log-date">${esc(parts[1])} </span>${esc(parts[2])}` : esc(time || "—")}</span><span class="log-source" style="color:${logSourceColor(source)}">${esc(source)}</span><pre class="log-message">${esc(entry.message)}</pre></div>`;
        })
        .join("")
    : '<div class="log-empty">暂无运行日志</div>';
}
async function loadLogs() {
  if (logBusy) return;
  logBusy = true;
  try {
    const data = await api("/api/logs");
    const area = $("#logContent"),
      content = data.content || "";
    if (content !== logLastContent) {
      const position = area.scrollTop,
        atBottom = position + area.clientHeight >= area.scrollHeight - 30;
      area.innerHTML = renderLogs(content);
      logLastContent = content;
      area.scrollTop = atBottom ? area.scrollHeight : position;
    }
  } catch (e) {
    logLastContent = null;
    $("#logContent").textContent = e.message;
  } finally {
    logBusy = false;
  }
}
let statsTimer = null,
  statsData = null,
  rankRoute = "all",
  rankExpanded = false;
function stopStats() {
  clearInterval(statsTimer);
  statsTimer = null;
}
const number = (v) => Number(v || 0).toLocaleString("zh-CN");
const categoryLabels = {
  local: "国内",
  remote: "国外",
  hosts: "Hosts",
  rejected: "策略拦截",
  unknown: "未识别",
  other: "历史未细分",
  mixed: "多种处理",
};
const categoryColors = {
  local: "#eea253",
  remote: "#51b1db",
  hosts: "#91bd72",
  rejected: "#b8a7d0",
  unknown: "#a3aab3",
  other: "#717c89",
  mixed: "#989da6",
};
let statsResponse = null,
  statsGeneration = 0;
async function loadStats() {
  const generation = ++statsGeneration,
    route = rankRoute,
    expanded = rankExpanded;
  try {
    const response = await api(
      "/api/stats?route=" +
        encodeURIComponent(route) +
        "&limit=" +
        (expanded ? 50 : 10),
    );
    if (
      generation !== statsGeneration ||
      $("#app").classList.contains("hidden")
    )
      return;
    statsResponse = response;
    statsData = response.data;
    const d = statsData,
      c = d.categories || {},
      total = d.local + d.remote + d.other;
    $("#totalQueries").textContent = number(total);
    $("#localQueries").textContent = number(d.local);
    $("#remoteQueries").textContent = number(d.remote);
    $("#rejectedQueries").textContent = number(c.rejected);
    $("#hostsQueries").textContent = number(c.hosts);
    $("#otherQueries").textContent = number((c.unknown || 0) + (c.other || 0));
    $("#localShare").textContent =
      (total ? (100 * d.local) / total : 0).toFixed(1) + "% · 占全部请求";
    $("#remoteShare").textContent =
      (total ? (100 * d.remote) / total : 0).toFixed(1) + "% · 占全部请求";
    const results = d.results || {},
      types = Object.entries(d.types || {})
        .filter(([, v]) => v)
        .map(([k, v]) => k + " " + number(v))
        .join(" · ");
    $("#resultSummary").textContent =
      "响应成功 " +
      number(results.success) +
      " · 上游 DNS 非成功响应 " +
      number(results.dns_error) +
      " · 解析错误 " +
      number(results.error) +
      " · 策略拒绝 " +
      number(results.rejected) +
      (results.historical
        ? " · 历史结果未知 " + number(results.historical)
        : "") +
      "。查询类型：" +
      types;
    $("#statsStatus").classList.toggle("error", !response.enabled);
    $("#statsStatus").textContent =
      (response.enabled
        ? "统计日期 " +
          d.day +
          " · 本日采集始于 " +
          new Date(d.started_at).toLocaleTimeString("zh-CN") +
          " · 刷新 " +
          new Date(response.fetched_at).toLocaleTimeString("zh-CN")
        : "当前配置未启用查询统计，请检查运行日志。") +
      (c.other ? " · 历史“其他”无法追溯拆分。" : "") +
      (d.unranked ? " · 未纳入排行 " + number(d.unranked) + " 次" : "");
    renderRanking();
    drawStatsCharts();
  } catch (e) {
    if (generation !== statsGeneration) return;
    $("#statsStatus").classList.add("error");
    $("#statsStatus").textContent =
      "刷新失败，显示的数据可能已过期：" + e.message;
  }
}
function renderRanking() {
  if (!statsResponse) return;
  const items = statsResponse.ranking || [],
    toggle = $("#rankToggle");
  toggle.classList.toggle("hidden", statsResponse.ranking_total <= 10);
  toggle.textContent = rankExpanded ? "收起" : "展开前 50 名";
  toggle.setAttribute("aria-expanded", String(rankExpanded));
  $("#statsEmpty").classList.toggle("hidden", items.length > 0);
  const max = items[0]?.count || 1;
  const openDomains = new Set(
    [...$("#domainRanking").querySelectorAll("details[open]")].map(
      (el) => el.dataset.domain,
    ),
  );
  $("#domainRanking").innerHTML = items
    .map((d, i) => {
      const color = categoryColors[d.route] || "#989da6",
        breakdown = Object.entries(d.routes || {})
          .filter(([, v]) => v)
          .map(([k, v]) => (categoryLabels[k] || k) + " " + number(v))
          .join(" · ");
      return `<tr><td class="rank-number">${i + 1}</td><td class="rank-domain">${esc(d.domain)}${
        rankRoute === "all"
          ? `<details class="rank-detail" data-domain="${encodeURIComponent(d.domain)}" ${openDomains.has(encodeURIComponent(d.domain)) ? "open" : ""}><summary>查看处理明细</summary>${esc(breakdown)}<br>查询类型：${esc(
              Object.entries(d.types || {})
                .map(([k, v]) => k + " " + number(v))
                .join(" · "),
            )}<br>结果：${esc(
              Object.entries(d.results || {})
                .map(
                  ([k, v]) =>
                    (({
                      success: "成功",
                      error: "错误",
                      dns_error: "DNS 非成功响应",
                      rejected: "策略拦截",
                      historical: "历史未知",
                    })[k] || k) +
                    " " +
                    number(v),
                )
                .join(" · "),
            )}${d.errors ? " · 解析错误 " + number(d.errors) : ""}</details>`
          : ""
      }</td><td style="color:${color}">${categoryLabels[d.route] || "未识别"}</td><td class="rank-bar-cell"><div class="rank-bar"><span style="width:${(100 * d.count) / max}%;background:${color}"></span></div></td><td>${number(d.count)}</td></tr>`;
    })
    .join("");
}
function chartCanvas(id) {
  const canvas = $("#" + id),
    rect = canvas.getBoundingClientRect(),
    scale = window.devicePixelRatio || 1;
  canvas.width = Math.max(1, Math.round(rect.width * scale));
  canvas.height = Math.max(1, Math.round(rect.height * scale));
  const ctx = canvas.getContext("2d");
  ctx.scale(scale, scale);
  return { ctx, w: rect.width, h: rect.height };
}
function traceTrend(ctx, points) {
  ctx.beginPath();
  if (!points.length) return;
  ctx.moveTo(points[0].x, points[0].y);
  if (points.length === 1) return;
  const slopes = points
      .slice(1)
      .map((p, i) => (p.y - points[i].y) / (p.x - points[i].x)),
    tangents = points.map((p, i) => {
      if (i === 0 || i === points.length - 1) return 0;
      const a = slopes[i - 1],
        b = slopes[i];
      return a * b <= 0 ? 0 : (a + b) / 2;
    });
  slopes.forEach((s, i) => {
    if (s === 0) {
      tangents[i] = tangents[i + 1] = 0;
      return;
    }
    const a = tangents[i] / s,
      b = tangents[i + 1] / s,
      length = Math.hypot(a, b);
    if (length > 3) {
      const scale = 3 / length;
      tangents[i] = scale * a * s;
      tangents[i + 1] = scale * b * s;
    }
  });
  points.slice(1).forEach((p, i) => {
    const previous = points[i],
      third = (p.x - previous.x) / 3;
    ctx.bezierCurveTo(
      previous.x + third,
      previous.y + tangents[i] * third,
      p.x - third,
      p.y - tangents[i + 1] * third,
      p.x,
      p.y,
    );
  });
}
function drawStatsCharts() {
  if (!statsData || $("#statsPage").classList.contains("hidden")) return;
  const colors = ["#eea253", "#51b1db", "#91bd72", "#b8a7d0", "#717c89"],
    values = [
      statsData.local,
      statsData.remote,
      statsData.categories?.hosts || 0,
      statsData.categories?.rejected || 0,
      (statsData.categories?.unknown || 0) + (statsData.categories?.other || 0),
    ],
    total = values.reduce((a, b) => a + b, 0);
  let { ctx, w, h } = chartCanvas("routeChart");
  const x = w / 2,
    y = h / 2,
    r = Math.min(w, h) * 0.38;
  ctx.lineWidth = 17;
  ctx.strokeStyle = "#222830";
  ctx.beginPath();
  ctx.arc(x, y, r, 0, Math.PI * 2);
  ctx.stroke();
  let start = -Math.PI / 2;
  values.forEach((v, i) => {
    if (!v || !total) return;
    const end = start + (v / total) * Math.PI * 2;
    ctx.strokeStyle = colors[i];
    ctx.beginPath();
    ctx.arc(
      x,
      y,
      r,
      start + Math.min(0.018, (end - start) / 4),
      end - Math.min(0.018, (end - start) / 4),
    );
    ctx.stroke();
    start = end;
  });
  ctx.textAlign = "center";
  ctx.fillStyle = "#f3f1ed";
  ctx.font = "700 25px -apple-system, sans-serif";
  ctx.fillText(number(total), x, y + 2);
  ctx.fillStyle = "#989da6";
  ctx.font = "12px -apple-system, sans-serif";
  ctx.fillText("今日查询", x, y + 25);
  $("#routeChart").setAttribute(
    "aria-label",
    "今日查询处理分布：" +
      ["国内", "国外", "Hosts", "策略拦截", "未细分"]
        .map((k, i) => k + " " + number(values[i]))
        .join("，"),
  );
  ctx.canvas.onpointermove = ctx.canvas.onpointerdown = (e) => {
    if (!total) return;
    const rect = e.currentTarget.getBoundingClientRect(),
      angle =
        (Math.atan2(e.clientY - rect.top - y, e.clientX - rect.left - x) +
          Math.PI / 2 +
          Math.PI * 2) %
        (Math.PI * 2);
    let cumulative = 0;
    for (let i = 0; i < values.length; i++) {
      cumulative += total ? (values[i] / total) * Math.PI * 2 : 0;
      if (angle <= cumulative) {
        $("#routeReadout").textContent =
          ["国内", "国外", "Hosts", "策略拦截", "未细分"][i] +
          " " +
          number(values[i]) +
          " 次 · " +
          ((100 * values[i]) / total).toFixed(1) +
          "%（全部请求）";
        break;
      }
    }
  };
  ({ ctx, w, h } = chartCanvas("hourChart"));
  const visibleHours = statsData.recent_hours || [];
  ctx.canvas.onpointermove = ctx.canvas.onpointerdown = (e) => {
    const rect = e.currentTarget.getBoundingClientRect(),
      i = Math.max(
        0,
        Math.min(
          23,
          Math.round(((e.clientX - rect.left - 42) / (rect.width - 60)) * 23),
        ),
      ),
      v = visibleHours[i];
    if (v)
      $("#hourReadout").textContent =
        new Date(v.time).toLocaleString("zh-CN", {
          month: "2-digit",
          day: "2-digit",
          hour: "2-digit",
          minute: "2-digit",
        }) +
        " · 国内 " +
        number(v.local) +
        " · 国外 " +
        number(v.remote) +
        " · 其他处理 " +
        number(v.other) +
        (i === 23 ? "（本小时未结束）" : "");
  };
  const series = ["local", "remote"];
  const hasOther = visibleHours.some((v) => v.other > 0);
  if (hasOther) series.push("other");
  $("#hourOtherLegend").classList.toggle("hidden", !hasOther);
  const peak = Math.max(
      1,
      ...visibleHours.flatMap((v) => series.map((key) => v[key] || 0)),
    ),
    magnitude = 10 ** Math.floor(Math.log10(peak)),
    unit = peak / magnitude,
    tick = Math.max(
      1,
      Math.ceil(
        ((unit <= 1 ? 1 : unit <= 2 ? 2 : unit <= 5 ? 5 : 10) * magnitude) / 4,
      ),
    ),
    max = tick * 4,
    left = 42,
    right = w - 18,
    bottom = h - 24,
    top = 10,
    plotHeight = bottom - top,
    step = (right - left) / 23;
  ctx.font = "10px -apple-system, sans-serif";
  ctx.textAlign = "right";
  ctx.lineWidth = 1;
  for (let i = 0; i <= 4; i++) {
    const y = bottom - (i / 4) * plotHeight;
    ctx.strokeStyle = "#232932";
    ctx.beginPath();
    ctx.moveTo(left, y);
    ctx.lineTo(right, y);
    ctx.stroke();
    ctx.fillStyle = "#838c99";
    ctx.fillText(number(Math.round((max * i) / 4)), left - 7, y + 3);
  }
  series.forEach((key, j) => {
    const points = visibleHours.map((hour, i) => ({
      x: left + i * step,
      y: bottom - ((hour[key] || 0) / max) * plotHeight,
    }));
    if (!points.length) return;
    const trace = () => traceTrend(ctx, points);
    trace();
    ctx.lineTo(points[points.length - 1].x, bottom);
    ctx.lineTo(points[0].x, bottom);
    ctx.closePath();
    const gradient = ctx.createLinearGradient(0, top, 0, bottom);
    gradient.addColorStop(0, ["#eea253", "#51b1db", "#717c89"][j] + "26");
    gradient.addColorStop(1, ["#eea253", "#51b1db", "#717c89"][j] + "00");
    ctx.fillStyle = gradient;
    ctx.fill();
    trace();
    ctx.strokeStyle = ["#eea253", "#51b1db", "#717c89"][j];
    ctx.lineWidth = 2;
    ctx.lineJoin = "round";
    ctx.lineCap = "round";
    ctx.stroke();
  });
  ctx.textAlign = "center";
  ctx.fillStyle = "#838c99";
  [0, 6, 12, 18, 23].forEach((i) =>
    ctx.fillText(
      String(visibleHours[i]?.hour ?? i).padStart(2, "0") + ":00",
      left + i * step,
      h - 5,
    ),
  );
}
document.querySelectorAll("[data-route]").forEach(
  (b) =>
    (b.onclick = () => {
      rankRoute = b.dataset.route;
      rankExpanded = false;
      document.querySelectorAll("[data-route]").forEach((x) => {
        x.classList.toggle("active", x === b);
        x.setAttribute("aria-pressed", String(x === b));
      });
      loadStats();
    }),
);
$("#rankToggle").onclick = () => {
  rankExpanded = !rankExpanded;
  loadStats();
};
window.addEventListener("resize", drawStatsCharts);
function pageFromURL() {
  const name = location.pathname.slice(1);
  return ["rules", "stats", "logs", "tests", "settings"].includes(name)
    ? name
    : "stats";
}
function selectPage(page, push = true) {
  for (const name of ["rules", "stats", "logs", "tests", "settings"])
    $("#" + name + "Page").classList.toggle("hidden", name !== page);
  document.querySelectorAll("[data-page]").forEach((a) => {
    if (a.dataset.page === page) a.setAttribute("aria-current", "page");
    else a.removeAttribute("aria-current");
  });
  if (push && location.pathname !== "/" + page)
    history.pushState({}, "", "/" + page);
  if (page === "rules" && !rulesLoaded)
    loadRules().catch((e) => {
      feedback(e.message, "error");
      toast(e.message);
    });
  if (page === "logs") {
    loadLogs();
    scheduleLogs();
  } else stopLogs();
  stopStats();
  if (page === "stats") {
    loadStats();
    statsTimer = setInterval(() => {
      if (!document.hidden) loadStats();
    }, 5000);
  }
  clearTimeout(kernelTimer);
  if (page === "settings") {
    if (!remoteLoaded) loadRemote();
    loadKernel();
  }
}
document.querySelectorAll("[data-page]").forEach(
  (a) =>
    (a.onclick = (e) => {
      if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
      e.preventDefault();
      selectPage(a.dataset.page);
    }),
);
window.addEventListener("popstate", () => selectPage(pageFromURL(), false));
let kernelTimer = null,
  kernelLoading = false,
  kernelData = null;
function renderKernel(data) {
  kernelData = data;
  $("#kernelCurrent").textContent = data.current || "未知";
  $("#kernelLatest").textContent = data.latest || "";
  $("#kernelLatestWrap").classList.toggle("hidden", !data.latest);
  $("#checkKernel").disabled = data.busy || !data.supported;
  $("#checkKernel").textContent = data.busy ? "正在处理…" : "检查更新";
  $("#updateKernel").classList.toggle("hidden", !data.update_available);
  $("#updateKernel").disabled = data.busy || !data.supported;
  $("#updateKernel").textContent = "更新到 " + (data.latest || "新版本");
  $("#rollbackKernel").classList.toggle("hidden", !data.backup);
  $("#rollbackKernel").disabled = data.busy || !data.supported;
  $("#rollbackKernel").textContent = "回滚到 " + (data.backup || "上一版本");
  const feedback = $("#kernelFeedback");
  feedback.className =
    "settings-feedback" +
    (data.error ? " error" : data.phase === "done" ? " success" : "");
  feedback.textContent = data.message || "";
}
async function loadKernel() {
  if (kernelLoading) return;
  kernelLoading = true;
  clearTimeout(kernelTimer);
  try {
    const data = await api("/api/settings/kernel");
    renderKernel(data);
    if (
      data.busy &&
      !$("#settingsPage").classList.contains("hidden") &&
      !$("#app").classList.contains("hidden")
    )
      kernelTimer = setTimeout(loadKernel, 1000);
  } catch (e) {
    $("#kernelFeedback").className = "settings-feedback error";
    $("#kernelFeedback").textContent = e.message;
    if (
      !$("#settingsPage").classList.contains("hidden") &&
      !$("#app").classList.contains("hidden")
    )
      kernelTimer = setTimeout(loadKernel, 3000);
  } finally {
    kernelLoading = false;
  }
}
async function kernelAction(action) {
  if (kernelData?.busy) return;
  clearTimeout(kernelTimer);
  if (kernelData)
    renderKernel({
      ...kernelData,
      busy: true,
      message: action === "check" ? "正在检查更新…" : "正在处理…",
      error: "",
    });
  try {
    renderKernel(
      await api("/api/settings/kernel/" + action, {
        method: "POST",
        body: "{}",
      }),
    );
    kernelTimer = setTimeout(loadKernel, 500);
  } catch (e) {
    await loadKernel();
    $("#kernelFeedback").className = "settings-feedback error";
    $("#kernelFeedback").textContent = e.message;
  }
}
$("#checkKernel").onclick = () => kernelAction("check");
$("#updateKernel").onclick = () => kernelAction("update");
$("#rollbackKernel").onclick = () => kernelAction("rollback");
let remoteOriginal = "",
  remoteLoaded = false,
  remoteBusy = false;
function updateRemoteDirty() {
  $("#remoteAddress").disabled = remoteBusy;
  $("#saveRemote").disabled =
    remoteBusy ||
    !remoteLoaded ||
    $("#remoteAddress").value.trim() === remoteOriginal;
}
async function loadRemote() {
  remoteBusy = true;
  updateRemoteDirty();
  try {
    const data = await api("/api/settings/remote");
    remoteOriginal = data.remote;
    $("#remoteAddress").value = data.remote;
    remoteLoaded = true;
  } catch (e) {
    $("#remoteFeedback").textContent = e.message;
  } finally {
    remoteBusy = false;
    updateRemoteDirty();
  }
}
$("#remoteAddress").oninput = updateRemoteDirty;
$("#remoteForm").onsubmit = async (e) => {
  e.preventDefault();
  remoteBusy = true;
  updateRemoteDirty();
  const feedback = $("#remoteFeedback");
  feedback.className = "settings-feedback";
  feedback.textContent = "正在保存…";
  try {
    const data = await api("/api/settings/remote", {
      method: "PUT",
      body: JSON.stringify({ remote: $("#remoteAddress").value }),
    });
    remoteOriginal = data.remote;
    $("#remoteAddress").value = data.remote;
    feedback.classList.add("success");
    feedback.textContent = data.message;
  } catch (err) {
    feedback.classList.add("error");
    feedback.textContent = err.message;
  } finally {
    remoteBusy = false;
    updateRemoteDirty();
  }
};
async function testDNS(target) {
  const button =
    target === "custom"
      ? $("#testCustom")
      : target === "baidu"
        ? $("#testBaidu")
        : $("#testGoogle");
  const result = $("#" + target + "Result");
  result.classList.remove("hidden");
  button.disabled = true;
  result.textContent = "正在解析…";
  try {
    const data = await api("/api/test/" + target, {
      method: "POST",
      body: JSON.stringify(
        target === "custom"
          ? { domain: $("#customDomain").value, type: $("#queryType").value }
          : {},
      ),
    });
    result.innerHTML = `${esc(data.domain)} · ${esc(data.type || "A")} · <span class="${data.ok ? "success" : "error"}">${data.actual_result === "rejected" ? "策略拦截" : data.ok ? "响应成功" : "解析失败"}</span> · ${data.elapsed_ms} ms · ${esc(data.rcode || "无响应")}<br>${esc((data.records || []).join("\n") || data.error || "没有该类型记录")}<br>${data.actual_route ? "实际处理：" + esc(categoryLabels[data.actual_route] || data.actual_route) : "执行日志尚未可用"}<br>匹配参考：${esc(data.rule_hint || "")}`;
  } catch (e) {
    result.textContent = e.message;
  } finally {
    button.disabled = false;
  }
}
$("#customTestForm").onsubmit = (e) => {
  e.preventDefault();
  testDNS("custom");
};
$("#refreshLogs").onclick = loadLogs;
$("#autoLogs").onchange = scheduleLogs;
$("#testBaidu").onclick = () => testDNS("baidu");
$("#testGoogle").onclick = () => testDNS("google");
function esc(v) {
  const d = document.createElement("div");
  d.textContent = v;
  return d.innerHTML;
}
function renderRules() {
  const html = state.rules
    .map(
      (r) =>
        `<button class="rule-card ${state.current?.id === r.id ? "active" : ""}" data-id="${r.id}"><span class="rule-title-row"><span class="rule-name">${esc(r.name)}</span><span class="rule-count">${r.entries} 条</span></span><span class="rule-description">${esc(r.description)}</span><span class="rule-file">${esc(r.filename)}</span></button>`,
    )
    .join("");
  $("#ruleList").innerHTML = html;
  $("#mobilePicker").innerHTML = state.rules
    .map(
      (r) =>
        `<option value="${r.id}" ${state.current?.id === r.id ? "selected" : ""}>${esc(r.name)} · ${r.entries} 条</option>`,
    )
    .join("");
  document
    .querySelectorAll(".rule-card")
    .forEach((b) => (b.onclick = () => selectRule(b.dataset.id)));
}
function entries(v) {
  return v.split("\n").filter((x) => {
    x = x.trim();
    return x && !x.startsWith("#");
  }).length;
}
function normalize(v) {
  const lines = v
    .replaceAll("\r\n", "\n")
    .replaceAll("\r", "\n")
    .split("\n")
    .map((x) => x.trim());
  const result = lines.join("\n").trim();
  return result ? result + "\n" : "";
}
function updateDirty() {
  const dirty = $("#content").value !== state.original;
  $("#dirtyState").textContent = dirty ? "有未保存的更改" : "";
  $("#apply").disabled = !dirty || state.loading;
}
let rulesLoaded = false;
async function loadRules() {
  const data = await api("/api/rules");
  state.rules = data.rules;
  rulesLoaded = true;
  renderRules();
  if (!state.current && state.rules.length) await selectRule(state.rules[0].id);
}
async function selectRule(id) {
  if (state.loading) return;
  if (state.current?.id === id) return;
  if (
    $("#content").value !== state.original &&
    !confirm("当前更改尚未保存，确定切换规则集吗？")
  ) {
    $("#mobilePicker").value = state.current.id;
    return;
  }
  setLoading(true);
  try {
    const data = await api("/api/rules/" + id);
    state.current = data.rule;
    state.original = data.content;
    $("#content").value = data.content;
    $("#search").value = "";
    $("#matchHint").textContent = "";
    renderRules();
    updateDirty();
  } catch (e) {
    feedback(e.message, "error");
    toast(e.message);
  } finally {
    setLoading(false);
  }
}
function setLoading(v) {
  state.loading = v;
  $("#content").disabled = v;
  updateDirty();
}
function feedback(msg, type = "") {
  const el = $("#feedback");
  el.textContent = msg;
  el.className = "feedback " + type;
}
function toast(msg) {
  const el = $("#toast");
  el.textContent = msg;
  el.classList.add("show");
  clearTimeout(el._t);
  el._t = setTimeout(() => el.classList.remove("show"), 2600);
}
async function save(apply) {
  if (!state.current) return;
  setLoading(true);
  feedback(apply ? "正在保存并应用…" : "正在保存…");
  try {
    const data = await api("/api/rules/" + state.current.id, {
      method: "PUT",
      body: JSON.stringify({ content: $("#content").value, apply }),
    });
    state.original = data.content ?? normalize($("#content").value);
    $("#content").value = state.original;
    const r = state.rules.find((x) => x.id === state.current.id);
    if (r) r.entries = data.entries;
    renderRules();
    feedback(data.message, "success");
    toast(data.message);
    updateDirty();
  } catch (e) {
    feedback(e.message, "error");
    toast(e.message);
  } finally {
    setLoading(false);
  }
}
$("#loginForm").onsubmit = async (e) => {
  e.preventDefault();
  const button = e.submitter;
  button.disabled = true;
  $("#loginError").textContent = "";
  try {
    await api("/api/login", {
      method: "POST",
      body: JSON.stringify({
        username: $("#username").value,
        password: $("#password").value,
      }),
    });
    $("#password").value = "";
    showApp();
  } catch (err) {
    $("#loginError").textContent = err.message;
  } finally {
    button.disabled = false;
  }
};
function closeUserMenu() {
  $("#userMenu").classList.add("hidden");
  $("#userMenuToggle").setAttribute("aria-expanded", "false");
}
$("#userMenuToggle").onclick = () => {
  const open = $("#userMenu").classList.toggle("hidden") === false;
  $("#userMenuToggle").setAttribute("aria-expanded", String(open));
  if (open) $("#restartDNS").focus();
};
document.addEventListener("click", (e) => {
  if (!e.target.closest(".user-menu")) closeUserMenu();
});
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && !$("#userMenu").classList.contains("hidden")) {
    closeUserMenu();
    $("#userMenuToggle").focus();
  }
});
$("#restartDNS").onclick = async () => {
  if (!confirm("重启 MosDNS 会短暂中断 DNS 解析，确定继续？")) return;
  const button = $("#restartDNS");
  button.disabled = true;
  button.textContent = "正在重启…";
  closeUserMenu();
  try {
    const result = await api("/api/restart", { method: "POST", body: "{}" });
    toast(result.message);
  } catch (e) {
    toast(e.message);
  } finally {
    button.disabled = false;
    button.textContent = "重启 MosDNS";
  }
};
$("#logout").onclick = async () => {
  try {
    await api("/api/logout", { method: "POST", body: "{}" });
  } catch {}
  state.current = null;
  state.original = "";
  remoteLoaded = false;
  rulesLoaded = false;
  showLogin();
};
$("#content").addEventListener("input", updateDirty);
$("#apply").onclick = () => save(true);
$("#mobilePicker").onchange = (e) => selectRule(e.target.value);
$("#search").addEventListener("input", (e) => {
  const q = e.target.value.trim().toLowerCase();
  if (!q) {
    $("#matchHint").textContent = "";
    return;
  }
  const n = $("#content")
    .value.split("\n")
    .filter((x) => x.toLowerCase().includes(q)).length;
  $("#matchHint").textContent = `${n} 个匹配`;
  if (n) {
    const area = $("#content"),
      i = area.value.toLowerCase().indexOf(q);
    area.setSelectionRange(i, i + q.length);
  }
});
$("#search").addEventListener("keydown", (e) => {
  if (e.key === "Enter") {
    e.preventDefault();
    $("#content").focus();
  }
});
window.addEventListener("beforeunload", (e) => {
  if (
    $("#content").value !== state.original ||
    (remoteLoaded && $("#remoteAddress").value.trim() !== remoteOriginal)
  ) {
    e.preventDefault();
    e.returnValue = "";
  }
});
window.addEventListener("online", updateConnection);
window.addEventListener("offline", updateConnection);
updateConnection();
if ("serviceWorker" in navigator)
  window.addEventListener("load", () =>
    navigator.serviceWorker.register("/sw.js").catch(() => {}),
  );
(async () => {
  try {
    await api("/api/session");
    showApp();
  } catch {
    showLogin();
  }
})();
