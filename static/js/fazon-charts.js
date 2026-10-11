// Small hand-rolled SVG charts (no libraries) for Track: stacked columns, horizontal bars and
// multi-line charts, with axes, legend, a hover/tap/keyboard tooltip (+ <title> fallbacks),
// redrawn to the container's width. Colours are CSS colour strings — normally
// "var(--tk-…)" from css/track.css — applied through style, so theme switches need no redraw.
//
//   FazonCharts.columns(box, {x, xLabel, xTitle, series:[{label, color, values}], fmt, ariaLabel, height})
//   FazonCharts.hbars(box, {rows:[{label, value, href, me, note}], color, fmt, ariaLabel})
//   FazonCharts.lines(box, {x, xLabel, xTitle, series:[{label, color, values, notes?}], yMax, fmt, ariaLabel, height,
//                           estimateGaps?, estimateColor?, estimateLabel?})
//     notes: optional per-point strings appended to the series label in the tooltip.
//     minStep: px per point below which the plot scrolls sideways instead of squeezing (default 6).
//     estimateGaps (opt-in): null runs between two real points of a series are bridged by a dashed
//     segment in estimateColor (default var(--fc-est)) and read in the tooltip as "≈ value (estimate)",
//     linearly interpolated; no estimate before a series' first or after its last real point.
// each returns {update(opts), destroy()}. All labels are set with textContent (untrusted data).
(function () {
  "use strict";
  var NS = "http://www.w3.org/2000/svg";

  function s(tag, attrs, text) {
    var n = document.createElementNS(NS, tag);
    if (attrs) for (var k in attrs) if (attrs[k] != null) n.setAttribute(k, attrs[k]);
    if (text != null) n.textContent = text;
    return n;
  }
  function h(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text != null) n.textContent = text;
    return n;
  }
  function num(v) { return typeof v === "number" && isFinite(v); }
  function fmtDefault(v) { return num(v) ? (Math.round(v * 100) / 100).toLocaleString("en", { maximumFractionDigits: 2 }) : "—"; }

  // 0..top in "nice" steps (1 / 2 / 2.5 / 5 × 10^n)
  function niceTicks(max, count) {
    if (!(max > 0)) max = 1;
    var raw = max / count, mag = Math.pow(10, Math.floor(Math.log10(raw))), f = raw / mag;
    var step = (f <= 1 ? 1 : f <= 2 ? 2 : f <= 2.5 ? 2.5 : f <= 5 ? 5 : 10) * mag;
    var top = Math.ceil(max / step - 1e-9) * step, out = [];
    for (var v = 0; v <= top + step / 2; v += step) out.push(Math.round(v * 1e6) / 1e6);
    return out;
  }

  // a bar that grows from y0 (baseline) to y1 with a 4px rounded data-end (vertical columns)
  function colPath(x, w, yTop, yBot, round) {
    var r = round ? Math.min(4, w / 2, yBot - yTop) : 0;
    return "M" + x + "," + yBot + "V" + (yTop + r) + (r ? "Q" + x + "," + yTop + " " + (x + r) + "," + yTop : "") +
      "H" + (x + w - r) + (r ? "Q" + (x + w) + "," + yTop + " " + (x + w) + "," + (yTop + r) : "") + "V" + yBot + "Z";
  }
  // horizontal bar from x0 rightwards, rounded at the right end only
  function rowPath(x0, y, len, th) {
    var r = Math.min(4, th / 2, len);
    return "M" + x0 + "," + y + "H" + (x0 + len - r) + (r ? "Q" + (x0 + len) + "," + y + " " + (x0 + len) + "," + (y + r) : "") +
      "V" + (y + th - r) + (r ? "Q" + (x0 + len) + "," + (y + th) + " " + (x0 + len - r) + "," + (y + th) : "") + "H" + x0 + "Z";
  }

  // ---- generic frame: legend + horizontally scrollable plot + one tooltip ----
  function frame(box, opts, draw) {
    box.textContent = "";
    box.classList.add("fc");
    var st = { box: box, opts: opts, legend: h("div", "fc-legend"), scroll: h("div", "fc-scroll"), tip: h("div", "fc-tip") };
    st.tip.hidden = true;
    st.tip.setAttribute("aria-hidden", "true");
    box.append(st.legend, st.scroll, st.tip);
    var w = -1;
    function render() { w = st.scroll.clientWidth; hideTip(st); st.scroll.textContent = ""; st.legend.textContent = ""; draw(st); }
    var ro = typeof ResizeObserver === "function" ? new ResizeObserver(function () { if (st.scroll.clientWidth !== w) render(); }) : null;
    if (ro) ro.observe(st.scroll);
    render();
    return {
      update: function (o) { st.opts = o; render(); },
      destroy: function () { if (ro) ro.disconnect(); if (st.outside) document.removeEventListener("pointerdown", st.outside); box.textContent = ""; }
    };
  }
  function legend(st, series, kind) {
    if (series.length < 2) return;
    series.forEach(function (se) {
      var sw = h("span", "fc-sw" + (kind === "line" ? " line" : ""));
      sw.style.background = se.color;
      var li = h("span", "fc-li");
      li.append(sw, document.createTextNode(se.label));
      st.legend.appendChild(li);
    });
  }
  function hideTip(st) { st.tip.hidden = true; }
  // rows: [{color, value, label}] — value leads (bold), label follows
  function showTip(st, cx, cy, title, rows, foot) {
    var tip = st.tip;
    tip.textContent = "";
    if (title) tip.appendChild(h("div", "fc-tip-t", title));
    rows.forEach(function (r) {
      var row = h("div", "fc-tip-r" + (r.cls ? " " + r.cls : "")), k = h("span", "fc-tip-k");
      if (r.color) k.style.background = r.color; else k.style.visibility = "hidden";
      row.append(k, h("b", null, r.value), h("span", null, r.label || ""));
      tip.appendChild(row);
    });
    if (foot) tip.appendChild(h("div", "fc-tip-f", foot));
    tip.hidden = false;
    var br = st.box.getBoundingClientRect(), x = cx - br.left, y = cy - br.top;
    var tw = tip.offsetWidth, th = tip.offsetHeight, bw = st.box.clientWidth;
    var left = x + 14;
    if (left + tw > bw) left = x - 14 - tw;
    if (left < 0) left = Math.max(0, Math.min(bw - tw, x - tw / 2));
    var top = y - th - 12;
    if (top < -8) top = y + 18;
    tip.style.left = Math.round(left) + "px";
    tip.style.top = Math.round(top) + "px";
  }
  function xLabelsEvery(n, pw, narrow) { return Math.max(1, Math.ceil(n / Math.max(1, Math.floor(pw / (narrow ? 58 : 72))))); }

  // index-based pointer + keyboard interaction shared by columns and lines
  function bindIndex(st, svg, n, idxAt, onIdx) {
    var cur = -1;
    function set(i, cx, cy) { cur = i; onIdx(i, cx, cy); }
    function clear() { cur = -1; onIdx(-1); }
    function fromEvent(e) {
      var r = svg.getBoundingClientRect();
      var i = idxAt(e.clientX - r.left);
      if (i < 0) { clear(); return; }
      set(i, e.clientX, e.clientY);
    }
    svg.addEventListener("pointermove", fromEvent);
    svg.addEventListener("pointerdown", fromEvent);
    svg.addEventListener("pointerleave", function (e) { if (e.pointerType === "mouse") clear(); });
    svg.addEventListener("blur", clear);
    svg.addEventListener("keydown", function (e) {
      var k = e.key, i = cur;
      if (k === "ArrowRight") i = Math.min(n - 1, cur < 0 ? 0 : cur + 1);
      else if (k === "ArrowLeft") i = Math.max(0, cur < 0 ? n - 1 : cur - 1);
      else if (k === "Home") i = 0;
      else if (k === "End") i = n - 1;
      else if (k === "Escape") { clear(); return; }
      else return;
      e.preventDefault();
      set(i, null, null);
    });
    svg.addEventListener("focus", function () { if (cur < 0 && n) set(n - 1, null, null); });
    // one outside-tap listener per chart, replaced on every redraw (resize / update) rather than piling up
    if (st.outside) document.removeEventListener("pointerdown", st.outside);
    st.outside = function (e) { if (cur >= 0 && !st.box.contains(e.target)) clear(); };
    document.addEventListener("pointerdown", st.outside);
  }

  // ---- stacked columns (one column per x; one stack segment per series) ----
  function drawColumns(st) {
    var o = st.opts, xs = o.x || [], n = xs.length, ser = (o.series || []).filter(Boolean);
    var fmt = o.fmt || fmtDefault, xLabel = o.xLabel || String, xTitle = o.xTitle || xLabel;
    legend(st, ser, "bar");
    if (!n) { st.scroll.appendChild(h("div", "fc-empty", "No data")); return; }
    var W0 = Math.max(240, st.scroll.clientWidth || 600), narrow = W0 < 480;
    var H = o.height || (narrow ? 200 : 240), pad = { l: 42, r: 8, t: 10, b: 26 };
    var W = Math.max(W0, pad.l + pad.r + n * (narrow ? 4 : 5)), pw = W - pad.l - pad.r, ph = H - pad.t - pad.b;
    var totals = xs.map(function (_, i) { return ser.reduce(function (a, se) { var v = se.values[i]; return a + (num(v) && v > 0 ? v : 0); }, 0); });
    var ticks = niceTicks(Math.max.apply(null, totals), narrow ? 4 : 5), top = ticks[ticks.length - 1];
    var y = function (v) { return pad.t + ph * (1 - v / top); };
    var band = pw / n, bw = Math.max(1, Math.min(24, band * 0.72));
    var svg = s("svg", { class: "fc-svg", width: W, height: H, viewBox: "0 0 " + W + " " + H, role: "img", tabindex: "0",
      "aria-label": (o.ariaLabel || "Chart") + " (use the arrow keys to read values)" });
    ticks.forEach(function (t) {
      svg.appendChild(s("line", { class: t === 0 ? "fc-base" : "fc-grid", x1: pad.l, x2: W - pad.r, y1: Math.round(y(t)) + 0.5, y2: Math.round(y(t)) + 0.5 }));
      svg.appendChild(s("text", { class: "fc-tick", x: pad.l - 6, y: y(t) + 4, "text-anchor": "end" }, fmt(t, true)));
    });
    var cols = [];
    xs.forEach(function (xv, i) {
      var g = s("g", { class: "fc-col" }), x0 = pad.l + band * i + (band - bw) / 2, cum = 0;
      var segs = ser.map(function (se) { return { se: se, v: se.values[i] }; }).filter(function (d) { return num(d.v) && d.v > 0; });
      segs.forEach(function (d, k) {
        var yb = y(cum), yt = y(cum + d.v); cum += d.v;
        if (k > 0 && yb - yt > 3) yb -= 2;                    // 2px surface gap between stacked segments
        if (yb - yt < 0.5) yb = yt + 0.5;
        g.appendChild(s("path", { d: colPath(x0, bw, yt, yb, k === segs.length - 1), style: "fill:" + d.se.color }));
      });
      var t = xTitle(xv, i) + "\n" + segs.map(function (d) { return d.se.label + ": " + fmt(d.v); }).join("\n") + (segs.length > 1 ? "\nTotal: " + fmt(totals[i]) : "");
      g.appendChild(s("title", null, t));
      g.appendChild(s("rect", { class: "fc-hit", x: pad.l + band * i, y: pad.t, width: band, height: ph }));
      svg.appendChild(g);
      cols.push(g);
    });
    var every = xLabelsEvery(n, pw, narrow);
    xs.forEach(function (xv, i) {
      if (i % every) return;
      svg.appendChild(s("text", { class: "fc-tick", x: pad.l + band * (i + 0.5), y: H - 8, "text-anchor": n === 1 ? "middle" : i === 0 && band < 50 ? "start" : "middle" }, xLabel(xv, i)));
    });
    st.scroll.appendChild(svg);
    bindIndex(st, svg, n, function (px) { return px < pad.l || px > W - pad.r ? -1 : Math.min(n - 1, Math.floor((px - pad.l) / band)); },
      function (i, cx, cy) {
        svg.classList.toggle("fc-dim", i >= 0);
        cols.forEach(function (g, k) { g.classList.toggle("on", k === i); });
        if (i < 0) { hideTip(st); return; }
        if (cx == null) { var r = cols[i].getBoundingClientRect(); cx = r.left + r.width / 2; cy = Math.max(r.top + 30, svg.getBoundingClientRect().top + 40); }
        var rows = ser.slice().reverse().filter(function (se) { return num(se.values[i]) && se.values[i] > 0; })
          .map(function (se) { return { color: se.color, value: fmt(se.values[i]), label: se.label }; });
        if (!rows.length) rows = [{ value: fmt(0), label: ser.length === 1 ? ser[0].label : "" }];
        showTip(st, cx, cy, xTitle(xs[i], i), rows, ser.length > 1 && rows.length > 1 ? "Total " + fmt(totals[i]) : null);
      });
  }

  // ---- horizontal bars (ranking): label | bar | value at the tip ----
  function drawHbars(st) {
    var o = st.opts, rows = o.rows || [], n = rows.length, fmt = o.fmt || fmtDefault;
    if (!n) { st.scroll.appendChild(h("div", "fc-empty", "No data")); return; }
    var W = Math.max(240, st.scroll.clientWidth || 600), narrow = W < 480;
    var lw = narrow ? Math.min(104, Math.round(W * 0.32)) : 160, vw = narrow ? 64 : 80, rowH = 26, th = 14;
    var pad = { t: 4, b: 4 }, H = pad.t + pad.b + n * rowH, x0 = lw + 8, plen = Math.max(20, W - x0 - vw);
    var max = Math.max.apply(null, rows.map(function (r) { return num(r.value) ? r.value : 0; }).concat([o.max || 0])) || 1;
    var chars = Math.max(4, Math.floor(lw / 7.3));
    var svg = s("svg", { class: "fc-svg", width: W, height: H, viewBox: "0 0 " + W + " " + H, role: "img", "aria-label": o.ariaLabel || "Bar chart" });
    svg.appendChild(s("line", { class: "fc-base", x1: x0 + 0.5, x2: x0 + 0.5, y1: pad.t, y2: H - pad.b }));
    rows.forEach(function (r, i) {
      var yc = pad.t + i * rowH + rowH / 2, name = String(r.label == null ? "" : r.label);
      var shown = Array.from(name).length > chars ? Array.from(name).slice(0, chars - 1).join("") + "…" : name;
      var lbl = s("text", { class: "fc-lbl" + (r.me ? " me" : ""), x: lw, y: yc + 4, "text-anchor": "end" }, shown);
      if (r.href) { var a = s("a", { href: r.href }); a.appendChild(lbl); a.appendChild(s("title", null, name)); svg.appendChild(a); }
      else svg.appendChild(lbl);
      var len = num(r.value) && r.value > 0 ? Math.max(1, plen * r.value / max) : 0;
      var g = s("g", { class: "fc-bar", tabindex: "0", role: "img", "aria-label": name + ": " + fmt(r.value) });
      if (len) g.appendChild(s("path", { d: rowPath(x0 + 1, yc - th / 2, len, th), style: "fill:" + (r.color || o.color || "var(--fz-accent)") }));
      g.appendChild(s("title", null, name + ": " + fmt(r.value) + (r.note ? " · " + r.note : "")));
      g.appendChild(s("rect", { class: "fc-hit", x: x0, y: yc - rowH / 2, width: W - x0, height: rowH }));
      svg.appendChild(g);
      svg.appendChild(s("text", { class: "fc-val", x: x0 + 1 + len + 6, y: yc + 4 }, fmt(r.value)));
      function show(e) {
        svg.querySelectorAll(".fc-bar.on").forEach(function (b) { b.classList.remove("on"); });
        g.classList.add("on");
        var cx, cy;
        if (e && e.clientX != null && e.type !== "focus") { cx = e.clientX; cy = e.clientY; }
        else { var b = g.getBoundingClientRect(); cx = b.left + Math.min(len, 120); cy = b.top + rowH / 2; }
        showTip(st, cx, cy, name, [{ color: r.color || o.color || "var(--fz-accent)", value: fmt(r.value), label: o.valueLabel || "" }], r.note || null);
      }
      function hide() { g.classList.remove("on"); hideTip(st); }
      g.addEventListener("pointermove", show);
      g.addEventListener("pointerdown", show);
      g.addEventListener("pointerleave", function (e) { if (e.pointerType === "mouse") hide(); });
      g.addEventListener("focus", show);
      g.addEventListener("blur", hide);
    });
    st.scroll.appendChild(svg);
  }

  // ---- multi-line (nulls = gaps), crosshair + one tooltip listing every series ----
  function drawLines(st) {
    var o = st.opts, xs = o.x || [], n = xs.length, ser = (o.series || []).filter(Boolean);
    var fmt = o.fmt || fmtDefault, xLabel = o.xLabel || String, xTitle = o.xTitle || xLabel;
    // opt-in estimates: est[k][i] = linear interpolation across a null run between two real points
    var estC = o.estimateColor || "var(--fc-est)", estDash = "repeating-linear-gradient(90deg," + estC + " 0 4px,transparent 4px 7px)";
    var est = ser.map(function (se) {
      var e = [], prev = -1;
      if (!o.estimateGaps) return e;
      se.values.forEach(function (v, i) {
        if (!num(v)) return;
        if (prev >= 0 && i - prev > 1) for (var j = prev + 1; j < i; j++) e[j] = se.values[prev] + (v - se.values[prev]) * (j - prev) / (i - prev);
        prev = i;
      });
      return e;
    });
    var hasEst = est.some(function (e) { return e.length > 0; });
    legend(st, ser, "line");
    if (hasEst) {
      // a lone series gets a legend entry too once it sits beside its estimates
      var items = ser.length < 2 ? ser.map(function (se) { return { label: se.label, bg: se.color }; }) : [];
      items.push({ label: o.estimateLabel || "Estimate (missing data)", bg: estDash, est: true });
      items.forEach(function (it) {
        var sw = h("span", "fc-sw line" + (it.est ? " est" : "")), li = h("span", "fc-li");
        sw.style.background = it.bg;
        li.append(sw, document.createTextNode(it.label));
        st.legend.appendChild(li);
      });
    }
    if (!n) { st.scroll.appendChild(h("div", "fc-empty", "No data")); return; }
    var W0 = Math.max(240, st.scroll.clientWidth || 600), narrow = W0 < 480;
    var H = o.height || (narrow ? 200 : 240), pad = { l: 42, r: 12, t: 10, b: 26 };
    var W = Math.max(W0, pad.l + pad.r + n * (o.minStep || 6)), pw = W - pad.l - pad.r, ph = H - pad.t - pad.b;
    var maxV = 0;
    ser.forEach(function (se) { se.values.forEach(function (v) { if (num(v) && v > maxV) maxV = v; }); });
    var ticks = o.yMax != null ? niceTicks(o.yMax, 4) : niceTicks(maxV, narrow ? 4 : 5), top = ticks[ticks.length - 1];
    var x = function (i) { return pad.l + (n === 1 ? pw / 2 : i * pw / (n - 1)); };
    var y = function (v) { return pad.t + ph * (1 - v / top); };
    var svg = s("svg", { class: "fc-svg", width: W, height: H, viewBox: "0 0 " + W + " " + H, role: "img", tabindex: "0",
      "aria-label": (o.ariaLabel || "Line chart") + (hasEst ? "; dashed segments estimate missing data" : "") + " (use the arrow keys to read values)" });
    ticks.forEach(function (t) {
      svg.appendChild(s("line", { class: t === 0 ? "fc-base" : "fc-grid", x1: pad.l, x2: W - pad.r, y1: Math.round(y(t)) + 0.5, y2: Math.round(y(t)) + 0.5 }));
      svg.appendChild(s("text", { class: "fc-tick", x: pad.l - 6, y: y(t) + 4, "text-anchor": "end" }, fmt(t, true)));
    });
    var every = xLabelsEvery(n, pw, narrow), tw = function (i) { return Array.from(String(xLabel(xs[i], i))).length * 6.8; };   // ~11px mono
    var right = -Infinity, skip0 = every < n - 1 && x(0) + tw(0) + 8 > x(every) - tw(every) / 2;
    xs.forEach(function (xv, i) {
      if (i % every && i !== n - 1 || (i === 0 && skip0)) return;   // a crowded first label gives way, keeping the rest evenly spaced
      // first label start-anchored, last end-anchored, the rest centred; a label that would run into
      // the previous one is skipped (the start-anchored first label reaches further right than a centred one)
      var anchor = n === 1 ? "middle" : i === 0 ? "start" : i === n - 1 ? "end" : "middle";
      var left = anchor === "start" ? x(i) : anchor === "end" ? x(i) - tw(i) : x(i) - tw(i) / 2;
      if (left < right + 8) return;
      right = left + tw(i);
      svg.appendChild(s("text", { class: "fc-tick", x: x(i), y: H - 8, "text-anchor": anchor }, xLabel(xv, i)));
    });
    var cross = s("line", { class: "fc-cross", y1: pad.t, y2: pad.t + ph, x1: -10, x2: -10, visibility: "hidden" });
    svg.appendChild(cross);
    var dense = n > 30;
    ser.forEach(function (se, k) {
      var g = s("g"), d = "", pen = false, e = est[k], prev = -1;
      se.values.forEach(function (v, i) {
        if (!num(v)) { pen = false; return; }
        // estimate: one dashed segment straight across the gap, under the real line
        if (prev >= 0 && i - prev > 1 && e[prev + 1] != null) {
          var ep = s("path", { class: "fc-line fc-est", d: "M" + x(prev).toFixed(1) + "," + y(se.values[prev]).toFixed(1) + "L" + x(i).toFixed(1) + "," + y(v).toFixed(1), style: "stroke:" + estC });
          ep.appendChild(s("title", null, se.label + " · estimate (missing data) between " + xTitle(xs[prev], prev) + " and " + xTitle(xs[i], i)));
          g.appendChild(ep);
        }
        d += (pen ? "L" : "M") + x(i).toFixed(1) + "," + y(v).toFixed(1); pen = true; prev = i;
      });
      if (d) g.appendChild(s("path", { class: "fc-line", d: d, style: "stroke:" + se.color }));
      var last = -1;
      se.values.forEach(function (v, i) { if (num(v)) last = i; });
      se.values.forEach(function (v, i) {
        if (!num(v)) return;
        var lonely = !num(se.values[i - 1]) && !num(se.values[i + 1]);
        if (dense && !lonely && i !== last && e[i - 1] == null && e[i + 1] == null) return;   // estimates keep their real ends dotted
        var c = s("circle", { class: "fc-dot", cx: x(i), cy: y(v), r: 4, style: "fill:" + se.color });
        c.appendChild(s("title", null, se.label + " · " + xTitle(xs[i], i) + ": " + fmt(v)));
        g.appendChild(c);
      });
      svg.appendChild(g);
    });
    var hov = s("g");
    svg.appendChild(hov);
    st.scroll.appendChild(svg);
    bindIndex(st, svg, n, function (px) {
      if (px < pad.l - 10 || px > W - pad.r + 10) return -1;
      return n === 1 ? 0 : Math.max(0, Math.min(n - 1, Math.round((px - pad.l) / (pw / (n - 1)))));
    }, function (i, cx, cy) {
      hov.textContent = "";
      if (i < 0) { cross.setAttribute("visibility", "hidden"); hideTip(st); return; }
      cross.setAttribute("x1", x(i)); cross.setAttribute("x2", x(i)); cross.setAttribute("visibility", "visible");
      var rows = [];
      ser.forEach(function (se, k) {
        var v = se.values[i], ev = est[k][i], note = se.notes && se.notes[i] ? " " + se.notes[i] : "";
        if (num(v)) hov.appendChild(s("circle", { class: "fc-hover-dot", cx: x(i), cy: y(v), r: 5, style: "fill:" + se.color }));
        else if (ev != null) {
          hov.appendChild(s("circle", { class: "fc-hover-dot fc-est-dot", cx: x(i), cy: y(ev), r: 4, style: "stroke:" + estC }));
          rows.push({ color: estDash, value: "≈ " + fmt(ev), label: se.label + " (estimate)", v: ev, cls: "est" });
          return;
        }
        rows.push({ color: se.color, value: fmt(v), label: se.label + note, v: num(v) ? v : -Infinity });
      });
      rows.sort(function (a, b) { return b.v - a.v; });
      var r = svg.getBoundingClientRect();
      if (cx == null) { cx = r.left + x(i); cy = r.top + pad.t + ph / 3; }
      else cx = r.left + x(i);
      showTip(st, cx, cy, xTitle(xs[i], i), rows);
    });
  }

  window.FazonCharts = {
    columns: function (box, opts) { return frame(box, opts, drawColumns); },
    hbars: function (box, opts) { return frame(box, opts, drawHbars); },
    lines: function (box, opts) { return frame(box, opts, drawLines); },
    niceTicks: niceTicks
  };
})();
