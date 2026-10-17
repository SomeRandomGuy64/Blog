// Track core (week maths, categories, formatting, summary cards) + the profile-page module:
//   FazonTrack.renderProfile(containerEl, userId) → Promise<boolean>  (true when something was shown)
// Self-contained: needs only js/fazon-api.js (window.Fazon); it loads css/track.css and
// js/fazon-charts.js itself if the page hasn't. js/fazon-track.js (logger + leaderboard) builds on it.
(function () {
  "use strict";
  var T = window.FazonTrack = window.FazonTrack || {};
  var SRC = (document.currentScript && document.currentScript.src) || "/js/fazon-track-charts.js";

  var CATS = [
    { id: "active", label: "Active immersion", short: "Active" },
    { id: "passive", label: "Passive immersion", short: "Passive" },
    { id: "phonetic", label: "Phonetic training", short: "Phonetic" },
    { id: "output", label: "Output", short: "Output" },
    { id: "soroban", label: "Soroban", short: "Soroban" },
    { id: "anki", label: "Anki", short: "Anki", proper: true }    // proper: a name, never lower-cased in prose
  ];
  var CAT = {};
  CATS.forEach(function (c) { CAT[c.id] = c; });
  T.CATS = CATS;
  T.CAT = CAT;
  T.catColor = function (id) { return "var(--tk-" + id + ")"; };
  T.slotColor = function (i) { return "var(--tk-s" + ((i % 8) + 1) + ")"; };

  // ---- weeks: Sun 03:00 UTC → next Sun 03:00 UTC, keyed by the Saturday before (YYYY-MM-DD) ----
  var DAY = 864e5;
  function keyToMs(k) { return Date.UTC(+k.slice(0, 4), +k.slice(5, 7) - 1, +k.slice(8, 10)); }
  function msToKey(ms) { return new Date(ms).toISOString().slice(0, 10); }
  T.isKey = function (k) { return typeof k === "string" && /^\d{4}-\d{2}-\d{2}$/.test(k) && !isNaN(keyToMs(k)) && msToKey(keyToMs(k)) === k; };
  T.addWeeks = function (k, n) { return msToKey(keyToMs(k) + n * 7 * DAY); };
  T.addDays = function (k, n) { return msToKey(keyToMs(k) + n * DAY); };
  T.weekOfDate = function (d) { var ms = keyToMs(d), dow = new Date(ms).getUTCDay(); return msToKey(ms - ((dow + 1) % 7) * DAY); };
  T.currentWeekKey = function (now) {
    var d = Math.floor(((now == null ? Date.now() : now) - 3 * 36e5) / DAY), dow = (d + 4) % 7;   // 1970-01-01 was a Thursday
    return msToKey((d - dow - 1) * DAY);
  };
  T.weekStartMs = function (k) { return keyToMs(k) + DAY + 3 * 36e5; };
  T.weeksBetween = function (a, b) { return Math.round((keyToMs(b) - keyToMs(a)) / (7 * DAY)); };
  // fixed English formatting (Intl's en-GB output varies: "Sat, 10 Oct", "Sept")
  var WD = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"], MO = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
  function parts(ms) { var d = new Date(ms); return { wd: WD[d.getUTCDay()], d: d.getUTCDate(), m: MO[d.getUTCMonth()], y: d.getUTCFullYear() }; }
  T.fmtKeyLong = function (k) { var p = parts(keyToMs(k)); return p.wd + " " + p.d + " " + p.m + " " + p.y; };    // Sat 10 Oct 2026
  T.fmtKeyShort = function (k) { var p = parts(keyToMs(k)); return p.d + " " + p.m; };                            // 10 Oct
  T.fmtKeyAxis = function (k, multiYear) { var p = parts(keyToMs(k)); return multiYear ? p.m + " " + String(p.y).slice(2) : p.d + " " + p.m; };
  T.fmtDayLabel = function (k) { var p = parts(keyToMs(k)); return p.wd + " " + p.d + " " + p.m; };              // Sat 10 Oct
  // the week's start (Sunday 03:00 UTC) as a date in the viewer's own time zone: "Week of Sun 11 Oct
  // 2026" in Europe/Asia, "Week of Sat 10 Oct 2026" in the Americas (the UTC range stays in T.utcRange)
  T.weekTitle = function (k) {
    var d = new Date(T.weekStartMs(k));
    return "Week of " + WD[d.getDay()] + " " + d.getDate() + " " + MO[d.getMonth()] + " " + d.getFullYear();
  };
  T.utcRange = function (k) {
    var a = T.weekStartMs(k);
    var f = function (ms) { var p = parts(ms); return p.wd + " " + p.d + " " + p.m + " 03:00"; };
    return f(a) + " → " + f(a + 7 * DAY) + " UTC";
  };

  // ---- numbers ----
  function num(v) { return typeof v === "number" && isFinite(v); }
  T.num = num;
  T.fmtNum = function (v, d) { return num(v) ? (Math.round(v * 100) / 100).toLocaleString("en", { maximumFractionDigits: d == null ? 2 : d }) : "—"; };
  T.fmtH = function (v) { return num(v) ? T.fmtNum(v) + " h" : "—"; };
  T.fmtPct = function (v) { return num(v) ? T.fmtNum(v, 1) + "%" : "—"; };
  // logger (goals, stopwatch): hours → "35:00" / "2:30"; ms → "0:42:10"
  T.fmtHM = function (v) { if (!num(v)) return "—"; var m = Math.round(Math.max(0, v) * 60); return Math.floor(m / 60) + ":" + String(m % 60).padStart(2, "0"); };
  T.fmtClock = function (ms) { var s = Math.floor(Math.max(0, ms) / 1000); return Math.floor(s / 3600) + ":" + String(Math.floor(s / 60) % 60).padStart(2, "0") + ":" + String(s % 60).padStart(2, "0"); };
  // weekly self-ratings (API values 1–4)
  T.RATINGS = [{ v: 1, e: "😞", t: "Rough" }, { v: 2, e: "😐", t: "So-so" }, { v: 3, e: "👌", t: "Good" }, { v: 4, e: "🔥", t: "Great" }];
  T.sumTotals = function (t) { var a = 0; if (t) CATS.forEach(function (c) { if (num(t[c.id])) a += t[c.id]; }); return a; };
  T.mean = function (arr) { var v = (arr || []).filter(num); return v.length ? v.reduce(function (a, b) { return a + b; }, 0) / v.length : null; };

  // ---- loading css/track.css + js/fazon-charts.js on pages that don't include them ----
  var depsP = null;
  T.loadDeps = function () {
    if (depsP) return depsP;
    if (!document.querySelector('link[href$="css/track.css"]')) {
      var l = document.createElement("link");
      l.rel = "stylesheet"; l.href = new URL("../css/track.css", SRC).href;
      document.head.appendChild(l);
    }
    depsP = window.FazonCharts ? Promise.resolve() : new Promise(function (res, rej) {
      var sc = document.createElement("script");
      sc.src = new URL("fazon-charts.js", SRC).href;
      sc.onload = function () { res(); }; sc.onerror = function () { depsP = null; rej(new Error("Couldn't load the charts.")); };
      document.head.appendChild(sc);
    });
    return depsP;
  };

  // ---- weekly summaries are Markdown (js/fazon-md.js + vendored marked/DOMPurify, loaded on demand) ----
  // T.loadMD() starts loading early; T.mdFill(el, src) shows plain text until it's ready (or if it fails).
  var mdP = null;
  T.loadMD = function () {
    if (mdP) return mdP;
    mdP = (window.FazonMD ? Promise.resolve() : new Promise(function (res) {
      var sc = document.createElement("script");
      sc.src = new URL("fazon-md.js", SRC).href;
      sc.onload = sc.onerror = function () { res(); };
      document.head.appendChild(sc);
    })).then(function () { return window.FazonMD ? window.FazonMD.load() : false; });
    return mdP;
  };
  T.mdFill = function (node, src) {
    if (window.FazonMD) return window.FazonMD.fill(node, src);
    node.classList.add("fz-md");
    var p = el0("div", "fz-md-plain"); p.textContent = src; node.textContent = ""; node.appendChild(p);
    return T.loadMD().then(function (ok) { return ok && window.FazonMD ? window.FazonMD.fill(node, src) : false; });
  };
  function el0(tag, cls) { var n = document.createElement(tag); n.className = cls; return n; }

  // ---- avatar + name link (hover card via data-user-card) ----
  T.userLink = function (u, size, cls) {
    var F = window.Fazon, el = F.el, href = F.profileHref ? F.profileHref(u) : "/users/?id=" + ((u && u.id) | 0);
    var av = typeof F.avatar === "function" ? F.avatar(u, size || 32) :
      el("span", { class: "tk-initial", style: "width:" + (size || 32) + "px;height:" + (size || 32) + "px", text: (Array.from(String((u && u.name) || "?"))[0] || "?").toUpperCase() });
    return el("span", { class: cls || "lb-user" }, av,
      el("a", { href: href, "data-user-card": String((u && u.id) | 0) }, el("span", { text: (u && u.name) || "Someone" })));
  };

  // ---- one summary card (leaderboard feed + profile list) ----
  // item: {user?, week, summary, totals?, kotu?, kyotsu?, private?}; opts: {noUser?, fmtH?(hours) → string}
  T.summaryCard = function (it, opts) {
    var F = window.Fazon, el = F.el;
    opts = opts || {};
    var head = el("div", { class: "sm-head" },
      opts.noUser ? null : T.userLink(it.user, 28, "sm-who"),
      el("span", { class: "sm-week", text: T.weekTitle(it.week) }),
      it.private ? el("span", { class: "sm-priv", title: "Only you can see this summary", text: "🔒 Private" }) : null);
    var chips = el("div", { class: "sm-chips" });
    CATS.forEach(function (c) {
      var v = it.totals && it.totals[c.id];
      if (num(v) && v > 0) {
        var dot = el("span", { class: "tk-dotc" }); dot.style.background = T.catColor(c.id);
        chips.appendChild(el("span", { class: "sm-chip", title: c.label }, dot, c.short + " " + (opts.fmtH || T.fmtH)(v)));   // opts.fmtH: the leaderboard's h:mm
      }
    });
    if (num(it.kotu)) chips.appendChild(el("span", { class: "sm-chip", text: "Kotu " + T.fmtPct(it.kotu) }));
    if (num(it.kyotsu)) chips.appendChild(el("span", { class: "sm-chip", text: "Kyōtsu " + T.fmtPct(it.kyotsu) }));
    var text = el("div", { class: "sm-text" });
    var card = el("article", { class: "sm-card" }, head, chips.children.length ? chips : null, text);
    // collapse long ones (measured once the Markdown is in and laid out)
    T.mdFill(text, it.summary || "").then(function () { requestAnimationFrame(clamp); }, function () {});
    function clamp() {
      if (text.scrollHeight < 190 || card.querySelector(".sm-more")) return;   // ~ 8 lines
      text.classList.add("clamped");
      var more = el("button", { type: "button", class: "fz-linkbtn sm-more", text: "Read more", "aria-expanded": "false" });
      more.addEventListener("click", function () {
        var open = text.classList.toggle("clamped") === false;
        more.textContent = open ? "Show less" : "Read more";
        more.setAttribute("aria-expanded", String(open));
      });
      card.appendChild(more);
    }
    return card;
  };

  // ---- profile charts ----
  // GET /v1/users/{id}/track → {user, subcats:[{id,category,name}], weeks:[{week,totals,subtotals,kotu,kyotsu,summary,summary_private?}]}
  T.renderProfile = function (container, userId) {
    var F = window.Fazon;
    if (!container || !F) return Promise.resolve(false);
    container.hidden = true;
    T.loadMD();   // in parallel: summaries render as Markdown
    return Promise.all([F.api("/v1/users/" + (userId | 0) + "/track"), F.me(), T.loadDeps()]).then(function (r) {
      var d = r[0] || {}, m = r[1] || {}, weeks = (d.weeks || []).slice();
      var own = !!(m.user && m.user.id === (userId | 0));
      weeks.sort(function (a, b) { return a.week < b.week ? -1 : a.week > b.week ? 1 : 0; });
      var sums = weeks.filter(function (w) { return w.summary && String(w.summary).trim(); });
      container.textContent = "";
      container.classList.add("tkp");
      if (weeks.length < 2 && !sums.length) {
        if (own) {
          container.append(F.el("h2", { text: "Immersion" }),
            F.el("p", { class: "fz-muted tkp-link" }, "Log at least two weeks in ", F.el("a", { href: "/track/", text: "Track" }), " to see your charts here."));
          container.hidden = false;
          return true;
        }
        return false;
      }
      container.appendChild(F.el("h2", { text: "Immersion" }));
      if (weeks.length >= 2) charts(container, d, weeks, own);
      else if (own) container.appendChild(F.el("p", { class: "fz-muted tkp-link" }, "Charts appear once you've logged two weeks in ", F.el("a", { href: "/track/", text: "Track" }), "."));
      if (sums.length) summaries(container, d, sums, own);
      container.hidden = false;
      return true;
    }, function (e) {
      container.hidden = true;
      if (e && e.status !== 404) console.warn("Track charts:", e.message);
      return false;
    });
  };

  function charts(root, d, weeks, own) {
    var F = window.Fazon, el = F.el;
    // continuous week axis: first logged week → the current week (gaps = 0 h / no score)
    var first = weeks[0].week, last = weeks[weeks.length - 1].week, cur = T.currentWeekKey();
    if (cur > last && T.weeksBetween(last, cur) <= 104) last = cur;
    var keys = [], byKey = {};
    for (var k = first; k <= last; k = T.addWeeks(k, 1)) keys.push(k);
    weeks.forEach(function (w) { byKey[w.week] = w; });
    var multiYear = keys[0].slice(0, 4) !== keys[keys.length - 1].slice(0, 4) && keys.length > 20;
    var xLabel = function (k) { return T.fmtKeyAxis(k, multiYear); };

    // totals strip
    var tot = {}, all = 0;
    CATS.forEach(function (c) { tot[c.id] = 0; });
    weeks.forEach(function (w) { CATS.forEach(function (c) { var v = w.totals && w.totals[c.id]; if (num(v)) { tot[c.id] += v; all += v; } }); });
    var strip = el("div", { class: "tkp-totals" });
    CATS.forEach(function (c) {
      if (!tot[c.id]) return;
      var dot = el("span", { class: "tk-dotc" }); dot.style.background = T.catColor(c.id);
      strip.appendChild(el("div", {}, el("b", { text: T.fmtH(tot[c.id]) }), el("span", {}, dot, c.label)));
    });
    strip.appendChild(el("div", {}, el("b", { text: String(weeks.length) }), el("span", { text: weeks.length === 1 ? "week logged" : "weeks logged" })));
    root.appendChild(strip);

    // weekly hours: stacked by category, or one category by sub-category
    var used = CATS.filter(function (c) { return tot[c.id] > 0; });
    var seg = el("div", { class: "tk-seg", role: "group", "aria-label": "Break down" });
    var box = el("div");
    var block = el("div", { class: "tkp-block" }, el("div", { class: "tkp-block-h" }, el("h3", { text: "Weekly hours" }), seg), box);
    root.appendChild(block);
    var chart = null, mode = "all";
    var subName = {};
    (d.subcats || []).forEach(function (s) { subName[s.id] = s.name; });
    function series() {
      if (mode === "all") return used.map(function (c) {
        return { label: c.label, color: T.catColor(c.id), values: keys.map(function (k) { var w = byKey[k]; return w && w.totals && num(w.totals[c.id]) ? w.totals[c.id] : 0; }) };
      });
      // one category → its sub-categories (top 7 by hours, the rest + unassigned folded into "Other")
      var subT = {};
      weeks.forEach(function (w) { var st = w.subtotals && w.subtotals[mode]; if (st) Object.keys(st).forEach(function (sid) { if (num(st[sid])) subT[sid] = (subT[sid] || 0) + st[sid]; }); });
      var ids = Object.keys(subT).filter(function (sid) { return sid !== "none" && subT[sid] > 0; }).sort(function (a, b) { return subT[b] - subT[a]; });
      var keep = ids.slice(0, 7), fold = ids.slice(7).concat(["none"]);
      var out = keep.map(function (sid, i) {
        return { label: subName[sid] || "Sub-category", color: T.slotColor(i), values: keys.map(function (k) { var st = byKey[k] && byKey[k].subtotals && byKey[k].subtotals[mode]; return st && num(st[sid]) ? st[sid] : 0; }) };
      });
      var other = keys.map(function (k) { var st = byKey[k] && byKey[k].subtotals && byKey[k].subtotals[mode]; return st ? fold.reduce(function (a, sid) { return a + (num(st[sid]) ? st[sid] : 0); }, 0) : 0; });
      // weeks without a subtotal breakdown fall back to the category total
      keys.forEach(function (k, i) {
        var w = byKey[k]; if (!w || (w.subtotals && w.subtotals[mode])) return;
        if (w.totals && num(w.totals[mode])) other[i] = w.totals[mode];
      });
      if (other.some(function (v) { return v > 0; })) out.push({ label: keep.length ? "Other / unassigned" : CAT[mode].label, color: keep.length ? "var(--tk-other)" : T.catColor(mode), values: other });
      return out;
    }
    function draw() {
      var o = { x: keys, xLabel: xLabel, xTitle: T.weekTitle, series: series(), fmt: function (v, tick) { return tick ? T.fmtNum(v) : T.fmtH(v); },
        ariaLabel: "Hours per week" + (mode === "all" ? " by category" : " of " + CAT[mode].label + " by sub-category") };
      if (chart) chart.update(o); else chart = FazonCharts.columns(box, o);
    }
    [{ id: "all", label: "All" }].concat(used.map(function (c) { return { id: c.id, label: c.short }; })).forEach(function (b) {
      var btn = el("button", { type: "button", "aria-pressed": String(b.id === mode), text: b.label });
      btn.addEventListener("click", function () {
        mode = b.id;
        seg.querySelectorAll("button").forEach(function (x) { x.setAttribute("aria-pressed", String(x === btn)); });
        draw();
      });
      seg.appendChild(btn);
    });
    if (used.length < 2 && !used.some(function (c) { return weeks.some(function (w) { return w.subtotals && w.subtotals[c.id] && Object.keys(w.subtotals[c.id]).some(function (s) { return s !== "none"; }); }); })) seg.hidden = true;
    draw();

    accuracy(root, d, weeks, last);
    if (own) root.appendChild(el("p", { class: "tkp-link" }, el("a", { href: "/track/", text: "Log this week's hours →" })));
  }

  // Kotu + Kyōtsu accuracy: weekly averages or day by day, from the first scored week / day (not the
  // first logged one); missing weeks / days between two scores are bridged by dashed estimates.
  var ACC_VIEW = "fz-track-acc-view";
  function accuracy(root, d, weeks, lastWeek) {
    var el = window.Fazon.el;
    var scored = weeks.filter(function (w) { return num(w.kotu) || num(w.kyotsu); });
    if (!scored.length) return;
    var byKey = {}, byDay = {}, firstDay = null, lastDay = null;
    weeks.forEach(function (w) {
      byKey[w.week] = w;
      (w.days || []).forEach(function (day, i) {
        var kv = w.kotu_days && w.kotu_days[i], yv = w.kyotsu_days && w.kyotsu_days[i];
        byDay[day] = { kotu: num(kv) ? kv : null, kyotsu: num(yv) ? yv : null,
          src: { kotu: w.kotu_source && w.kotu_source[i], kyotsu: w.kyotsu_source && w.kyotsu_source[i] } };   // "manual" / "auto" (Kyōtsu trainer) / null
        if ((num(kv) || num(yv)) && (!firstDay || day < firstDay)) firstDay = day;
        if (!lastDay || day > lastDay) lastDay = day;
      });
    });
    var firstWeek = T.isKey(d.first_score_week) && byKey[d.first_score_week] ? d.first_score_week : scored[0].week;
    var wkeys = [];
    for (var k = firstWeek; k <= lastWeek; k = T.addWeeks(k, 1)) wkeys.push(k);
    // days run to the end of the last returned week, but never past today's label date (day i of
    // a week is labelled key + 1 + i: Sunday … Saturday)
    var cur = T.currentWeekKey(), today = T.addDays(cur, 1 + Math.max(0, Math.min(6, Math.floor((Date.now() - T.weekStartMs(cur)) / DAY))));
    if (lastDay && lastDay > today) lastDay = today;
    var dkeys = [];
    if (firstDay) for (var dk = firstDay; dk <= lastDay; dk = T.addDays(dk, 1)) dkeys.push(dk);

    var view = "week";
    try { if (localStorage.getItem(ACC_VIEW) === "day") view = "day"; } catch (e) { /* storage blocked */ }
    var seg = el("div", { class: "tk-seg", role: "group", "aria-label": "Accuracy view" });
    var box = el("div"), title = el("h3");
    root.appendChild(el("div", { class: "tkp-block tkp-acc" }, el("div", { class: "tkp-block-h" }, title, seg), box));
    var chart = null;
    var pct = function (v, tick) { return tick ? v + "%" : T.fmtPct(v); };
    function draw() {
      var o, daily = view === "day", ser = [];
      title.textContent = "Kotu & Kyōtsu accuracy (" + (daily ? "day by day" : "weekly average") + ")";
      if (daily) {
        var kot = dkeys.map(function (k) { return byDay[k] ? byDay[k].kotu : null; });
        var kyo = dkeys.map(function (k) { return byDay[k] ? byDay[k].kyotsu : null; });
        var auto = function (f) { return dkeys.map(function (k) { return byDay[k] && num(byDay[k][f]) && byDay[k].src[f] === "auto" ? "(auto from trainer)" : null; }); };
        if (kot.some(num)) ser.push({ label: "Kotu", color: "var(--tk-kotu)", values: kot, notes: auto("kotu") });
        if (kyo.some(num)) ser.push({ label: "Kyōtsu", color: "var(--tk-kyotsu)", values: kyo, notes: auto("kyotsu") });
        o = { x: dkeys, xLabel: T.fmtKeyShort, xTitle: T.fmtDayLabel, ariaLabel: "Daily Kotu and Kyōtsu accuracy", minStep: 3 };
      } else {
        var multiYear = wkeys[0].slice(0, 4) !== wkeys[wkeys.length - 1].slice(0, 4) && wkeys.length > 20;
        ["kotu", "kyotsu"].forEach(function (f) {
          var vals = wkeys.map(function (k) { return byKey[k] && num(byKey[k][f]) ? byKey[k][f] : null; });
          if (vals.some(num)) ser.push({ label: f === "kotu" ? "Kotu" : "Kyōtsu", color: "var(--tk-" + f + ")", values: vals });
        });
        o = { x: wkeys, xLabel: function (k) { return T.fmtKeyAxis(k, multiYear); }, xTitle: T.weekTitle, ariaLabel: "Weekly Kotu and Kyōtsu accuracy" };
      }
      o.series = ser; o.yMax = 100; o.fmt = pct; o.estimateGaps = true;
      if (chart) chart.update(o); else chart = FazonCharts.lines(box, o);
      // a long day-by-day run scrolls inside the chart: open on the latest days
      var sc = box.querySelector(".fc-scroll");
      if (sc) sc.scrollLeft = sc.scrollWidth;
    }
    [{ id: "week", label: "Weekly average" }, { id: "day", label: "Day by day" }].forEach(function (b) {
      var btn = el("button", { type: "button", "aria-pressed": String(b.id === view), text: b.label });
      btn.addEventListener("click", function () {
        if (view === b.id) return;
        view = b.id;
        seg.querySelectorAll("button").forEach(function (x) { x.setAttribute("aria-pressed", String(x === btn)); });
        try { localStorage.setItem(ACC_VIEW, view); } catch (e) { /* storage blocked */ }
        draw();
      });
      seg.appendChild(btn);
    });
    draw();
  }

  function summaries(root, d, sums, own) {
    var F = window.Fazon, el = F.el;
    var list = el("div", { class: "sm-list" });
    var items = sums.slice().reverse();
    var SHOW = 5, shown = 0;
    var more = el("button", { type: "button", class: "fz-btn", text: "Show more" });
    function page() {
      items.slice(shown, shown += SHOW).forEach(function (w) {
        list.appendChild(T.summaryCard({ user: d.user, week: w.week, summary: w.summary, totals: w.totals, kotu: w.kotu, kyotsu: w.kyotsu,
          private: own && !!w.summary_private }, { noUser: true }));
      });
      more.hidden = shown >= items.length;
    }
    more.addEventListener("click", page);
    page();
    root.append(el("div", { class: "tkp-block" }, el("div", { class: "tkp-block-h" }, el("h3", { text: "Weekly summaries" })), list,
      el("div", { class: "sm-foot" }, more)));
  }
})();
