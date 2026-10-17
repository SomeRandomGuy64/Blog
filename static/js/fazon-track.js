// Track pages: the weekly logger (/track/) and the leaderboard (/track/leaderboard/).
// Needs js/fazon-api.js (window.Fazon), js/fazon-charts.js and js/fazon-track-charts.js (FazonTrack core).
//   FazonTrack.initLogger(rootEl)      FazonTrack.initLeaderboard(rootEl)
(function () {
  "use strict";
  var T = window.FazonTrack, F = window.Fazon, el = F.el, num = T.num, CATS = T.CATS, CAT = T.CAT;
  var runes = function (s) { return Array.from(String(s || "")).length; };
  var SUMMARY_MAX = 10000, SUB_MAX = 30;

  // "1.5" / "1,5" / "1:30" / "90m" / "1h30" / "2h" → hours (2 decimals); "" → null
  function parseHours(raw) {
    var t = String(raw == null ? "" : raw).trim().toLowerCase().replace(/\s+/g, "");
    if (!t) return { v: null };
    var m, v;
    if ((m = t.match(/^(\d{1,3}):([0-5]?\d)$/))) v = +m[1] + (+m[2]) / 60;
    else if ((m = t.match(/^(\d+(?:[.,]\d+)?)m(?:in|ins|inutes?)?$/))) v = parseFloat(m[1].replace(",", ".")) / 60;
    else if ((m = t.match(/^(\d+(?:[.,]\d+)?)h(?:rs?|ours?)?(?:(\d{1,2})(?:m|min|mins)?)?$/))) v = parseFloat(m[1].replace(",", ".")) + (m[2] ? +m[2] / 60 : 0);
    else if (/^(\d+(?:[.,]\d*)?|[.,]\d+)$/.test(t)) v = parseFloat(t.replace(",", "."));
    else return { err: "Use 1.5, 1:30 or 90m" };
    if (!isFinite(v)) return { err: "Use 1.5, 1:30 or 90m" };
    v = Math.round(v * 100) / 100;
    if (v > 168) return { err: "At most 168 h in a week" };
    return { v: v };
  }
  function parseScore(raw) {
    var t = String(raw == null ? "" : raw).trim().replace(/%$/, "").trim().replace(",", ".");
    if (!t) return { v: null };
    if (!/^(\d+(\.\d*)?|\.\d+)$/.test(t)) return { err: "0–100" };
    var v = Math.round(parseFloat(t) * 10) / 10;
    if (!(v >= 0 && v <= 100)) return { err: "0–100" };
    return { v: v };
  }
  function hk(cat, sub) { return cat + "|" + (sub == null ? "none" : sub); }
  function inputH(v) { return num(v) && v > 0 ? String(Math.round(v * 100) / 100) : ""; }

  // =====================================================================================
  // Logger
  // =====================================================================================
  T.initLogger = function (root) {
    var cfg = null, cur = null, key = null, wk = null, st = null, base = "", saving = false, savedAt = null, saveErr = "";
    // stale: the week changed on the server since it was loaded here (another tab, the Kyōtsu trainer adding
    // practice time, the API). A save replaces the whole week, so it's refused (409) until the week is reloaded.
    var stale = false;
    var STALE_MSG = "This week was changed elsewhere (another tab, the Kyōtsu trainer or the API). Reload it to get the latest; unsaved changes here are lost.";
    var ui = {}, stripData = null, stripRange = null, openMenu = null, showErrs = false, saveP = null, uid = 0;
    T.loadMD();   // summary preview renders Markdown (fazon-track-charts.js)

    function msg() {
      root.textContent = "";
      root.appendChild(el.apply(null, ["p", { class: "tk-msg" }].concat([].slice.call(arguments))));
    }
    function subs(cat) {
      return (cfg.subcats || []).filter(function (s) { return s.category === cat; })
        .sort(function (a, b) { return (a.position || 0) - (b.position || 0) || a.id - b.id; });
    }
    function subById(id) { return (cfg.subcats || []).filter(function (s) { return s.id === id; })[0] || null; }
    // sub-categories belong to weeks: the form shows those added to this week, or with hours in it
    function shownSubs(cat) { return subs(cat).filter(function (s) { return st.subs[s.id] || st.hours[hk(cat, s.id)]; }); }
    function upsertSub(n) { var x = subById(n.id); if (x) { Object.assign(x, n); return x; } cfg.subcats.push(n); return n; }

    function fromServer(w) {
      var s = { hours: {}, subs: {}, kotu: [], kyotsu: [], summary: w.summary || "", priv: !!w.summary_private, ratings: {} };
      (w.subcats || []).forEach(function (id) { s.subs[id] = true; });
      CATS.forEach(function (c) { var v = w.ratings && w.ratings[c.id]; if (v >= 1 && v <= 4) s.ratings[c.id] = v | 0; });
      (w.hours || []).forEach(function (h) {
        if (!(num(h.hours) && h.hours > 0)) return;
        s.hours[hk(h.category, h.subcat)] = inputH(h.hours);
        if (h.subcat != null) s.subs[h.subcat] = true;   // stays in the form while its hours are edited
      });
      for (var i = 0; i < 7; i++) {
        s.kotu.push(num(w.kotu && w.kotu[i]) ? String(w.kotu[i]) : "");
        s.kyotsu.push(num(w.kyotsu && w.kyotsu[i]) ? String(w.kyotsu[i]) : "");
      }
      return s;
    }
    // parse the form: payload + per-field errors + totals; norm = string used for dirty checks
    function compute() {
      var out = { hours: [], errs: {}, catTotal: {}, catErr: {}, ok: true }, normH = [];
      CATS.forEach(function (c) { out.catTotal[c.id] = 0; });
      Object.keys(st.hours).sort().forEach(function (k) {
        var p = parseHours(st.hours[k]), parts = k.split("|"), cat = parts[0];
        if (p.err) { out.errs["h:" + k] = p.err; out.ok = false; normH.push(k + "=?" + st.hours[k]); return; }
        if (p.v == null || p.v === 0) return;
        out.catTotal[cat] += p.v;
        out.hours.push({ category: cat, subcat: parts[1] === "none" ? null : +parts[1], hours: p.v });
        normH.push(k + "=" + p.v);
      });
      CATS.forEach(function (c) {
        out.catTotal[c.id] = Math.round(out.catTotal[c.id] * 100) / 100;
        if (out.catTotal[c.id] > 168) { out.catErr[c.id] = c.label + " adds up to " + T.fmtH(out.catTotal[c.id]) + " — a week only has 168 hours."; out.ok = false; }
      });
      ["kotu", "kyotsu"].forEach(function (f) {
        out[f] = st[f].map(function (raw, i) {
          var p = parseScore(raw);
          if (p.err) { out.errs[f + ":" + i] = p.err; out.ok = false; return "?" + raw; }
          return p.v;
        });
      });
      var sumLen = runes(st.summary);
      if (sumLen > SUMMARY_MAX) { out.errs.summary = "Too long"; out.ok = false; }
      out.norm = JSON.stringify([normH, out.kotu, out.kyotsu, st.summary.trim(), st.priv, CATS.map(function (c) { return st.ratings[c.id] || 0; })]);
      // blank days fall back to the trainer's automatic values (Kotu ← Kyōtsu's Tokyo mode, Kyōtsu ← Keihan)
      ["kotu", "kyotsu"].forEach(function (f) {
        out[f + "Avg"] = T.mean(out[f].map(function (v, i) { return num(v) ? v : (wk[f + "_auto"] ? wk[f + "_auto"][i] : null); }));
      });
      return out;
    }
    function dirty() { return !!st && compute().norm !== base; }

    // ---- week navigation ----
    function go(k) {
      if (!T.isKey(k)) return;
      if (k > cur) k = cur;
      if (k < "2000-01-01") k = "2000-01-01";
      if (k === key) return;
      if (dirty() && !confirm("You have unsaved changes for the week of " + T.fmtWeekLong(key) + ". Discard them?")) return;
      loadWeek(k);
    }
    function loadWeek(k) {
      if (ui.form) ui.form.style.opacity = "0.5";
      return F.api("/v1/track/weeks/" + k).then(function (w) { showWeek(w, k); }, function (e) {
        if (ui.form) ui.form.style.opacity = "";
        if (e.status === 401) { loggedOut(true); return; }
        if (!wk) msg(e.message); else alert(e.message);
      });
    }

    function showWeek(w, k) {
      wk = w; key = w.week || k; st = fromServer(w); base = compute().norm;
      savedAt = null; saveErr = ""; stale = false; showErrs = false; goalUI = { open: false, vals: {} };
      var u = new URL(location.href);
      if (key === cur) u.searchParams.delete("week"); else u.searchParams.set("week", key);
      history.replaceState(null, "", u.pathname + u.search + u.hash);
      render();
      loadStrip(false);
    }
    // back on this tab: if the week changed elsewhere meanwhile, show the new one (nothing unsaved here
    // to lose) or warn before a save would be refused
    function checkFresh() {
      if (!st || saving || stale) return;
      var k = key;
      F.api("/v1/track/weeks/" + k).then(function (w) {
        if (key !== k || saving || !st || (w.updated || "") === (wk.updated || "")) return;
        if (!dirty()) { var y = window.scrollY; showWeek(w, k); window.scrollTo(0, y); swFlush(); paintSw(); }
        else { stale = true; refresh(); }
      }, function () {});
    }
    function reloadStale() {
      var y = window.scrollY;
      return loadWeek(key).then(function () { window.scrollTo(0, y); swFlush(); paintSw(); });
    }

    // ---- recent-weeks strip (12 weeks around the one shown) ----
    function loadStrip(force) {
      var end = T.addWeeks(key, 5) > cur ? cur : T.addWeeks(key, 5), start = T.addWeeks(end, -11);
      if (start < "2000-01-01") { start = "2000-01-01"; }
      var rk = start + ".." + end;
      if (!force && stripRange === rk && stripData) { paintStrip(); return; }
      stripRange = rk;
      F.api("/v1/track/weeks?from=" + start + "&to=" + end).then(function (d) {
        if (stripRange !== rk) return;
        stripData = {};
        ((d && d.weeks) || []).forEach(function (w) { stripData[w.week] = w; });
        paintStrip();
      }, function () { stripData = {}; paintStrip(); });
    }
    function paintStrip() {
      if (!ui.strip || !stripRange) return;
      var r = stripRange.split(".."), box = ui.strip;
      box.textContent = "";
      for (var k = r[0]; k <= r[1]; k = T.addWeeks(k, 1)) {
        var w = stripData && stripData[k], tot = w ? T.sumTotals(w.totals) : 0;
        var chip = el("button", { type: "button", class: "tk-chip" + (w ? " filled" : "") + (k === key ? " cur" : ""),
          "aria-current": k === key ? "true" : null, title: T.weekTitle(k) + (w ? " · " + T.fmtH(tot) + " logged" : " · nothing logged"), "data-k": k },
          el("span", { text: T.fmtWeekShort(k) }), el("b", { text: w ? (tot ? T.fmtNum(tot, 1) + "h" : "✓") : "—" }));
        chip.addEventListener("click", (function (kk) { return function () { go(kk); }; })(k));
        box.appendChild(chip);
      }
      var c = box.querySelector(".cur");
      if (c) box.scrollLeft = c.offsetLeft - box.clientWidth / 2 + c.offsetWidth / 2;
    }

    // ---- rendering ----
    function closeMenu() { if (openMenu) { openMenu.menu.hidden = true; openMenu.btn.setAttribute("aria-expanded", "false"); openMenu = null; } }
    document.addEventListener("click", function (e) { if (openMenu && !openMenu.wrap.contains(e.target)) closeMenu(); });
    document.addEventListener("keydown", function (e) {
      if (e.key === "Escape" && openMenu) { var b = openMenu.btn; closeMenu(); b.focus(); }
      if ((e.ctrlKey || e.metaKey) && !e.altKey && (e.key === "s" || e.key === "S") && st) { e.preventDefault(); save(); }
    });
    window.addEventListener("beforeunload", function (e) { if (dirty() && !saving) { e.preventDefault(); e.returnValue = ""; } });

    function render() {
      var focusF = document.activeElement && document.activeElement.getAttribute && document.activeElement.getAttribute("data-f");
      closeMenu();
      root.textContent = "";
      ui = { cats: {}, fields: {}, errs: {}, tot: {}, catErr: {}, goal: {}, sw: {} };

      // navigator
      var prev = el("button", { type: "button", class: "tk-nav-btn", "aria-label": "Previous week", text: "‹", disabled: key <= "2000-01-01" });
      var next = el("button", { type: "button", class: "tk-nav-btn", "aria-label": "Next week", text: "›", disabled: key >= cur });
      prev.addEventListener("click", function () { go(T.addWeeks(key, -1)); });
      next.addEventListener("click", function () { go(T.addWeeks(key, 1)); });
      var date = el("input", { type: "date", class: "fz-input tk-input-sm", value: T.weekPickerValue(key), min: "2000-01-01", max: T.weekPickerMax(cur), "aria-label": "Jump to the week containing a date" });
      date.addEventListener("change", function () {
        if (!T.isKey(date.value)) return;
        var k = T.weekOfLocalDate(date.value);
        if (k > cur) { k = cur; }
        if (k === key) { date.value = T.weekPickerValue(key); return; }
        go(k);
        if (key !== k) date.value = T.weekPickerValue(key);   // navigation cancelled
      });
      var thisWeek = el("button", { type: "button", class: "fz-btn tk-small", text: "This week", disabled: key === cur });
      thisWeek.addEventListener("click", function () { go(cur); });
      var nav = el("div", { class: "tk-card" },
        el("div", { class: "tk-nav" }, prev,
          el("div", { class: "tk-nav-mid" },
            el("div", { class: "tk-week", text: T.weekTitle(key) + (key === cur ? " (this week)" : "") }),
            el("div", { class: "tk-utc", text: T.utcRange(key) })),
          next),
        el("div", { class: "tk-nav-tools" }, el("label", { text: "Go to date" }), date, thisWeek),
        ui.strip = el("div", { class: "tk-strip", role: "group", "aria-label": "Recent weeks" }));
      date.id = "tkDate"; date.previousSibling && date.previousSibling.setAttribute("for", "tkDate");

      ui.form = el("div", { class: "tk-form" });
      CATS.forEach(function (c) { ui.form.appendChild(ui.cats[c.id] = catSection(c)); });
      ui.form.appendChild(scoresSection());
      ui.form.appendChild(summarySection());

      ui.status = el("span", { class: "tk-status", role: "status" });
      ui.save = el("button", { type: "button", class: "fz-btn primary", text: "Save week" });
      ui.save.addEventListener("click", save);
      ui.reload = el("button", { type: "button", class: "fz-btn", text: "Reload week", hidden: true });
      ui.reload.addEventListener("click", reloadStale);
      var bar = el("div", { class: "tk-savebar" }, ui.status, ui.reload, ui.save);

      var pub = el("input", { type: "checkbox", checked: !!cfg.public, id: "tkPublic" });
      var pubErr = el("span", { class: "tk-err" });
      pub.addEventListener("change", function () {
        pub.disabled = true; pubErr.textContent = "";
        F.api("/v1/track/settings", { method: "POST", body: { public: pub.checked } }).then(function (d) {
          cfg.public = !!(d && d.public); pub.checked = cfg.public; pub.disabled = false; paintSummaryNote();
        }, function (e) { pub.checked = !!cfg.public; pub.disabled = false; pubErr.textContent = e.message; });
      });
      var settings = el("div", { class: "tk-card tk-settings" },
        el("label", { for: "tkPublic" }, pub, "Show me on the leaderboard"),
        pubErr,
        el("a", { href: "/track/leaderboard/", text: "See the leaderboard →" }));

      root.append(swBar(), nav);
      var gc = goalsCard(); if (gc) root.appendChild(gc);
      root.append(ui.form, bar, settings);
      var ac = adminCard(); if (ac) root.appendChild(ac);
      paintStrip();
      refresh();
      paintSw();
      if (focusF) { var f = root.querySelector('[data-f="' + focusF + '"]'); if (f) f.focus({ preventScroll: true }); }
    }

    function catSection(c) {
      var sec = el("section", { class: "tk-card tk-cat", "aria-label": c.label });
      var tot = ui.tot[c.id] = el("span", { class: "tk-cat-tot" });
      sec.appendChild(el("div", { class: "tk-cat-h" }, el("h3", {}, (function () { var d = el("span", { class: "tk-dotc" }); d.style.background = T.catColor(c.id); return d; })(), c.label), tot));
      var gl = goalLine(c); if (gl) sec.appendChild(gl);
      var shown = shownSubs(c.id);
      // hours for an id we don't know (deleted meanwhile) still show
      Object.keys(st.hours).forEach(function (k) {
        var p = k.split("|");
        if (p[0] === c.id && p[1] !== "none" && st.hours[k] && !subById(+p[1])) shown.push({ id: +p[1], category: c.id, name: "Sub-category #" + p[1], archived: true, unknown: true });
      });
      shown.forEach(function (s) { sec.appendChild(row(c, s)); });
      sec.appendChild(row(c, null, shown.length ? "Other / unassigned" : "Hours"));
      sec.appendChild(ui.catErr[c.id] = el("div", { class: "tk-err", role: "alert" }));
      sec.appendChild(rateRow(c));

      // footer: + add (to this week only)
      var foot = el("div", { class: "tk-cat-foot" });
      var addBtn = el("button", { type: "button", class: "fz-linkbtn", text: "+ Add sub-category to this week" });
      foot.appendChild(addBtn);
      addBtn.addEventListener("click", function () {
        if (shown.length >= SUB_MAX) { alert("A week can have up to " + SUB_MAX + " sub-categories here. Remove one first."); return; }
        var inp = el("input", { type: "text", class: "fz-input tk-input-sm", maxlength: "60", placeholder: c.id === "active" ? "e.g. YouTube, Anime, Games" : "Name", "aria-label": "New sub-category name for " + c.label });
        var ok = el("button", { type: "button", class: "fz-btn primary tk-small", text: "Add" });
        var no = el("button", { type: "button", class: "fz-btn tk-small", text: "Cancel" });
        var err = el("div", { class: "tk-err", role: "alert" });
        var form = el("div", { class: "tk-add" }, inp, ok, no, err);
        addBtn.replaceWith(form); inp.focus();
        no.addEventListener("click", function () { form.replaceWith(addBtn); addBtn.focus(); });
        function submit() {
          var name = inp.value.trim();
          if (!name || runes(name) > 40) { err.textContent = "Names are 1–40 characters."; return; }
          if (shown.some(function (s) { return s.name.toLowerCase() === name.toLowerCase(); })) { err.textContent = "“" + name + "” is already in this week."; return; }
          ok.disabled = true; err.textContent = "";
          F.api("/v1/track/weeks/" + key + "/subcats", { method: "POST", body: { category: c.id, name: name } }).then(function (n) {
            var s = upsertSub(n); st.subs[s.id] = true; rerenderCat(c);
            var f = ui.cats[c.id].querySelector('[data-f="h:' + hk(c.id, s.id) + '"]'); if (f) f.focus();
          }, function (e) { ok.disabled = false; err.textContent = e.message; });
        }
        ok.addEventListener("click", submit);
        inp.addEventListener("keydown", function (e) { if (e.key === "Enter") { e.preventDefault(); submit(); } else if (e.key === "Escape") { e.preventDefault(); no.click(); } });
      });
      sec.appendChild(foot);
      return sec;
    }
    function rerenderCat(c) {
      var old = ui.cats[c.id], fresh = catSection(c);
      old.replaceWith(fresh); ui.cats[c.id] = fresh; refresh(); paintSw();
    }

    function row(c, s, noneLabel) {
      var k = hk(c.id, s ? s.id : null), f = "h:" + k;
      var inp = el("input", { type: "text", inputmode: "decimal", autocomplete: "off", class: "fz-input tk-h", "data-f": f,
        value: st.hours[k] || "", placeholder: "0", "aria-label": c.label + " — " + (s ? s.name : noneLabel) + " (hours)" });
      inp.addEventListener("input", function () { st.hours[k] = inp.value; if (!inp.value) delete st.hours[k]; refresh(); });
      inp.addEventListener("blur", function () {   // tidy to decimal hours once valid
        var p = parseHours(inp.value);
        if (!p.err && inp.value) { inp.value = p.v ? String(p.v) : ""; if (inp.value) st.hours[k] = inp.value; else delete st.hours[k]; }
        showErrs = true; refresh();
      });
      ui.fields[f] = inp;
      var name = el("span", { class: "tk-row-name" + (s ? "" : " none") }, s ? s.name : noneLabel);
      var r = el("div", { class: "tk-row" }, name);
      var swRow = key === cur ? swRowControls(c, s, noneLabel, k) : null;   // stopwatch: the current week only
      if (swRow) r.appendChild(swRow.start);
      r.append(inp, el("span", { class: "tk-unit", text: "h" }));
      if (s && !s.unknown) {
        var btn = el("button", { type: "button", class: "tk-menu-btn", "aria-haspopup": "true", "aria-expanded": "false", "aria-label": "Manage " + s.name, text: "⋯" });
        var menu = el("div", { class: "tk-menu", hidden: true, role: "menu" });
        var mk = function (label, fn, cls) { var b = el("button", { type: "button", role: "menuitem", class: cls || null, text: label }); b.addEventListener("click", function () { closeMenu(); fn(); }); menu.appendChild(b); };
        mk("Rename", function () { renameSub(c, s, name, r); });
        mk("Remove from this week", function () { removeSub(c, s); }, "danger");
        btn.addEventListener("click", function (e) {
          e.stopPropagation();
          var was = openMenu && openMenu.menu === menu; closeMenu();
          if (!was) { menu.hidden = false; btn.setAttribute("aria-expanded", "true"); openMenu = { menu: menu, btn: btn, wrap: r }; var b0 = menu.querySelector("button"); if (b0) b0.focus(); }
        });
        r.append(btn, menu);
      } else r.appendChild(el("span", { class: "tk-menu-btn ph", "aria-hidden": "true" }));
      ui.errs[f] = el("div", { class: "tk-err", role: "alert" });
      var wrap = el("div", {}, r, swRow ? swRow.live : null, ui.errs[f]);
      return wrap;
    }

    function renameSub(c, s, nameEl, r) {
      var inp = el("input", { type: "text", class: "fz-input tk-input-sm", value: s.name, maxlength: "60", "aria-label": "New name for " + s.name });
      var ok = el("button", { type: "button", class: "fz-btn primary tk-small", text: "Save" });
      var no = el("button", { type: "button", class: "fz-btn tk-small", text: "Cancel" });
      var box = el("div", { class: "tk-rename" }, inp, ok, no);
      var err = r.parentNode.querySelector(".tk-err");
      nameEl.replaceWith(box); inp.focus(); inp.select();
      var close = function () { box.replaceWith(nameEl); };
      no.addEventListener("click", close);
      function submit() {
        var v = inp.value.trim();
        if (!v || runes(v) > 40) { err.textContent = "Names are 1–40 characters."; return; }
        if (v === s.name) { close(); return; }
        ok.disabled = true; err.textContent = "";
        // this week only: the server renames it in place if no other week has it, else this week
        // switches to a sub-category with the new name and its hours move over (saved with the form)
        F.api("/v1/track/weeks/" + key + "/subcats", { method: "POST", body: { category: c.id, name: v, replace: s.id } }).then(function (n) {
          upsertSub(n);
          if (n.id !== s.id) {
            var ok = hk(c.id, s.id), nk = hk(c.id, n.id);
            if (st.hours[ok]) { st.hours[nk] = st.hours[ok]; delete st.hours[ok]; }
            delete st.subs[s.id];
          }
          st.subs[n.id] = true; rerenderCat(c);
        }, function (e) { ok.disabled = false; err.textContent = e.message; });
      }
      ok.addEventListener("click", submit);
      inp.addEventListener("keydown", function (e) { if (e.key === "Enter") { e.preventDefault(); submit(); } else if (e.key === "Escape") { e.preventDefault(); close(); } });
    }
    function removeSub(c, s) {
      var v = st.hours[hk(c.id, s.id)];
      if (v && parseHours(v).v) { alert("“" + s.name + "” has hours in this week. Clear them first (they can go under Other / unassigned)."); return; }
      F.api("/v1/track/weeks/" + key + "/subcats/" + s.id, { method: "DELETE" }).then(function (r) {
        delete st.subs[s.id]; delete st.hours[hk(c.id, s.id)];
        if (r && r.deleted) cfg.subcats = cfg.subcats.filter(function (x) { return x.id !== s.id; });
        rerenderCat(c);
      }, function (e) { alert(e.message); });
    }

    function scoresSection() {
      var sec = el("section", { class: "tk-card", "aria-label": "Daily scores" });
      sec.appendChild(el("div", { class: "tk-cat-h" }, el("h3", { text: "Daily scores" })));
      sec.appendChild(el("p", { class: "fz-muted tk-small", style: "margin:0 0 10px" },
        "Total accuracy per day: ", el("a", { href: "https://kotu.io", target: "_blank", rel: "noopener", text: "Kotu" }),
        " (enter it yourself) and ", el("a", { href: "/kyotsu/", text: "Kyōtsu" }),
        ". Blank days use your saved Kyōtsu sessions automatically (Kotu from its Tokyo mode, if you have it)."));
      var grid = el("div", { class: "tk-scores" },
        el("span", { class: "hd", text: "Day" }), el("span", { class: "hd", style: "text-align:right", text: "Kotu %" }), el("span", { class: "hd", style: "text-align:right", text: "Kyōtsu %" }));
      var days = wk.days && wk.days.length === 7 ? wk.days : [1, 2, 3, 4, 5, 6, 7].map(function (i) { return T.addDays(key, i); });   // Sunday … Saturday
      // automatic values from saved trainer sessions: Kyōtsu ← Keihan mode, Kotu ← Tokyo mode
      var AUTO = { kotu: wk.kotu_auto || [], kyotsu: wk.kyotsu_auto || [] }, ANS = { kotu: wk.kotu_auto_answers || [], kyotsu: wk.kyotsu_auto_answers || [] };
      days.forEach(function (d, i) {
        grid.appendChild(el("span", { class: "tk-day", text: T.fmtDayLabel(d) }));
        ["kotu", "kyotsu"].forEach(function (f) {
          var a = num(AUTO[f][i]) ? AUTO[f][i] : null, ans = ANS[f];
          var inp = el("input", { type: "text", inputmode: "decimal", autocomplete: "off", class: "fz-input tk-sc", "data-f": f + ":" + i, value: st[f][i] || "",
            placeholder: a != null ? T.fmtNum(a, 1) : "–", "aria-label": (f === "kotu" ? "Kotu" : "Kyōtsu") + " % on " + T.fmtDayLabel(d) + (a != null ? " (auto: " + T.fmtNum(a, 1) + "%)" : "") });
          inp.addEventListener("input", function () { st[f][i] = inp.value; refresh(); });
          inp.addEventListener("blur", function () { var p = parseScore(inp.value); if (!p.err && inp.value) { inp.value = p.v == null ? "" : String(p.v); st[f][i] = inp.value; } showErrs = true; refresh(); });
          ui.fields[f + ":" + i] = inp;
          var ht = ans[i] ? ans[i] + " answers" + (st[f][i] ? " · auto " + T.fmtNum(a, 1) + "%" : "") : "";
          var hint = el("span", { class: "tk-hint", text: ht, title: ht || null });
          ui.errs[f + ":" + i] = el("span", { class: "tk-err", role: "alert" });
          grid.appendChild(el("span", { class: "tk-sc-cell" }, inp, hint, ui.errs[f + ":" + i]));
        });
      });
      ui.kotuAvg = el("span", { class: "tk-avg" }); ui.kyotsuAvg = el("span", { class: "tk-avg" });
      grid.append(el("span", { class: "tk-day", style: "font-weight:700", text: "Average" }), ui.kotuAvg, ui.kyotsuAvg);
      sec.appendChild(grid);
      // "Use my … results": copy the automatic values into the column. Kyōtsu's is always shown; Kotu's
      // only in weeks with Tokyo-mode sessions (most accounts don't have that mode)
      function useAuto(f) {
        var auto = AUTO[f], ans = ANS[f], anyAuto = auto.some(num), what = f === "kotu" ? "Kyōtsu Tokyo-mode" : "Kyōtsu";
        var use = el("button", { type: "button", class: "fz-btn tk-small", text: f === "kotu" ? "Use my Tokyo-mode results" : "Use my Kyōtsu results", disabled: !anyAuto,
          title: anyAuto ? "Fill the " + (f === "kotu" ? "Kotu" : "Kyōtsu") + " column from your saved " + what + " sessions this week" : "No saved Kyōtsu sessions this week" });
        use.addEventListener("click", function () {
          auto.forEach(function (v, i) { if (num(v)) { st[f][i] = String(Math.round(v * 10) / 10); var x = ui.fields[f + ":" + i]; if (x) x.value = st[f][i]; } });
          refresh();
        });
        return el("div", { class: "tk-scores-foot" }, use,
          el("span", { class: "fz-muted", text: anyAuto ? auto.filter(num).length + " day" + (auto.filter(num).length === 1 ? "" : "s") + " with saved " + (f === "kotu" ? "Tokyo-mode " : "") + "sessions (" + ans.reduce(function (a, b) { return a + (b | 0); }, 0) + " answers)" : "No saved Kyōtsu sessions this week" }));
      }
      sec.appendChild(useAuto("kyotsu"));
      if (AUTO.kotu.some(num)) sec.appendChild(useAuto("kotu"));
      return sec;
    }

    function summarySection() {
      var sec = el("section", { class: "tk-card", "aria-label": "Weekly summary" });
      var ta = el("textarea", { class: "fz-textarea", rows: "5", "data-f": "summary", "aria-label": "Weekly summary",
        placeholder: "How did the week go? What did you watch, play or read? What clicked, what didn't?" });
      ta.value = st.summary;
      var count = el("span", { class: "tk-count" });
      var priv = el("input", { type: "checkbox", id: "tkPriv", checked: st.priv });
      ui.sumNote = el("p", { class: "fz-muted tk-small", style: "margin:0 0 8px" });
      ta.addEventListener("input", function () { st.summary = ta.value; refresh(); });
      priv.addEventListener("change", function () { st.priv = priv.checked; refresh(); });
      ui.count = count;
      // Write | Preview tabs: the preview uses the same sanitized Markdown renderer as the public pages
      var tabW = el("button", { type: "button", role: "tab", id: "tkSumTabW", class: "tk-mdtab", "aria-selected": "true", "aria-controls": "tkSumPanW", text: "Write" });
      var tabP = el("button", { type: "button", role: "tab", id: "tkSumTabP", class: "tk-mdtab", "aria-selected": "false", "aria-controls": "tkSumPanP", tabindex: "-1", text: "Preview" });
      var prev = el("div", { class: "sm-text tk-md-prev" });
      var panW = el("div", { role: "tabpanel", id: "tkSumPanW", "aria-labelledby": "tkSumTabW" }, ta);
      var panP = el("div", { role: "tabpanel", id: "tkSumPanP", "aria-labelledby": "tkSumTabP", tabindex: "0", hidden: true }, prev);
      function showTab(preview, focus) {
        [[tabW, !preview], [tabP, preview]].forEach(function (x) { x[0].setAttribute("aria-selected", String(x[1])); x[0].tabIndex = x[1] ? 0 : -1; });
        panW.hidden = preview; panP.hidden = !preview;
        if (preview) {
          if (st.summary.trim()) T.mdFill(prev, st.summary);
          else { prev.textContent = ""; prev.appendChild(el("p", { class: "fz-muted", text: "Nothing to preview yet." })); }
        }
        if (focus) (preview ? tabP : tabW).focus();
      }
      tabW.addEventListener("click", function () { showTab(false); });
      tabP.addEventListener("click", function () { showTab(true); });
      var tabs = el("div", { role: "tablist", class: "tk-mdtabs", "aria-label": "Weekly summary editor" }, tabW, tabP);
      tabs.addEventListener("keydown", function (e) {
        var k = e.key, toP = k === "ArrowRight" || k === "End" ? true : k === "ArrowLeft" || k === "Home" ? false : null;
        if (toP === null) return;
        e.preventDefault();
        showTab(k === "ArrowRight" || k === "ArrowLeft" ? tabP.getAttribute("aria-selected") !== "true" : toP, true);
      });
      var ex = function (codes, what) {
        var li = el("li");
        codes.forEach(function (c) { li.append(el("code", { text: c }), " "); });
        if (what) li.append(what);
        return li;
      };
      var help = el("details", { class: "tk-md-help" }, el("summary", { text: "Markdown supported" }),
        el("ul", null,
          ex(["**bold**", "*italic*", "~~strike~~"]),
          ex(["# Heading"], "(shown small)"),
          ex(["- item", "1. item", "- [x] done"], "lists, task lists"),
          ex(["> quote"]),
          ex(["`code`"], "— a line of ``` before and after makes a code block"),
          ex(["[text](https://example.com)"], "links (http, https, mailto)"),
          ex(["| a | b |"], "tables, with a |---|---| line under the header")),
        el("p", { class: "fz-muted", text: "A single line break is kept. Images are shown as links, and HTML is shown as plain text." }));
      sec.append(el("div", { class: "tk-cat-h" }, el("h3", { text: "Weekly summary" })), ui.sumNote,
        el("div", { class: "tk-md-bar" }, tabs, help), panW, panP,
        el("div", { class: "tk-sum-foot" }, el("label", { for: "tkPriv" }, priv, "Keep this summary private"), count));
      return sec;
    }
    function paintSummaryNote() {
      if (!ui.sumNote) return;
      ui.sumNote.textContent = st.priv ? "Private — only you can see this week's summary."
        : cfg.public ? "Public — shown on your profile and in the leaderboard's summary feed. Tick “Keep this summary private” to hide it."
        : "Only you can see it while you're hidden from the leaderboard (see the setting below).";
    }

    function refresh() {
      if (!st || !ui.status) return;
      var r = compute();
      CATS.forEach(function (c) {
        var t = ui.tot[c.id]; if (!t) return;
        t.textContent = T.fmtH(r.catTotal[c.id] || 0);
        t.classList.toggle("bad", !!r.catErr[c.id]);
        ui.catErr[c.id].textContent = r.catErr[c.id] || "";
        var g = ui.goal[c.id]; if (!g) return;   // goal progress follows the form, saved or not
        // rounded down (on exact hundredths), so 99.6% never reads as a met 100%
        var have = r.catTotal[c.id] || 0, pc = Math.floor(Math.round(have * 100) / g.g + 1e-9), w = Math.min(100, pc);
        g.pct.textContent = pc + "%";
        g.fill.style.width = w + "%";
        g.box.classList.toggle("met", have >= g.g);
        g.bar.setAttribute("aria-valuenow", String(w));
        g.bar.setAttribute("aria-valuetext", T.fmtHM(have) + " of " + T.fmtHM(g.g) + " (" + pc + "%)");
      });
      Object.keys(ui.fields).forEach(function (f) {
        var bad = r.errs[f], inp = ui.fields[f], show = bad && (showErrs || document.activeElement !== inp);
        inp.classList.toggle("bad", !!bad);
        inp.setAttribute("aria-invalid", bad ? "true" : "false");
        if (ui.errs[f]) ui.errs[f].textContent = show ? bad : "";
        else inp.title = bad || "";
      });
      ui.kotuAvg.textContent = T.fmtPct(r.kotuAvg);
      ui.kyotsuAvg.textContent = T.fmtPct(r.kyotsuAvg);
      var n = runes(st.summary);
      ui.count.textContent = n.toLocaleString("en") + " / " + SUMMARY_MAX.toLocaleString("en");
      ui.count.classList.toggle("over", n > SUMMARY_MAX);
      paintSummaryNote();
      var d = r.norm !== base;
      ui.status.className = "tk-status";
      ui.reload.hidden = !stale || saving;
      if (saving) ui.status.textContent = "Saving…";
      else if (stale) { ui.status.textContent = STALE_MSG; ui.status.classList.add("bad"); }
      else if (!r.ok) { ui.status.textContent = "Fix the highlighted fields to save."; ui.status.classList.add("bad"); }
      else if (saveErr && d) { ui.status.textContent = saveErr; ui.status.classList.add("bad"); }
      else if (d) { ui.status.textContent = "● Unsaved changes"; ui.status.classList.add("dirty"); }
      else ui.status.textContent = savedAt ? "Saved ✓ " + savedAt : wk.updated ? "Saved · last edited " + F.relTime(wk.updated) : "Nothing logged for this week yet.";
      ui.save.disabled = saving || stale || !r.ok || !d;
      ui.save.hidden = stale && !saving;   // Reload week takes its place
    }

    // → Promise<boolean> (true = saved); while a save runs, saveP is it
    function save() {
      if (saving) return saveP;
      if (!st) return Promise.resolve(false);
      var r = compute();
      showErrs = true;
      if (!r.ok) { refresh(); var bad = root.querySelector(".tk-form .bad"); if (bad && bad.focus) bad.focus(); return Promise.resolve(false); }
      if (r.norm === base) return Promise.resolve(true);
      if (stale) { refresh(); return Promise.resolve(false); }
      saving = true; saveErr = ""; refresh();
      var ratings = {}, sk = key;
      CATS.forEach(function (c) { if (st.ratings[c.id]) ratings[c.id] = st.ratings[c.id]; });
      // if_updated: refused (409) if the week changed on the server since it was loaded here
      var body = { hours: r.hours, kotu: r.kotu, kyotsu: r.kyotsu, ratings: ratings, summary: st.summary.trim(), summary_private: !!st.priv,
        if_updated: wk.updated || "" };
      return (saveP = F.api("/v1/track/weeks/" + sk, { method: "POST", body: body }).then(function (w) {
        saving = false;
        if (key !== sk) return true;   // navigated away meanwhile
        wk = w; st = fromServer(w); base = compute().norm;
        savedAt = new Date().toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
        render(); loadStrip(true);
        return true;
      }, function (e) {
        saving = false;
        if (key !== sk) return false;
        if (e.status === 401) saveErr = "Your login has expired — log in again in another tab, then save.";
        else if (e.status === 409) stale = true;
        else saveErr = "Couldn't save: " + e.message;
        refresh();
        return false;
      }));
    }

    // ---- small shared bits for ratings / goals / stopwatch / admin ----
    var SVGNS = "http://www.w3.org/2000/svg";
    var ICON = { play: "M4 2.5v11l9-5.5z", pause: "M3.5 2.5h3v11h-3zM9.5 2.5h3v11h-3z", stop: "M3 3h10v10H3z" };
    function icon(name) {
      var s = document.createElementNS(SVGNS, "svg"), p = document.createElementNS(SVGNS, "path");
      s.setAttribute("viewBox", "0 0 16 16"); s.setAttribute("width", "12"); s.setAttribute("height", "12");
      s.setAttribute("aria-hidden", "true"); s.setAttribute("focusable", "false"); s.setAttribute("class", "tk-ico");
      p.setAttribute("d", ICON[name]); p.setAttribute("fill", "currentColor"); s.appendChild(p);
      return s;
    }
    function setBtn(b, ico, text, label) {   // icon + visible text, same element (keeps focus)
      b.textContent = ""; b.appendChild(icon(ico)); if (text) b.appendChild(el("span", { text: text }));
      b.setAttribute("aria-label", label);
    }
    function fmtWhen(ms) {
      var d = new Date(ms);
      return isNaN(d) ? "—" : d.toLocaleString(undefined, { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit", timeZoneName: "short" });
    }
    // "Confirm? [Yes] [Cancel]" inside the page (no window.confirm)
    function confirmBox(text, yesLabel, onYes, onNo, danger) {
      var yes = el("button", { type: "button", class: "fz-btn tk-small" + (danger ? " tk-danger" : " primary"), text: yesLabel });
      var no = el("button", { type: "button", class: "fz-btn tk-small", text: "Cancel" });
      yes.addEventListener("click", onYes); no.addEventListener("click", onNo);
      var box = el("div", { class: "tk-confirm", role: "group", "aria-label": "Confirm" }, el("p", { text: text }), el("div", { class: "tk-confirm-btns" }, yes, no));
      box.addEventListener("keydown", function (e) { if (e.key === "Escape") { e.preventDefault(); onNo(); } });
      return { box: box, yes: yes, no: no };
    }

    // ---- weekly self-rating per category (saved with the week) ----
    function rateRow(c) {
      var grp = el("div", { class: "tk-rate-btns", role: "group", "aria-label": "How did " + (c.proper ? c.label : c.label.toLowerCase()) + " go this week?" });
      T.RATINGS.forEach(function (r) {
        var b = el("button", { type: "button", class: "tk-rate-btn", "data-v": String(r.v), "aria-pressed": String(st.ratings[c.id] === r.v),
          "aria-label": c.short + " rating: " + r.t, title: r.t, text: r.e });
        b.addEventListener("click", function () {
          if (st.ratings[c.id] === r.v) delete st.ratings[c.id]; else st.ratings[c.id] = r.v;
          grp.querySelectorAll("button").forEach(function (x) { x.setAttribute("aria-pressed", String(+x.getAttribute("data-v") === st.ratings[c.id])); });
          refresh();
        });
        grp.appendChild(b);
      });
      return el("div", { class: "tk-rate" }, el("span", { class: "tk-rate-l", "aria-hidden": "true", text: "Rating" }), grp);
    }

    // ---- goals: progress in each category card + the write-once "Set goals" panel ----
    var goalUI = { open: false, vals: {} };   // survives re-renders (a stopwatch save re-renders the page); reset per week
    function goalOf(id) { var g = wk.goals && wk.goals[id]; return num(g) && g > 0 ? g : null; }
    function goalLine(c) {
      var g = goalOf(c.id); if (g == null) return null;
      var pct = el("span", { class: "tk-goal-pct" }), fill = el("span", { class: "tk-goal-fill" });
      fill.style.background = T.catColor(c.id);
      var bar = el("div", { class: "tk-goal-bar", role: "progressbar", "aria-label": c.label + " goal progress", "aria-valuemin": "0", "aria-valuemax": "100" }, fill);
      var box = el("div", { class: "tk-goal" },
        el("div", { class: "tk-goal-h" }, el("span", { class: "tk-goal-l", title: "This week's goal (goals can't be changed once set)" }, el("span", { class: "tk-goal-ic", "aria-hidden": "true", text: "◎" }), "Goal: ", el("b", { text: T.fmtHM(g) })), pct),
        bar);
      ui.goal[c.id] = { g: g, pct: pct, fill: fill, bar: bar, box: box };
      return box;
    }
    function goalsCard() {
      var missing = CATS.filter(function (c) { return goalOf(c.id) == null; });
      if (!wk.goals_open || key !== cur || (!missing.length && !goalUI.err && !goalUI.note)) return null;
      var nset = CATS.length - missing.length;
      var btn = el("button", { type: "button", class: "fz-btn tk-small", "aria-expanded": String(!!goalUI.open), "aria-controls": "tkGoalsPanel", text: "Set goals", hidden: !missing.length });
      var panel = el("div", { class: "tk-goals-panel", id: "tkGoalsPanel", hidden: !goalUI.open || !missing.length });
      var note = el("p", { class: "tk-goals-note" + (goalUI.err ? " bad" : ""), role: "status", text: goalUI.open ? "" : (goalUI.err || goalUI.note || "") });
      var card = el("section", { class: "tk-card tk-goals", "aria-label": "Weekly goals" },
        el("div", { class: "tk-goals-h" },
          el("div", { class: "tk-goals-t" }, el("b", {}, el("span", { class: "tk-goal-ic", "aria-hidden": "true", text: "◎" }), "Weekly goals"),
            el("span", { class: "fz-muted", text: nset ? nset + " of " + CATS.length + " set for this week" : "Aim for a number of hours in any category this week" })),
          btn),
        note, panel);
      btn.addEventListener("click", function () {
        goalUI.open = !goalUI.open; goalUI.confirm = false; goalUI.err = ""; goalUI.note = "";
        btn.setAttribute("aria-expanded", String(goalUI.open)); panel.hidden = !goalUI.open; note.textContent = "";
        if (goalUI.open) { paintGoalsPanel(panel, btn); var f = panel.querySelector("input"); if (f) f.focus(); }
      });
      if (goalUI.open && missing.length) paintGoalsPanel(panel, btn);
      return card;
    }
    function paintGoalsPanel(panel, btn) {
      panel.textContent = "";
      var list = el("div", { class: "tk-goal-list" }), inputs = {}, errs = {};
      CATS.forEach(function (c) {
        var g = goalOf(c.id), dot = el("span", { class: "tk-dotc" }); dot.style.background = T.catColor(c.id);
        var lab = el("label", { class: "tk-goal-name", for: "tkGoal-" + c.id }, dot, c.label);
        if (g != null) {
          lab = el("span", { class: "tk-goal-name" }, dot, c.label);
          list.appendChild(el("div", { class: "tk-goal-row locked" }, lab,
            el("span", { class: "tk-goal-lock", title: "Goals can't be changed once set" }, el("span", { "aria-hidden": "true", text: "🔒 " }), T.fmtHM(g) + " h",
              el("span", { class: "fz-sr", text: " (set, locked)" }))));
          return;
        }
        var inp = inputs[c.id] = el("input", { type: "text", inputmode: "decimal", autocomplete: "off", class: "fz-input tk-h", id: "tkGoal-" + c.id,
          value: goalUI.vals[c.id] || "", placeholder: "h:mm", "aria-label": "Goal for " + c.label + " (hours)", readonly: !!goalUI.confirm });
        inp.addEventListener("input", function () { goalUI.vals[c.id] = inp.value; inp.classList.remove("bad"); errs[c.id].textContent = ""; });
        inp.addEventListener("keydown", function (e) { if (e.key === "Enter") { e.preventDefault(); if (!goalUI.confirm) review(); } });
        errs[c.id] = el("div", { class: "tk-err", role: "alert" });
        list.appendChild(el("div", {}, el("div", { class: "tk-goal-row" }, lab, inp, el("span", { class: "tk-unit", text: "h" })), errs[c.id]));
      });
      var err = el("div", { class: "tk-err", role: "alert", text: goalUI.err || "" });
      var warn = el("p", { class: "tk-goal-warn" }, el("b", { text: "Goals are final." }),
        " Once set, a goal can't be changed or removed this week. Leave a category blank to set it later this week.");
      var acts = el("div", { class: "tk-goal-acts" });
      panel.append(list, warn, err, acts);
      function parsed() {   // → {goals, labels} or null (errors shown)
        var goals = {}, labels = [], ok = true;
        Object.keys(inputs).forEach(function (id) {
          var p = parseHours(inputs[id].value), e = p.err || (p.v === 0 ? "Must be more than 0" : "");
          if (!e && p.v != null && Math.round(p.v * 60) < 1) e = "At least a minute";
          errs[id].textContent = e; inputs[id].classList.toggle("bad", !!e); inputs[id].setAttribute("aria-invalid", e ? "true" : "false");
          if (e) { ok = false; return; }
          if (p.v != null) { goals[id] = p.v; labels.push(CAT[id].short + " " + T.fmtHM(p.v)); }
        });
        if (!ok) { var b = panel.querySelector("input.bad"); if (b) b.focus(); return null; }
        if (!labels.length) { err.textContent = "Enter a goal for at least one category."; var f = panel.querySelector("input"); if (f) f.focus(); return null; }
        return { goals: goals, labels: labels };
      }
      function review() {
        err.textContent = "";
        var g = parsed(); if (!g) return;
        goalUI.confirm = g; paintGoalsPanel(panel, btn);
        var y = panel.querySelector(".tk-confirm .primary"); if (y) y.focus();
      }
      if (!goalUI.confirm) {
        var go = el("button", { type: "button", class: "fz-btn primary tk-small", text: "Review goals" });
        var cancel = el("button", { type: "button", class: "fz-btn tk-small", text: "Cancel" });
        go.addEventListener("click", review);
        cancel.addEventListener("click", function () { goalUI.open = false; goalUI.err = ""; panel.hidden = true; btn.setAttribute("aria-expanded", "false"); btn.focus(); });
        acts.append(go, cancel);
        return;
      }
      var G = goalUI.confirm;
      var cb = confirmBox("Set " + G.labels.join(", ") + " as this week's goal" + (G.labels.length > 1 ? "s" : "") + "? You won't be able to change " + (G.labels.length > 1 ? "them" : "it") + ".",
        "Yes, set " + (G.labels.length > 1 ? "goals" : "goal"), submit, function () {
          goalUI.confirm = false; paintGoalsPanel(panel, btn); var f = panel.querySelector("input"); if (f) f.focus();
        });
      cb.no.textContent = "Back";
      acts.appendChild(cb.box);
      function submit() {
        cb.yes.disabled = cb.no.disabled = true; cb.yes.textContent = "Saving…";
        var gk = key;
        F.api("/v1/track/weeks/" + gk + "/goals", { method: "POST", body: { goals: G.goals } }).then(function (w) {
          if (key !== gk) return;
          wk.goals = (w && w.goals) || {}; wk.goals_open = !!(w && w.goals_open);
          goalUI = { open: false, vals: {}, note: "Goals set ✓ — your progress shows in each category below." };
          render();
          var b = root.querySelector(".tk-goals button"); if (b && !b.hidden) b.focus();
        }, function (e) {
          if (key !== gk) return;
          goalUI.confirm = false;
          goalUI.err = e.status === 401 ? "Your login has expired — log in again in another tab." : e.message;
          if (e.status === 409 || e.status === 400) {
            // someone (another tab?) may have set goals meanwhile, or the week just ended: refresh what's locked
            F.api("/v1/track/weeks/" + gk).then(function (w2) {
              if (key !== gk) return;
              wk.goals = w2.goals || {}; wk.goals_open = !!w2.goals_open;
              CATS.forEach(function (c) { if (goalOf(c.id) != null) delete goalUI.vals[c.id]; });
              render();
            }, function () { paintGoalsPanel(panel, btn); });
          } else paintGoalsPanel(panel, btn);
        });
      }
    }

    // ---- admin: end the current week early (POST/DELETE /v1/track/admin/end-week) ----
    var adminUI = { confirm: false, busy: false, err: "", note: "" }, adminTimer = null;
    function adminCard() {
      if (!cfg.end_week) return null;
      ui.admin = el("section", { class: "tk-card tk-admin", "aria-label": "Admin: end of week" });
      paintAdmin();
      return ui.admin;
    }
    function paintAdmin() {
      var box = ui.admin; if (!box || !cfg.end_week) return;
      var focusOn = box.contains(document.activeElement);
      box.textContent = "";
      clearTimeout(adminTimer);
      var endMs = Date.parse(cfg.current_week_end), fromMs = Date.parse(cfg.end_week.allowed_from), now = Date.now();
      var open = now >= fromMs && now < endMs;   // the server decides; this only keeps the button honest while the page sits open
      if (!open && fromMs > now && fromMs - now < 2147483647) adminTimer = setTimeout(paintAdmin, fromMs - now + 1000);
      box.appendChild(el("div", { class: "tk-admin-h" }, el("span", { class: "tk-admin-tag", text: "Admin" }), el("b", { text: "End of week" })));
      var line = el("p", { class: "tk-admin-line" }), acts = el("div", { class: "tk-admin-acts" });
      var btn;
      if (cfg.ended_early) {
        line.append(el("b", { class: "tk-admin-ended", text: "Ended early — results are public." }),
          " The week still takes edits until " + fmtWhen(endMs) + ".");
        btn = el("button", { type: "button", class: "fz-btn tk-small", text: "Undo" });
      } else {
        line.append("This week's results go public at ", el("b", { text: fmtWhen(endMs) }), ".");
        btn = el("button", { type: "button", class: "fz-btn tk-small", text: "End week now", disabled: !open || adminUI.busy,
          "aria-describedby": open ? null : "tkAdminWhen" });
      }
      if (adminUI.busy) btn.disabled = true;
      acts.appendChild(btn);
      if (!cfg.ended_early && !open) acts.appendChild(el("span", { class: "fz-muted tk-small", id: "tkAdminWhen",
        text: now < fromMs ? "available from " + fmtWhen(fromMs) : "the week is over" }));
      box.append(line, acts);
      if (adminUI.confirm) {
        var undo = !!cfg.ended_early;
        var cb = confirmBox(undo ? "Undo the early end? This week's results go private again until " + fmtWhen(endMs) + "."
            : "End this week now? Everyone's results for this week become public on the leaderboard straight away. You can undo it until the week ends.",
          undo ? "Yes, undo" : "Yes, end the week", function () { adminSend(undo ? "DELETE" : "POST"); },
          function () { adminUI.confirm = false; paintAdmin(); var b = ui.admin && ui.admin.querySelector("button"); if (b) b.focus(); }, !undo);
        if (adminUI.busy) { cb.yes.disabled = cb.no.disabled = true; cb.yes.textContent = "Working…"; }
        btn.hidden = true;
        box.appendChild(cb.box);
        if (focusOn && !adminUI.busy) cb.yes.focus();
      } else if (focusOn && !btn.disabled) btn.focus();
      btn.addEventListener("click", function () { adminUI.confirm = true; adminUI.err = ""; adminUI.note = ""; paintAdmin(); var y = ui.admin.querySelector(".tk-confirm button"); if (y) y.focus(); });
      box.appendChild(el("p", { class: "tk-err", role: "alert", text: adminUI.err }));
      box.appendChild(el("p", { class: "tk-admin-note", role: "status", text: adminUI.note }));
    }
    function adminSend(method) {
      adminUI.busy = true; adminUI.err = ""; paintAdmin();
      F.api("/v1/track/admin/end-week", { method: method }).then(function (d) {
        d = d || {};
        ["published_through", "current_week", "current_week_end", "ended_early", "end_week"].forEach(function (f) { if (f in d) cfg[f] = d[f]; });
        adminUI = { confirm: false, busy: false, err: "", note: method === "POST" ? "Done — this week's results are on the leaderboard now." : "Undone — this week's results are private again." };
        paintAdmin();
        var b = ui.admin && ui.admin.querySelector("button"); if (b) b.focus();
      }, function (e) {
        adminUI = { confirm: false, busy: false, err: (e.status === 401 ? "Your login has expired." : e.message), note: "" };
        paintAdmin();
      });
    }

    // ---- stopwatch: one at a time, per user, kept in localStorage so it survives reloads ----
    // sw  = {week, cat, sub (id|null), acc: ms banked, since: ms epoch while running | null while paused}
    // swq = [{week, cat, sub, ms}]: time on its way into a week (a stop, or the part before a week rolled over)
    var sw = null, swq = [], swTimer = null, swMsg = null, swBusy = false, swStoreOK = true, swRefetched = 0;
    var MIN = 6e4;
    function swKey(q) { return "fz-track-" + (q ? "swq" : "sw") + ":" + uid; }
    function swOK(v) {
      return v && typeof v === "object" && T.isKey(v.week) && CAT[v.cat] && (v.sub === null || (typeof v.sub === "number" && v.sub > 0 && v.sub % 1 === 0));
    }
    function swLoad() {   // storage is the truth (another tab may have changed it); memory if storage is unusable
      if (!swStoreOK || !uid) return;
      try {
        var v = JSON.parse(localStorage.getItem(swKey()) || "null");
        sw = swOK(v) && num(v.acc) && v.acc >= 0 && (v.since === null || num(v.since)) ? { week: v.week, cat: v.cat, sub: v.sub, acc: v.acc, since: v.since } : null;
        var q = JSON.parse(localStorage.getItem(swKey(true)) || "[]");
        swq = Array.isArray(q) ? q.filter(function (x) { return swOK(x) && num(x.ms) && x.ms > 0; }) : [];
      } catch (e) { /* keep what's in memory */ }
    }
    function swStore() {
      if (!uid) return;
      try {
        if (sw) localStorage.setItem(swKey(), JSON.stringify(sw)); else localStorage.removeItem(swKey());
        if (swq.length) localStorage.setItem(swKey(true), JSON.stringify(swq)); else localStorage.removeItem(swKey(true));
        swStoreOK = true;
      } catch (e) { swStoreOK = false; }
    }
    function swElapsed(s, now) { return s ? s.acc + (s.since != null ? Math.max(0, (now || Date.now()) - s.since) : 0) : 0; }
    function weekEnd(k) {
      if (k === cfg.current_week && cfg.current_week_end) { var e = Date.parse(cfg.current_week_end); if (!isNaN(e)) return e; }
      return T.weekStartMs(k) + 7 * 864e5;
    }
    function swName(it) {
      var c = CAT[it.cat], s = it.sub != null ? subById(it.sub) : null;
      return c.short + (it.sub != null ? " › " + (s ? s.name : "Sub-category") : st && it.week === key && shownSubs(it.cat).length ? " › Other" : "");
    }
    function swWeekNote(it) { return it.week === key ? "" : " (week of " + T.fmtWeekShort(it.week) + ")"; }
    function note(text, bad, item) {
      swMsg = { text: text, bad: !!bad, item: item || null, at: Date.now() };
      if (!bad) setTimeout(function () { if (swMsg && swMsg.text === text && !swMsg.bad) { swMsg = null; paintSw(); } }, 8000);
    }
    // a running stopwatch that crossed its week's end: the time before goes to that week, the rest to the next
    // week's same row (its unassigned row if the sub-category is archived or gone)
    function swRoll() {
      var moved = false;
      while (sw && sw.since != null) {
        var end = weekEnd(sw.week);
        if (Date.now() < end) break;
        var ms = sw.acc + Math.max(0, end - sw.since);
        if (Math.round(ms / MIN) >= 1) swq.push({ week: sw.week, cat: sw.cat, sub: sw.sub, ms: ms });
        var nsub = sw.sub != null && subById(sw.sub) && !subById(sw.sub).archived ? sw.sub : null;
        sw = { week: T.addWeeks(sw.week, 1), cat: sw.cat, sub: nsub, acc: 0, since: Math.max(sw.since, end) };
        moved = true;
      }
      if (!moved) return false;
      swStore();
      if (sw.week > cur && Date.now() - swRefetched > 60000) {   // the calendar moved on under an open page
        swRefetched = Date.now();
        F.api("/v1/track/config").then(function (c) {
          if (!c || !T.isKey(c.current_week) || c.current_week === cur) return;
          ["current_week", "current_week_end", "published_through", "ended_early", "end_week", "public"].forEach(function (f) { if (f in c) cfg[f] = c[f]; });
          if (c.subcats) cfg.subcats = c.subcats;
          cur = c.current_week;
          if (st) render();   // the form (st) is kept as it is
        }, function () {});
      }
      swFlush();
      return true;
    }
    function swStart(c, sub) {
      swLoad(); swRoll();
      if (sw && sw.cat === c.id && sw.sub === sub && sw.week === key) return;
      if (sw) swStop(true);   // only one at a time: the other one's time goes to its own row first
      sw = { week: key, cat: c.id, sub: sub, acc: 0, since: Date.now() };
      swStore(); paintSw();
      var r = ui.sw[hk(c.id, sub)]; if (r) r.toggle.focus();
    }
    function swToggle() {   // pause ⇄ resume
      swLoad(); if (!sw) { paintSw(); return; }
      swRoll();
      if (sw.since != null) { sw.acc = swElapsed(sw); sw.since = null; }
      else { sw.since = Date.now(); swStore(); swRoll(); }
      swStore(); paintSw();
    }
    function swStop(quiet) {
      swLoad(); if (!sw) { paintSw(); return; }
      swRoll();
      var it = { week: sw.week, cat: sw.cat, sub: sw.sub, ms: swElapsed(sw) }, k = hk(sw.cat, sw.sub), wasHere = sw.week === key;
      sw = null;
      if (Math.round(it.ms / MIN) >= 1) swq.push(it);
      else if (!quiet) note("Stopped after less than half a minute — nothing added.");
      swStore(); paintSw(); swFlush();
      if (!quiet && wasHere && ui.sw[k] && ui.sw[k].start.isConnected) ui.sw[k].start.focus();
    }
    // add queued time, oldest first; the shown week goes through the form (keeps unsaved edits), others are
    // loaded, added to and saved on their own key
    function swFlush() {
      if (swBusy) return;
      swLoad();
      if (!swq.length) return;
      var it = swq.shift(); swStore();
      swBusy = true;
      swAdd(it).then(function (res) {
        swBusy = false;
        if (swMsg && swMsg.bad && swMsg.item && swMsg.item.week === it.week && swMsg.item.cat === it.cat && swMsg.item.sub === it.sub && swMsg.item.ms === it.ms) swMsg = null;
        if (res && res.other) note("Added " + T.fmtHM(res.min / 60) + " to " + swName(it) + " in the week of " + T.fmtWeekShort(it.week) + " ✓");
        paintSw(); swFlush();
      }, function (e) {
        swBusy = false;
        swLoad(); swq.unshift(it); swStore();
        note("Couldn't add " + T.fmtHM(Math.round(it.ms / MIN) / 60) + " to " + swName(it) + swWeekNote(it) + ": " + (e.status === 401 ? "your login has expired." : e.message), true, it);
        paintSw();
      });
    }
    // retried: a 409 (the week changed elsewhere) was already answered by reloading the week once
    function swAdd(it, retried) {
      var min = Math.round(it.ms / MIN);
      if (min < 1) return Promise.resolve(null);
      var sub = it.sub != null && subById(it.sub) ? it.sub : null;   // deleted meanwhile → unassigned
      var staleErr = function () {
        var e = new Error("this week was changed elsewhere. Reload week adds it (unsaved edits on this page are lost)."); e.status = 409; return e;
      };
      if (it.week === key && st) {
        if (saving) return saveP.then(function () { return swAdd(it, retried); });   // the save in flight would overwrite the form
        if (stale) return Promise.reject(staleErr());
        var k = hk(it.cat, sub), p = parseHours(st.hours[k]), wasDirty = dirty(), prev = st.hours[k];
        if (p.err) return Promise.reject(new Error("fix the " + swName({ cat: it.cat, sub: sub }) + " hours first, then press Retry"));
        st.hours[k] = String(Math.round(((p.v || 0) + min / 60) * 100) / 100);
        var f = ui.fields["h:" + k];
        if (f && f.isConnected) { f.value = st.hours[k]; refresh(); } else render();
        return save().then(function (ok) {
          if (ok || !stale || key !== it.week) return { min: min };   // a failed save leaves it in the form as an unsaved change
          // 409: take the time back out of the stale form; it stays queued, nothing is lost
          st.hours[k] = prev; var f2 = ui.fields["h:" + k];
          if (f2 && f2.isConnected) { f2.value = prev || ""; refresh(); } else render();
          // nothing else unsaved here: reload the week and add the time to the fresh copy
          if (!wasDirty && !retried) return loadWeek(key).then(function () { return swAdd(it, true); });
          throw staleErr();
        });
      }
      return F.api("/v1/track/weeks/" + it.week).then(function (w) {
        var found = false, hours = (w.hours || []).map(function (h) {
          var x = { category: h.category, subcat: h.subcat == null ? null : h.subcat, hours: h.hours };
          if (x.category === it.cat && x.subcat === sub) { x.hours = Math.round((x.hours + min / 60) * 100) / 100; found = true; }
          return x;
        });
        if (!found) hours.push({ category: it.cat, subcat: sub, hours: Math.round(min / 60 * 100) / 100 });
        return F.api("/v1/track/weeks/" + it.week, { method: "POST", body: { hours: hours, kotu: w.kotu || [], kyotsu: w.kyotsu || [],
          ratings: w.ratings || {}, summary: w.summary || "", summary_private: !!w.summary_private, if_updated: w.updated || "" } });
      }).then(function () { loadStrip(true); return { min: min, other: true }; }, function (e) {
        if (e.status === 409 && !retried) return swAdd(it, true);   // changed between the read and the write: once more
        throw e;
      });
    }
    // per-row controls: ▶ in the row; while it's this row's turn, a line under it with the time, ⏸/▶ and ■
    function swRowControls(c, s, noneLabel, k) {
      var who = s ? s.name : noneLabel === "Hours" ? c.label : c.label + " (other / unassigned)";
      // data-f: render() puts focus back on the same control after a save re-renders the page
      var start = el("button", { type: "button", class: "tk-sw-start", title: "Start a stopwatch", "data-f": "sw-start:" + k });
      setBtn(start, "play", "", "Start stopwatch for " + who);
      var time = el("span", { class: "tk-sw-time", role: "timer", "aria-label": "Stopwatch for " + who });
      var state = el("span", { class: "tk-sw-state" });
      var toggle = el("button", { type: "button", class: "fz-btn tk-small tk-sw-btn", "data-f": "sw-toggle:" + k });
      var stop = el("button", { type: "button", class: "fz-btn tk-small tk-sw-btn", "data-f": "sw-stop:" + k });
      setBtn(stop, "stop", "Stop", "Stop stopwatch for " + who + " and add the time");
      var live = el("div", { class: "tk-sw-live", hidden: true }, el("span", { class: "tk-sw-dot", "aria-hidden": "true" }), time, state, el("span", { class: "tk-sw-btns" }, toggle, stop));
      start.addEventListener("click", function () { swStart(c, s ? s.id : null); });
      toggle.addEventListener("click", swToggle);
      stop.addEventListener("click", function () { swStop(false); });
      ui.sw[k] = { start: start, live: live, time: time, state: state, toggle: toggle, who: who };
      return ui.sw[k];
    }
    function swBar() {
      var label = el("span", { class: "tk-swbar-l" }), time = el("span", { class: "tk-sw-time", role: "timer" }), state = el("span", { class: "tk-sw-state" });
      var toggle = el("button", { type: "button", class: "fz-btn tk-small tk-sw-btn" }), stop = el("button", { type: "button", class: "fz-btn tk-small tk-sw-btn" });
      toggle.addEventListener("click", swToggle);
      stop.addEventListener("click", function () { swStop(false); });
      var retry = el("button", { type: "button", class: "fz-linkbtn", text: "Retry" }), drop = el("button", { type: "button", class: "fz-linkbtn", text: "Discard" });
      retry.addEventListener("click", function () { swMsg = null; paintSw(); swFlush(); });
      drop.addEventListener("click", function () {
        var it = swMsg && swMsg.item; swMsg = null;
        if (it) { swLoad(); swq = swq.filter(function (x) { return !(x.week === it.week && x.cat === it.cat && x.sub === it.sub && x.ms === it.ms); }); swStore(); }
        paintSw(); swFlush();
      });
      var msgT = el("span"), msg = el("div", { class: "tk-swbar-msg", role: "status" }, msgT, retry, drop);
      var main = el("div", { class: "tk-swbar-main" }, el("span", { class: "tk-sw-dot", "aria-hidden": "true" }), label, el("span", { class: "tk-swbar-t" }, time, state), el("span", { class: "tk-swbar-btns" }, toggle, stop));
      ui.swbar = { box: el("div", { class: "tk-swbar", hidden: true, role: "region", "aria-label": "Stopwatch" }, main, msg), main: main, label: label, time: time, state: state, toggle: toggle, stop: stop, msg: msg, msgT: msgT, retry: retry, drop: drop };
      return ui.swbar.box;
    }
    function paintSw() {
      if (!ui.sw) return;
      var now = Date.now(), k = sw ? hk(sw.cat, sw.sub) : null, mine = sw && sw.week === key && ui.sw[k] && ui.sw[k].start.isConnected ? ui.sw[k] : null;
      var running = !!(sw && sw.since != null), txt = sw ? T.fmtClock(swElapsed(sw, now)) : "";
      Object.keys(ui.sw).forEach(function (rk) {
        var r = ui.sw[rk], on = r === mine;
        r.start.hidden = on; r.live.hidden = !on;
        r.live.classList.toggle("paused", on && !running);
        if (!on) return;
        r.time.textContent = txt; r.state.textContent = running ? "" : "Paused";
        setBtn(r.toggle, running ? "pause" : "play", running ? "Pause" : "Resume", (running ? "Pause" : "Resume") + " stopwatch for " + r.who);
      });
      var b = ui.swbar;
      if (b) {
        var showMain = !!sw && !mine;
        b.main.hidden = !showMain;
        if (showMain) {
          b.box.classList.toggle("paused", !running);
          b.label.textContent = (running ? "Stopwatch running: " : "Stopwatch paused: ") + swName(sw) + swWeekNote(sw);
          b.time.textContent = txt; b.state.textContent = "";
          setBtn(b.toggle, running ? "pause" : "play", running ? "Pause" : "Resume", (running ? "Pause" : "Resume") + " stopwatch for " + swName(sw));
          setBtn(b.stop, "stop", "Stop", "Stop stopwatch for " + swName(sw) + " and add the time");
        }
        b.msg.hidden = !swMsg;
        if (swMsg) { b.msgT.textContent = swMsg.text; b.msg.classList.toggle("bad", swMsg.bad); b.retry.hidden = b.drop.hidden = !swMsg.item; }
        b.box.hidden = !showMain && !swMsg;
      }
      if (running && !swTimer) swTimer = setInterval(swTick, 1000);
      else if (!running && swTimer) { clearInterval(swTimer); swTimer = null; }
    }
    function swTick() {
      var before = sw && sw.week;
      swLoad();
      if (swRoll() || (sw && sw.week) !== before) { paintSw(); return; }
      if (!sw || sw.since == null) { paintSw(); return; }
      var txt = T.fmtClock(swElapsed(sw));
      var r = ui.sw[hk(sw.cat, sw.sub)];
      if (r && !r.live.hidden) r.time.textContent = txt;
      if (ui.swbar && !ui.swbar.main.hidden) ui.swbar.time.textContent = txt;
    }
    window.addEventListener("storage", function (e) {
      if (!uid || (e.key !== swKey() && e.key !== swKey(true) && e.key !== null)) return;
      swLoad(); paintSw();
    });
    document.addEventListener("visibilitychange", function () { if (document.hidden) return; if (sw) swTick(); checkFresh(); });

    function loggedOut(expired) {
      root.textContent = "";
      root.appendChild(el("div", { class: "tk-intro" },
        expired ? el("p", { class: "fz-error", text: "Your login has expired." }) : null,
        el("p", { text: "Track is a weekly log of your Japanese study: hours of active and passive immersion, phonetic training, output, soroban and Anki — split into your own sub-categories — plus your daily Kotu and Kyōtsu accuracy and a short summary of the week." }),
        el("p", { text: "Your weeks add up to charts on your profile, and (if you like) a place on the leaderboard." }),
        el("p", {}, el("a", { href: F.loginHref(), class: "fz-login-pill", text: "Log in" })),
        el("p", {}, el("a", { href: "/track/leaderboard/", text: "See the leaderboard →" }))));
    }

    F.me().then(function (m) {
      if (m.offline) { msg("Couldn't reach the server. Try again in a moment."); return; }
      if (!m.user) { loggedOut(false); return; }
      uid = m.user.id | 0;
      return F.api("/v1/track/config").then(function (c) {
        cfg = c || {}; cfg.subcats = cfg.subcats || [];
        cur = T.isKey(cfg.current_week) ? cfg.current_week : T.currentWeekKey();
        var q = new URLSearchParams(location.search).get("week"), k = cur;
        if (T.isKey(q)) { k = T.weekOfDate(q); if (k > cur) k = cur; if (k < "2000-01-01") k = "2000-01-01"; }
        swLoad();
        return loadWeek(k).then(function () { if (!st) return; swRoll(); swFlush(); paintSw(); });   // a stopwatch left running / time not yet saved
      }, function (e) { if (e.status === 401) loggedOut(true); else msg(e.message); });
    });
  };

  // =====================================================================================
  // Leaderboard
  // =====================================================================================
  // Only published weeks are ranked (the server stops at published_through): "last" is that one
  // week, the others end with it (or with an earlier ?week=). The current week appears once it ends.
  var PERIODS = [{ id: "last", label: "Last week", n: 1 }, { id: "4w", label: "4 weeks", n: 4 }, { id: "12w", label: "12 weeks", n: 12 },
    { id: "52w", label: "52 weeks", n: 52 }, { id: "all", label: "All time" }];
  var LB_PERIOD = "last", LB_METRIC = "active";   // defaults (left out of the URL)
  var METRICS = [{ id: "all", label: "All", long: "All categories", noun: "hours", color: "var(--tk-all)" },
    { id: "nopassive", label: "Exclude Passive", long: "All except passive immersion", noun: "hours outside passive immersion", color: "var(--tk-nopassive)" }]
    .concat(CATS.map(function (c) { return { id: c.id, label: c.short, long: c.label, noun: c.proper ? c.label : null, color: T.catColor(c.id) }; }))
    .concat([{ id: "kotu", label: "Kotu", long: "Kotu accuracy (average)", color: "var(--tk-kotu)", pct: true },
      { id: "kyotsu", label: "Kyōtsu", long: "Kyōtsu accuracy (average)", color: "var(--tk-kyotsu)", pct: true }]);
  var MET = {};
  METRICS.forEach(function (m) { MET[m.id] = m; });
  var RATING = {};   // 1..4 → {e: emoji, t: word}, shared with the logger (T.RATINGS)
  (T.RATINGS || [{ v: 1, e: "😞", t: "Rough" }, { v: 2, e: "😐", t: "So-so" }, { v: 3, e: "👌", t: "Good" }, { v: 4, e: "🔥", t: "Great" }])
    .forEach(function (r) { RATING[r.v] = { e: r.e, t: String(r.t).toLowerCase() }; });
  // hours → "h:mm" (API hours are exact hundredths, so whole minutes are exact)
  function hm(v) { if (!num(v)) return "—"; var m = Math.round(Math.max(0, v) * 60); return Math.floor(m / 60) + ":" + String(m % 60).padStart(2, "0"); }
  function signedPct(v) { return num(v) ? (v < 0 ? "−" : "+") + T.fmtNum(Math.abs(v), 1) + "%" : "—"; }
  function catOrder(ids) { var o = CATS.map(function (c) { return c.id; }); return ids.slice().sort(function (a, b) { return (o.indexOf(a) + 1 || 99) - (o.indexOf(b) + 1 || 99); }); }

  T.initLeaderboard = function (root) {
    T.loadMD();   // summaries (expanded rows + feed) render as Markdown
    var q = new URLSearchParams(location.search);
    var cur = T.currentWeekKey();
    var qp = q.get("period") === "week" ? "last" : q.get("period");   // old links: "This week" became "Last week"
    var period = PERIODS.some(function (p) { return p.id === qp; }) ? qp : LB_PERIOD;
    // not ?metric=__proto__/constructor; old ?metric=immersion falls back to the default
    var metric = Object.prototype.hasOwnProperty.call(MET, q.get("metric")) ? q.get("metric") : LB_METRIC;
    var week = T.isKey(q.get("week")) ? T.weekOfDate(q.get("week")) : null;   // null = the latest published week
    var pub = T.addWeeks(cur, -1);   // published_through until the server says otherwise
    if (week && week >= pub) week = null;
    var me = null, data = null, seq = 0, cache = {}, closed = {};   // rows start expanded; closed = the ones folded away

    var periodSeg = el("div", { class: "tk-seg", role: "group", "aria-label": "Period" });
    PERIODS.forEach(function (p) {
      var b = el("button", { type: "button", "aria-pressed": String(p.id === period), text: p.label, "data-v": p.id });
      b.addEventListener("click", function () { if (period === p.id) return; period = p.id; paintSeg(periodSeg, period); load(); });
      periodSeg.appendChild(b);
    });
    var metricSeg = el("div", { class: "tk-seg", role: "group", "aria-label": "Metric" });
    METRICS.forEach(function (m) {
      var b = el("button", { type: "button", "aria-pressed": String(m.id === metric), text: m.label, title: m.long, "data-v": m.id });
      b.addEventListener("click", function () { metric = m.id; paintSeg(metricSeg, metric); syncUrl(); paint(); });
      metricSeg.appendChild(b);
    });
    var date = el("input", { type: "date", class: "fz-input tk-input-sm", id: "lbDate", min: "2000-01-01", max: T.weekPickerMax(pub), "aria-label": "Period ends with the week containing this date (published weeks only)" });
    var latest = el("button", { type: "button", class: "fz-btn tk-small", text: "Latest" });
    date.addEventListener("change", function () {
      if (!T.isKey(date.value)) return;
      var k = T.weekOfLocalDate(date.value);
      week = k >= pub ? null : k;   // nothing after published_through
      load();
    });
    latest.addEventListener("click", function () { week = null; load(); });
    function paintSeg(seg, v) { seg.querySelectorAll("button").forEach(function (b) { b.setAttribute("aria-pressed", String(b.getAttribute("data-v") === v)); }); }

    var range = el("p", { class: "lb-range", role: "status" });
    var note = el("p", { class: "lb-note" });
    var tableBox = el("div", { class: "lb-table-wrap" });
    var g1 = el("div"), g2 = el("div"), g2note = el("p", { class: "fz-muted tk-small", hidden: true });
    var g1h = el("h3"), g2h = el("h3");
    var graphs = el("section", { class: "tk-card", "aria-label": "Graphs" },
      el("div", { class: "lb-graph" }, g1h, g1), el("div", { class: "lb-graph" }, g2h, g2note, g2));
    var goalsIntro = el("p", { class: "fz-muted tk-small lb-goals-intro" }), goalsBody = el("div");
    var goals = el("section", { class: "tk-card lb-goals", "aria-labelledby": "lbGoalsH", hidden: true },
      el("h2", { id: "lbGoalsH", text: "Goal achievers" }), goalsIntro, goalsBody);
    var feed = el("section", { class: "tk-card", "aria-label": "Recent summaries" });
    root.textContent = "";
    root.append(
      el("div", { class: "tk-card lb-controls" },
        el("div", { class: "lb-ctl" }, el("span", { text: "Period" }), periodSeg),
        el("div", { class: "lb-ctl" }, el("label", { for: "lbDate", class: "fz-muted", style: "font-size:0.8em;min-width:4.5em", text: "Ending" }), date, latest),
        el("div", { class: "lb-ctl" }, el("span", { text: "Metric" }), metricSeg)),
      range, note,
      el("section", { class: "tk-card", "aria-label": "Rankings" }, tableBox),
      goals, graphs, feed);
    var c1 = null, c2 = null;

    function syncUrl() {
      var u = new URL(location.href);
      if (period !== LB_PERIOD) u.searchParams.set("period", period); else u.searchParams.delete("period");
      if (week) u.searchParams.set("week", week); else u.searchParams.delete("week");
      if (metric !== LB_METRIC) u.searchParams.set("metric", metric); else u.searchParams.delete("metric");
      history.replaceState(null, "", u.pathname + u.search + u.hash);
    }

    function load() {
      syncUrl();
      date.value = week || pub; latest.disabled = !week;
      if (period !== "last") { goals.hidden = true; gseq++; }
      var ck = period + "|" + (week || "");
      var my = ++seq;
      tableBox.style.opacity = "0.5";
      // always send period: the server's default (4w) isn't ours
      var p = cache[ck] || F.api("/v1/track/leaderboard?period=" + period + (week ? "&week=" + week : ""));
      cache[ck] = p;
      p.then(function (d) {
        if (my !== seq) return;
        tableBox.style.opacity = "";
        data = d || { users: [], weeks: [] };
        if (T.isKey(data.published_through)) {
          pub = data.published_through;
          date.max = T.weekPickerMax(pub);
          if (week && week >= pub) { week = null; syncUrl(); }
          date.value = T.weekPickerValue(week || pub); latest.disabled = !week;
        }
        paint();
        loadGoals();
      }, function (e) {
        delete cache[ck];
        if (my !== seq) return;
        tableBox.style.opacity = "";
        tableBox.textContent = "";
        tableBox.appendChild(el("p", { class: "fz-error", text: e.message }));
      });
    }

    function value(u) {
      if (metric === "kotu" || metric === "kyotsu") return num(u[metric]) ? u[metric] : null;
      var t = u.totals || {};
      if (metric === "all" && !num(t.all)) return T.sumTotals(t);
      if (metric === "nopassive" && !num(t.nopassive)) return T.sumTotals(t) - (num(t.passive) ? t.passive : 0);
      return num(t[metric]) ? t[metric] : 0;
    }
    function shownWeek() { return (data && (data.to || (data.weeks && data.weeks[data.weeks.length - 1]))) || week || pub; }
    // "Sun 18 Oct, 04:00" in the viewer's own time zone
    function localTime(ms) { return new Date(ms).toLocaleString(undefined, { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" }); }

    function paintRange() {
      var P = PERIODS.filter(function (p) { return p.id === period; })[0];
      var to = shownWeek(), from = data.from || (data.weeks && data.weeks[0]) || to;
      var span = T.fmtWeekLong(from) + " → " + T.fmtWeekLong(to, 6);
      range.textContent = period === "last" ? (week ? "" : "Last week: ") + T.weekTitle(to) + " (" + T.fmtWeekDay(to) + " → " + T.fmtWeekDay(to, 6) + ")"
        : period === "all" ? "All time: " + span
        : P.label + ": " + span;
      // the running week is never shown until it ends (or the admin ends it early)
      var cw = T.isKey(data.current_week) ? data.current_week : cur, endMs = Date.parse(data.current_week_end);
      if (!isFinite(endMs)) endMs = T.weekStartMs(cw) + 7 * 864e5;
      note.textContent = data.ended_early
        ? "The admin released this week's results (week of " + T.fmtWeekDay(cw) + ") early, so they're included. The following week's results appear once it ends: " + localTime(endMs + 7 * 864e5) + " your time."
        : "This week's results (week of " + T.fmtWeekDay(cw) + ") appear once it ends: " + localTime(endMs) + " your time.";
    }

    function paint() {
      if (!data) return;
      var M = MET[metric], fmt = M.pct ? T.fmtPct : hm, isLast = period === "last", to = shownWeek();
      var P = PERIODS.filter(function (p) { return p.id === period; })[0];
      paintRange();

      var rows = (data.users || []).map(function (u) { return { u: u, v: value(u) }; })
        .filter(function (r) { return r.v != null && (M.pct || r.v > 0); })   // nobody ranked at 0 h
        .sort(function (a, b) { return b.v - a.v || String(a.u.user && a.u.user.name).localeCompare(String(b.u.user && b.u.user.name)); });
      var missing = (data.users || []).length - rows.length;

      // table: rank, name, [metric h:mm], Kotu, Kyōtsu, [weeks], details toggle
      tableBox.textContent = "";
      if (!rows.length) {
        tableBox.appendChild(el("p", { class: "fc-empty", text: (data.users || []).length ? "Nobody has logged " + (M.noun || M.long.toLowerCase()).replace(/ \(average\)$/, "") + " in this period." : "Nobody has logged anything in this period yet." }));
      } else {
        var sorted = function (id) { return metric === id ? "descending" : null; };
        var hr = el("tr", {},
          el("th", { scope: "col", text: "#" }), el("th", { scope: "col", text: "Name" }),
          M.pct ? null : el("th", { scope: "col", class: "num lb-sel", "aria-sort": "descending" }, M.label, el("span", { class: "lb-unit", text: " h:mm" })),
          el("th", { scope: "col", class: "num" + (metric === "kotu" ? " lb-sel" : ""), "aria-sort": sorted("kotu"), text: "Kotu" }),
          el("th", { scope: "col", class: "num" + (metric === "kyotsu" ? " lb-sel" : ""), "aria-sort": sorted("kyotsu"), text: "Kyōtsu" }),
          isLast ? null : el("th", { scope: "col", class: "num lb-wk", text: "Weeks" }),
          el("th", { scope: "col", class: "lb-tg" }, el("span", { class: "fz-sr", text: "Details" })));
        var ncol = hr.children.length;
        var tb = el("tbody"), rank = 0, prevV = null;
        rows.forEach(function (r, i) {
          if (r.v !== prevV) { rank = i + 1; prevV = r.v; }
          var u = r.u, uid = (u.user && u.user.id) | 0, mine = me && uid === me.id, name = (u.user && u.user.name) || "Someone";
          var detId = "lbd-" + uid;
          var det = el("tr", { class: "lb-det-row" + (mine ? " me" : ""), id: detId, hidden: !!closed[uid] }, el("td", { colspan: String(ncol) }));
          var btn = el("button", { type: "button", class: "lb-toggle", "aria-expanded": String(!closed[uid]), "aria-controls": detId, "aria-label": "Details for " + name, title: "Details" });
          var fill = function () { if (!det.firstChild.firstChild) det.firstChild.appendChild(detail(u, isLast)); };
          if (!closed[uid]) fill();
          btn.addEventListener("click", function () {
            var o = btn.getAttribute("aria-expanded") !== "true";
            if (o) fill();
            btn.setAttribute("aria-expanded", String(o));
            det.hidden = !o;
            if (o) delete closed[uid]; else closed[uid] = 1;
            row.classList.toggle("open", o);
          });
          var row = el("tr", { class: "lb-row" + (mine ? " me" : "") + (!closed[uid] ? " open" : "") },
            el("td", { class: "lb-rank num", text: String(rank) }),
            el("td", { class: "lb-name" }, T.userLink(u.user, 32)),
            M.pct ? null : el("td", { class: "num lb-sel", text: hm(r.v) }),
            el("td", { class: "num" + (metric === "kotu" ? " lb-sel" : ""), text: T.fmtPct(u.kotu) }),
            el("td", { class: "num" + (metric === "kyotsu" ? " lb-sel" : ""), text: T.fmtPct(u.kyotsu) }),
            isLast ? null : el("td", { class: "num lb-wk", text: String(u.weeks_logged | 0) }),
            el("td", { class: "lb-tg" }, btn));
          tb.append(row, det);
        });
        tableBox.appendChild(el("table", { class: "lb-table" }, el("thead", {}, hr), tb));
        if (missing) tableBox.appendChild(el("p", { class: "fz-muted tk-small", text: missing + " more " + (missing === 1 ? "person" : "people") + " logged this period but no " + (M.pct ? M.label + " scores." : (M.noun || M.long.toLowerCase()) + ".") }));
      }

      // graph 1: the metric per user
      var unit = M.pct ? "" : " (h:mm)";
      g1h.textContent = M.long + unit + " — " + (isLast ? "week of " + T.fmtWeekDay(to) : P.label.toLowerCase());
      var top = rows.slice(0, 20);
      var o1 = { rows: top.map(function (r) { return { label: (r.u.user && r.u.user.name) || "Someone", value: r.v, href: F.profileHref ? F.profileHref(r.u.user) : null, me: me && r.u.user && r.u.user.id === me.id, note: isLast ? null : (r.u.weeks_logged | 0) + " week" + ((r.u.weeks_logged | 0) === 1 ? "" : "s") + " logged" }; }),
        color: M.color, fmt: fmt, max: M.pct ? 100 : 0, valueLabel: M.pct ? M.label : M.label + " (h:mm)", ariaLabel: M.long + " per person" };
      if (c1) c1.update(o1); else c1 = FazonCharts.hbars(g1, o1);
      graphs.hidden = !rows.length;

      // graph 2: per week, top 8
      var weeks = data.weeks || [];
      g2h.textContent = M.long + unit + " per week — top " + Math.min(8, rows.length);
      if (weeks.length < 2) {
        g2note.hidden = false; g2note.textContent = "Pick a longer period to see week-by-week lines."; g2.hidden = true;
      } else {
        g2note.hidden = true; g2.hidden = false;
        var multiYear = weeks[0].slice(0, 4) !== weeks[weeks.length - 1].slice(0, 4) && weeks.length > 20;
        var ser = rows.slice(0, 8).map(function (r, i) {
          var s = (r.u.series && r.u.series[metric]) || [];
          return { label: (r.u.user && r.u.user.name) || "Someone", color: T.slotColor(i), values: weeks.map(function (_, j) { return num(s[j]) ? s[j] : null; }) };
        });
        var o2 = { x: weeks, xLabel: function (k) { return T.fmtKeyAxis(k, multiYear); }, xTitle: T.weekTitle, series: ser, yMax: M.pct ? 100 : null,
          fmt: function (v, tick) { return M.pct ? (tick ? v + "%" : T.fmtPct(v)) : hm(v); }, ariaLabel: M.long + " per week, top people" };
        if (c2) c2.update(o2); else c2 = FazonCharts.lines(g2, o2);
      }
    }

    // ---- one person's expanded row: totals, per-category breakdown (+ ratings), summary ----
    function detail(u, isLast) {
      var t = u.totals || {}, all = num(t.all) ? t.all : T.sumTotals(t);
      var nop = num(t.nopassive) ? t.nopassive : all - (num(t.passive) ? t.passive : 0);
      var stat = function (k, v, cls) { return el("div", { class: "lb-stat" + (cls ? " " + cls : "") }, el("dt", { text: k }), el("dd", { text: v })); };
      var stats = el("dl", { class: "lb-stats" },
        stat("All", hm(all), "lb-all"), stat("Exclude Passive", hm(nop), "lb-nop"),
        stat("Kotu", T.fmtPct(u.kotu)), stat("Kyōtsu", T.fmtPct(u.kyotsu)),
        isLast ? null : stat("Weeks logged", String(u.weeks_logged | 0)));
      var bar = el("div", { class: "lb-stack", "aria-hidden": "true" }), list = el("ul", { class: "lb-cats" + (isLast ? " rated" : ""), "aria-label": "Hours per category (h:mm)" });
      CATS.forEach(function (c) {
        var v = num(t[c.id]) ? t[c.id] : 0, rt = isLast && u.ratings ? RATING[u.ratings[c.id]] : null;
        if (v > 0 && all > 0) { var sg = el("span", { title: c.short + " " + hm(v) }); sg.style.background = T.catColor(c.id); sg.style.flexGrow = String(v); bar.appendChild(sg); }
        var dot = el("span", { class: "tk-dotc", "aria-hidden": "true" }); dot.style.background = T.catColor(c.id);
        list.appendChild(el("li", { class: v > 0 ? null : "zero" }, dot,
          el("span", { class: "lb-cat-n", title: c.label, text: c.short }),
          el("span", { class: "lb-cat-v", text: hm(v) }),
          isLast ? el("span", rt ? { class: "lb-rate", role: "img", "aria-label": "self-rating: " + rt.t, title: "Self-rating: " + rt.t, text: rt.e } : { class: "lb-rate" }) : null));
      });
      var sum;
      if (u.summary && String(u.summary).trim()) {
        var txt = el("div", { class: "sm-text" });
        T.mdFill(txt, String(u.summary));   // Markdown (sanitized); shown in full here (the feed clamps long ones)
        sum = el("div", { class: "lb-sum" }, el("p", { class: "lb-sum-h", text: isLast || !T.isKey(u.summary_week) ? "Summary" : "Summary — week of " + T.fmtWeekDay(u.summary_week) }), txt);
      } else sum = el("p", { class: "fz-muted tk-small lb-sum-none", text: isLast ? "No public summary for this week." : "No public summary in this period." });
      return el("div", { class: "lb-det" }, stats, all > 0 ? bar : null, list, sum);
    }

    // ---- goal achievers (Last week only) ----
    var gcache = {}, gseq = 0;
    function loadGoals() {
      if (period !== "last" || !data) { goals.hidden = true; return; }
      var wk = shownWeek(), my = ++gseq;
      goals.hidden = false;
      goalsIntro.textContent = "Who met the goals they set for the week of " + T.fmtWeekLong(wk) + ", ranked by how far they beat them (all goal hours together). Meeting every goal ranks above meeting only some, whatever the percentage.";
      goalsBody.style.opacity = "0.5";
      var p = gcache[wk] || F.api("/v1/track/leaderboard/goals?week=" + wk);
      gcache[wk] = p;
      p.then(function (d) {
        if (my !== gseq) return;
        goalsBody.style.opacity = "";
        paintGoals((d && d.users) || []);
      }, function (e) {
        delete gcache[wk];
        if (my !== gseq) return;
        goalsBody.style.opacity = "";
        goalsBody.textContent = "";
        goalsBody.appendChild(el("p", { class: "fz-error", text: e.message }));
      });
    }
    function paintGoals(users) {
      goalsBody.textContent = "";
      if (!users.length) { goalsBody.appendChild(el("p", { class: "fc-empty", text: "Nobody met any of their goals that week." })); return; }
      var rank = 0;   // the server's order: tier 1 first, then by percentage
      [{ t1: true, h: "Met all their goals" }, { t1: false, h: "Met some of their goals" }].forEach(function (tier) {
        var us = users.filter(function (u) { return (u.tier != null ? u.tier === 1 : !!u.all_met) === tier.t1; });
        if (!us.length) return;
        var ol = el("ol", { class: "lb-gl" });
        us.forEach(function (u) { ol.appendChild(goalItem(u, ++rank)); });
        goalsBody.append(el("div", { class: "lb-tier" + (tier.t1 ? "" : " t2") }, el("h3", { text: tier.h }), ol));
      });
    }
    function goalItem(u, rank) {
      var uid = (u.user && u.user.id) | 0, id = "lbg-" + uid, name = (u.user && u.user.name) || "Someone";
      var det = el("ul", { class: "lb-gd", id: id, "aria-label": "Goals" });   // expanded by default
      catOrder(Object.keys(u.goals || {})).forEach(function (c) {
        var g = u.goals[c], l = u.logged && num(u.logged[c]) ? u.logged[c] : 0, met = num(g) && l >= g;
        var dot = el("span", { class: "tk-dotc", "aria-hidden": "true" }); dot.style.background = T.catColor(c);
        det.appendChild(el("li", { class: met ? "met" : "miss" }, dot,
          el("span", { class: "lb-gd-n", text: CAT[c] ? CAT[c].short : c }),
          el("span", { class: "lb-gd-v" }, el("b", { text: hm(l) }), " logged / " + hm(g) + " goal"),
          el("span", { class: "lb-gd-m", role: "img", "aria-label": met ? "met" : "not met", title: met ? "Met" : "Not met", text: met ? "✓" : "✗" })));
      });
      var btn = el("button", { type: "button", class: "lb-toggle", "aria-expanded": "true", "aria-controls": id, "aria-label": "Goal details for " + name, title: "Goal details" });
      var li = el("li", { class: "lb-gi open" + (me && uid === me.id ? " me" : "") },
        el("div", { class: "lb-gi-row" },
          el("span", { class: "lb-rank num", text: String(rank) }),
          el("span", { class: "lb-gi-name" }, T.userLink(u.user, 28)),
          el("span", { class: "lb-gi-stats" },
            el("b", { class: "lb-gpct" + (num(u.percent) && u.percent < 0 ? " neg" : ""), title: "Logged in the goal categories vs the goals' total", text: signedPct(u.percent) }),
            el("span", { class: "lb-gcnt", text: (u.goals_met | 0) + "/" + (u.goals_set | 0) + " goals" })),
          btn), det);
      btn.addEventListener("click", function () {
        var o = btn.getAttribute("aria-expanded") !== "true";
        btn.setAttribute("aria-expanded", String(o));
        det.hidden = !o;
        li.classList.toggle("open", o);
      });
      return li;
    }

    // ---- recent summaries feed ----
    var LIMIT = 20, seen = {}, oldest = null, list = el("div", { class: "sm-list" });
    var more = el("button", { type: "button", class: "fz-btn", text: "Load more", hidden: true });
    var feedMsg = el("p", { class: "fz-muted tk-small" });
    feed.append(el("h2", { text: "Recent summaries" }), feedMsg, list, el("div", { class: "sm-foot" }, more));
    function loadFeed(inclusive) {
      more.disabled = true;
      // `before` is exclusive; ask from the oldest week shown (inclusive, de-duplicated) so a week
      // split across two pages isn't skipped
      var before = oldest ? (inclusive ? T.addWeeks(oldest, 1) : oldest) : null;
      return F.api("/v1/track/summaries?limit=" + LIMIT + (before ? "&before=" + before : "")).then(function (d) {
        var items = (d && d.summaries) || [], added = 0;
        items.forEach(function (it) {
          var id = ((it.user && it.user.id) | 0) + "|" + it.week;
          if (seen[id]) return;
          seen[id] = 1; added++;
          list.appendChild(T.summaryCard(it, { fmtH: hm }));
          if (!oldest || it.week < oldest) oldest = it.week;
        });
        if (!added && items.length >= LIMIT && inclusive) return loadFeed(false);
        more.hidden = items.length < LIMIT; more.disabled = false;
        if (!list.children.length) feedMsg.textContent = "No public summaries yet.";
        else feedMsg.textContent = "";
      }, function (e) { more.disabled = false; feedMsg.textContent = e.message; });
    }
    more.addEventListener("click", function () { loadFeed(true); });

    F.me().then(function (m) { me = m && m.user; load(); loadFeed(false); });
  };
})();
