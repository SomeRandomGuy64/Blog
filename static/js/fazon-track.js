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
    var ui = {}, stripData = null, stripRange = null, openMenu = null, showErrs = false;

    function msg() {
      root.textContent = "";
      root.appendChild(el.apply(null, ["p", { class: "tk-msg" }].concat([].slice.call(arguments))));
    }
    function subs(cat) {
      return (cfg.subcats || []).filter(function (s) { return s.category === cat; })
        .sort(function (a, b) { return (a.position || 0) - (b.position || 0) || a.id - b.id; });
    }
    function subById(id) { return (cfg.subcats || []).filter(function (s) { return s.id === id; })[0] || null; }

    function fromServer(w) {
      var s = { hours: {}, kotu: [], kyotsu: [], summary: w.summary || "", priv: !!w.summary_private };
      (w.hours || []).forEach(function (h) { if (num(h.hours) && h.hours > 0) s.hours[hk(h.category, h.subcat)] = inputH(h.hours); });
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
      out.norm = JSON.stringify([normH, out.kotu, out.kyotsu, st.summary.trim(), st.priv]);
      out.kotuAvg = T.mean(out.kotu);
      out.kyotsuAvg = T.mean(out.kyotsu.map(function (v, i) { return num(v) ? v : (wk.kyotsu_auto ? wk.kyotsu_auto[i] : null); }));
      return out;
    }
    function dirty() { return !!st && compute().norm !== base; }

    // ---- week navigation ----
    function go(k) {
      if (!T.isKey(k)) return;
      if (k > cur) k = cur;
      if (k < "2000-01-01") k = "2000-01-01";
      if (k === key) return;
      if (dirty() && !confirm("You have unsaved changes for the week of " + T.fmtKeyLong(key) + ". Discard them?")) return;
      loadWeek(k);
    }
    function loadWeek(k) {
      if (ui.form) ui.form.style.opacity = "0.5";
      return F.api("/v1/track/weeks/" + k).then(function (w) {
        wk = w; key = w.week || k; st = fromServer(w); base = compute().norm;
        savedAt = null; saveErr = ""; showErrs = false;
        var u = new URL(location.href);
        if (key === cur) u.searchParams.delete("week"); else u.searchParams.set("week", key);
        history.replaceState(null, "", u.pathname + u.search + u.hash);
        render();
        loadStrip(false);
      }, function (e) {
        if (ui.form) ui.form.style.opacity = "";
        if (e.status === 401) { loggedOut(true); return; }
        if (!wk) msg(e.message); else alert(e.message);
      });
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
          el("span", { text: T.fmtKeyShort(k) }), el("b", { text: w ? (tot ? T.fmtNum(tot, 1) + "h" : "✓") : "—" }));
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
      ui = { cats: {}, fields: {}, errs: {}, tot: {}, catErr: {} };

      // navigator
      var prev = el("button", { type: "button", class: "tk-nav-btn", "aria-label": "Previous week", text: "‹", disabled: key <= "2000-01-01" });
      var next = el("button", { type: "button", class: "tk-nav-btn", "aria-label": "Next week", text: "›", disabled: key >= cur });
      prev.addEventListener("click", function () { go(T.addWeeks(key, -1)); });
      next.addEventListener("click", function () { go(T.addWeeks(key, 1)); });
      var date = el("input", { type: "date", class: "fz-input tk-input-sm", value: key, min: "2000-01-01", max: T.addDays(cur, 6), "aria-label": "Jump to the week containing a date" });
      date.addEventListener("change", function () {
        if (!T.isKey(date.value)) return;
        var k = T.weekOfDate(date.value);
        if (k > cur) { k = cur; }
        if (k === key) { date.value = key; return; }
        go(k);
        if (key !== k) date.value = key;   // navigation cancelled
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
      var bar = el("div", { class: "tk-savebar" }, ui.status, ui.save);

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

      root.append(nav, ui.form, bar, settings);
      paintStrip();
      refresh();
      if (focusF) { var f = root.querySelector('[data-f="' + focusF + '"]'); if (f) f.focus({ preventScroll: true }); }
    }

    function catSection(c) {
      var sec = el("section", { class: "tk-card tk-cat", "aria-label": c.label });
      var tot = ui.tot[c.id] = el("span", { class: "tk-cat-tot" });
      sec.appendChild(el("div", { class: "tk-cat-h" }, el("h3", {}, (function () { var d = el("span", { class: "tk-dotc" }); d.style.background = T.catColor(c.id); return d; })(), c.label), tot));
      var all = subs(c.id), live = all.filter(function (s) { return !s.archived; });
      var archived = all.filter(function (s) { return s.archived; });
      // archived ones still show when this week has hours for them; unknown ids too
      var shown = live.slice();
      archived.forEach(function (s) { if (st.hours[hk(c.id, s.id)]) shown.push(s); });
      Object.keys(st.hours).forEach(function (k) {
        var p = k.split("|");
        if (p[0] === c.id && p[1] !== "none" && st.hours[k] && !subById(+p[1])) shown.push({ id: +p[1], category: c.id, name: "Sub-category #" + p[1], archived: true, unknown: true });
      });
      shown.forEach(function (s) { sec.appendChild(row(c, s)); });
      sec.appendChild(row(c, null, live.length || shown.length ? "Other / unassigned" : "Hours"));
      sec.appendChild(ui.catErr[c.id] = el("div", { class: "tk-err", role: "alert" }));

      // footer: + add, archived list
      var foot = el("div", { class: "tk-cat-foot" });
      var addBtn = el("button", { type: "button", class: "fz-linkbtn", text: "+ Add sub-category" });
      foot.appendChild(addBtn);
      addBtn.addEventListener("click", function () {
        if (live.length >= SUB_MAX) { alert("You can have up to " + SUB_MAX + " sub-categories here. Archive one first."); return; }
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
          if (live.some(function (s) { return s.name.toLowerCase() === name.toLowerCase(); })) { err.textContent = "You already have “" + name + "” here."; return; }
          ok.disabled = true; err.textContent = "";
          F.api("/v1/track/subcats", { method: "POST", body: { category: c.id, name: name } }).then(function (s) {
            cfg.subcats.push(s); rerenderCat(c);
            var f = ui.cats[c.id].querySelector('[data-f="h:' + hk(c.id, s.id) + '"]'); if (f) f.focus();
          }, function (e) { ok.disabled = false; err.textContent = e.message; });
        }
        ok.addEventListener("click", submit);
        inp.addEventListener("keydown", function (e) { if (e.key === "Enter") { e.preventDefault(); submit(); } else if (e.key === "Escape") { e.preventDefault(); no.click(); } });
      });
      if (archived.length) {
        var list = el("div", { class: "tk-arch", hidden: true });
        var tog = el("button", { type: "button", class: "fz-linkbtn", "aria-expanded": "false", text: "Archived (" + archived.length + ")" });
        tog.addEventListener("click", function () { list.hidden = !list.hidden; tog.setAttribute("aria-expanded", String(!list.hidden)); });
        archived.forEach(function (s) {
          var un = el("button", { type: "button", class: "fz-linkbtn", text: "Unarchive" });
          var del = el("button", { type: "button", class: "fz-linkbtn", text: "Delete" });
          un.addEventListener("click", function () { patchSub(c, s, { archived: false }); });
          del.addEventListener("click", function () { deleteSub(c, s); });
          list.appendChild(el("div", { class: "tk-arch-row" }, el("span", { text: s.name }), un, del));
        });
        foot.append(tog, list);
      }
      sec.appendChild(foot);
      return sec;
    }
    function rerenderCat(c) {
      var old = ui.cats[c.id], fresh = catSection(c);
      old.replaceWith(fresh); ui.cats[c.id] = fresh; refresh();
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
      var name = el("span", { class: "tk-row-name" + (s ? "" : " none") }, s ? s.name : noneLabel,
        s && s.archived ? el("span", { class: "tk-tag", text: "archived" }) : null);
      var r = el("div", { class: "tk-row" }, name, inp, el("span", { class: "tk-unit", text: "h" }));
      if (s && !s.unknown) {
        var btn = el("button", { type: "button", class: "tk-menu-btn", "aria-haspopup": "true", "aria-expanded": "false", "aria-label": "Manage " + s.name, text: "⋯" });
        var menu = el("div", { class: "tk-menu", hidden: true, role: "menu" });
        var mk = function (label, fn, cls) { var b = el("button", { type: "button", role: "menuitem", class: cls || null, text: label }); b.addEventListener("click", function () { closeMenu(); fn(); }); menu.appendChild(b); };
        mk("Rename", function () { renameSub(c, s, name, r); });
        mk(s.archived ? "Unarchive" : "Archive", function () { patchSub(c, s, { archived: !s.archived }); });
        mk("Delete", function () { deleteSub(c, s); }, "danger");
        btn.addEventListener("click", function (e) {
          e.stopPropagation();
          var was = openMenu && openMenu.menu === menu; closeMenu();
          if (!was) { menu.hidden = false; btn.setAttribute("aria-expanded", "true"); openMenu = { menu: menu, btn: btn, wrap: r }; var b0 = menu.querySelector("button"); if (b0) b0.focus(); }
        });
        r.append(btn, menu);
      } else r.appendChild(el("span", { class: "tk-menu-btn ph", "aria-hidden": "true" }));
      ui.errs[f] = el("div", { class: "tk-err", role: "alert" });
      var wrap = el("div", {}, r, ui.errs[f]);
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
        F.api("/v1/track/subcats/" + s.id, { method: "POST", body: { name: v } }).then(function (n) {
          Object.assign(s, n); rerenderCat(c);
        }, function (e) { ok.disabled = false; err.textContent = e.message; });
      }
      ok.addEventListener("click", submit);
      inp.addEventListener("keydown", function (e) { if (e.key === "Enter") { e.preventDefault(); submit(); } else if (e.key === "Escape") { e.preventDefault(); close(); } });
    }
    function patchSub(c, s, body) {
      if (body.archived === false && subs(c.id).filter(function (x) { return !x.archived; }).length >= SUB_MAX) { alert("You can have up to " + SUB_MAX + " active sub-categories here. Archive another one first."); return; }
      return F.api("/v1/track/subcats/" + s.id, { method: "POST", body: body }).then(function (n) {
        Object.assign(s, n); rerenderCat(c);
      }, function (e) { alert(e.message); });
    }
    function deleteSub(c, s) {
      var v = st.hours[hk(c.id, s.id)];
      if (v && parseHours(v).v) { alert("“" + s.name + "” has hours in this week. Clear them (and save) first, or archive it instead."); return; }
      if (!confirm("Delete “" + s.name + "”? This can't be undone.")) return;
      F.api("/v1/track/subcats/" + s.id, { method: "DELETE" }).then(function () {
        cfg.subcats = cfg.subcats.filter(function (x) { return x.id !== s.id; });
        delete st.hours[hk(c.id, s.id)];
        rerenderCat(c);
      }, function (e) {
        if (e.status === 409) {
          if (s.archived) { alert("“" + s.name + "” has hours logged in other weeks, so it can't be deleted. It stays archived — its hours remain in past weeks and charts."); return; }
          if (confirm("“" + s.name + "” has hours logged in other weeks, so it can't be deleted.\n\nArchive it instead? It disappears from the form for new weeks; its hours stay in past weeks and charts.")) patchSub(c, s, { archived: true });
        } else alert(e.message);
      });
    }

    function scoresSection() {
      var sec = el("section", { class: "tk-card", "aria-label": "Daily scores" });
      sec.appendChild(el("div", { class: "tk-cat-h" }, el("h3", { text: "Daily scores" })));
      sec.appendChild(el("p", { class: "fz-muted tk-small", style: "margin:0 0 10px" },
        "Total accuracy per day: ", el("a", { href: "https://kotu.io", target: "_blank", rel: "noopener", text: "Kotu" }),
        " (enter it yourself) and ", el("a", { href: "/kyotsu/", text: "Kyōtsu" }),
        ". Blank Kyōtsu days use your saved Kyōtsu sessions automatically."));
      var grid = el("div", { class: "tk-scores" },
        el("span", { class: "hd", text: "Day" }), el("span", { class: "hd", style: "text-align:right", text: "Kotu %" }), el("span", { class: "hd", style: "text-align:right", text: "Kyōtsu %" }));
      var days = wk.days && wk.days.length === 7 ? wk.days : [0, 1, 2, 3, 4, 5, 6].map(function (i) { return T.addDays(key, i); });
      var auto = wk.kyotsu_auto || [], ans = wk.kyotsu_auto_answers || [];
      days.forEach(function (d, i) {
        grid.appendChild(el("span", { class: "tk-day", text: T.fmtDayLabel(d) }));
        ["kotu", "kyotsu"].forEach(function (f) {
          var a = f === "kyotsu" && num(auto[i]) ? auto[i] : null;
          var inp = el("input", { type: "text", inputmode: "decimal", autocomplete: "off", class: "fz-input tk-sc", "data-f": f + ":" + i, value: st[f][i] || "",
            placeholder: a != null ? T.fmtNum(a, 1) : "–", "aria-label": (f === "kotu" ? "Kotu" : "Kyōtsu") + " % on " + T.fmtDayLabel(d) + (a != null ? " (auto: " + T.fmtNum(a, 1) + "%)" : "") });
          inp.addEventListener("input", function () { st[f][i] = inp.value; refresh(); });
          inp.addEventListener("blur", function () { var p = parseScore(inp.value); if (!p.err && inp.value) { inp.value = p.v == null ? "" : String(p.v); st[f][i] = inp.value; } showErrs = true; refresh(); });
          ui.fields[f + ":" + i] = inp;
          var ht = f === "kyotsu" && ans[i] ? ans[i] + " answers" + (st.kyotsu[i] ? " · auto " + T.fmtNum(a, 1) + "%" : "") : "";
          var hint = el("span", { class: "tk-hint", text: ht, title: ht || null });
          ui.errs[f + ":" + i] = el("span", { class: "tk-err", role: "alert" });
          grid.appendChild(el("span", { class: "tk-sc-cell" }, inp, hint, ui.errs[f + ":" + i]));
        });
      });
      ui.kotuAvg = el("span", { class: "tk-avg" }); ui.kyotsuAvg = el("span", { class: "tk-avg" });
      grid.append(el("span", { class: "tk-day", style: "font-weight:700", text: "Average" }), ui.kotuAvg, ui.kyotsuAvg);
      sec.appendChild(grid);
      var anyAuto = auto.some(num);
      var use = el("button", { type: "button", class: "fz-btn tk-small", text: "Use my Kyōtsu results", disabled: !anyAuto,
        title: anyAuto ? "Fill the Kyōtsu column from your saved trainer sessions this week" : "No saved Kyōtsu sessions this week" });
      use.addEventListener("click", function () {
        auto.forEach(function (v, i) { if (num(v)) { st.kyotsu[i] = String(Math.round(v * 10) / 10); var f = ui.fields["kyotsu:" + i]; if (f) f.value = st.kyotsu[i]; } });
        refresh();
      });
      sec.appendChild(el("div", { class: "tk-scores-foot" }, use,
        el("span", { class: "fz-muted", text: anyAuto ? auto.filter(num).length + " day" + (auto.filter(num).length === 1 ? "" : "s") + " with saved sessions (" + ans.reduce(function (a, b) { return a + (b | 0); }, 0) + " answers)" : "No saved Kyōtsu sessions this week" })));
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
      sec.append(el("div", { class: "tk-cat-h" }, el("h3", { text: "Weekly summary" })), ui.sumNote, ta,
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
      if (saving) ui.status.textContent = "Saving…";
      else if (!r.ok) { ui.status.textContent = "Fix the highlighted fields to save."; ui.status.classList.add("bad"); }
      else if (saveErr && d) { ui.status.textContent = saveErr; ui.status.classList.add("bad"); }
      else if (d) { ui.status.textContent = "● Unsaved changes"; ui.status.classList.add("dirty"); }
      else ui.status.textContent = savedAt ? "Saved ✓ " + savedAt : wk.updated ? "Saved · last edited " + F.relTime(wk.updated) : "Nothing logged for this week yet.";
      ui.save.disabled = saving || !r.ok || !d;
    }

    function save() {
      if (saving || !st) return;
      var r = compute();
      showErrs = true;
      if (!r.ok) { refresh(); var bad = root.querySelector(".bad"); if (bad && bad.focus) bad.focus(); return; }
      if (r.norm === base) return;
      saving = true; saveErr = ""; refresh();
      var body = { hours: r.hours, kotu: r.kotu, kyotsu: r.kyotsu, summary: st.summary.trim(), summary_private: !!st.priv };
      F.api("/v1/track/weeks/" + key, { method: "POST", body: body }).then(function (w) {
        saving = false; wk = w; st = fromServer(w); base = compute().norm;
        savedAt = new Date().toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
        render(); loadStrip(true);
      }, function (e) {
        saving = false;
        if (e.status === 401) saveErr = "Your login has expired — log in again in another tab, then save.";
        else saveErr = "Couldn't save: " + e.message;
        refresh();
      });
    }

    function loggedOut(expired) {
      root.textContent = "";
      root.appendChild(el("div", { class: "tk-intro" },
        expired ? el("p", { class: "fz-error", text: "Your login has expired." }) : null,
        el("p", { text: "Track is a weekly log of your Japanese study: hours of active and passive immersion, phonetic training, output and soroban — split into your own sub-categories — plus your daily Kotu and Kyōtsu accuracy and a short summary of the week." }),
        el("p", { text: "Your weeks add up to charts on your profile, and (if you like) a place on the leaderboard." }),
        el("p", {}, el("a", { href: F.loginHref(), class: "fz-login-pill", text: "Log in" })),
        el("p", {}, el("a", { href: "/track/leaderboard/", text: "See the leaderboard →" }))));
    }

    F.me().then(function (m) {
      if (m.offline) { msg("Couldn't reach the server. Try again in a moment."); return; }
      if (!m.user) { loggedOut(false); return; }
      return F.api("/v1/track/config").then(function (c) {
        cfg = c || {}; cfg.subcats = cfg.subcats || [];
        cur = T.isKey(cfg.current_week) ? cfg.current_week : T.currentWeekKey();
        var q = new URLSearchParams(location.search).get("week"), k = cur;
        if (T.isKey(q)) { k = T.weekOfDate(q); if (k > cur) k = cur; if (k < "2000-01-01") k = "2000-01-01"; }
        return loadWeek(k);
      }, function (e) { if (e.status === 401) loggedOut(true); else msg(e.message); });
    });
  };

  // =====================================================================================
  // Leaderboard
  // =====================================================================================
  var PERIODS = [{ id: "week", label: "This week", n: 1 }, { id: "4w", label: "4 weeks", n: 4 }, { id: "12w", label: "12 weeks", n: 12 },
    { id: "52w", label: "52 weeks", n: 52 }, { id: "all", label: "All time" }];
  var METRICS = [{ id: "immersion", label: "Immersion", long: "Immersion (active + passive)", color: "var(--fz-accent)" }]
    .concat(CATS.map(function (c) { return { id: c.id, label: c.short, long: c.label, color: T.catColor(c.id) }; }))
    .concat([{ id: "kotu", label: "Kotu", long: "Kotu accuracy (average)", color: "var(--tk-kotu)", pct: true },
      { id: "kyotsu", label: "Kyōtsu", long: "Kyōtsu accuracy (average)", color: "var(--tk-kyotsu)", pct: true }]);
  var MET = {};
  METRICS.forEach(function (m) { MET[m.id] = m; });

  T.initLeaderboard = function (root) {
    var q = new URLSearchParams(location.search);
    var cur = T.currentWeekKey();
    var period = PERIODS.some(function (p) { return p.id === q.get("period"); }) ? q.get("period") : "4w";
    var metric = MET[q.get("metric")] ? q.get("metric") : "immersion";
    var week = T.isKey(q.get("week")) ? T.weekOfDate(q.get("week")) : null;   // null = current
    var me = null, data = null, seq = 0, cache = {};

    var periodSeg = el("div", { class: "tk-seg", role: "group", "aria-label": "Period" });
    PERIODS.forEach(function (p) {
      var b = el("button", { type: "button", "aria-pressed": String(p.id === period), text: p.label, "data-v": p.id });
      b.addEventListener("click", function () { period = p.id; paintSeg(periodSeg, period); load(); });
      periodSeg.appendChild(b);
    });
    var metricSeg = el("div", { class: "tk-seg", role: "group", "aria-label": "Metric" });
    METRICS.forEach(function (m) {
      var b = el("button", { type: "button", "aria-pressed": String(m.id === metric), text: m.label, title: m.long, "data-v": m.id });
      b.addEventListener("click", function () { metric = m.id; paintSeg(metricSeg, metric); syncUrl(); paint(); });
      metricSeg.appendChild(b);
    });
    var date = el("input", { type: "date", class: "fz-input tk-input-sm", id: "lbDate", min: "2000-01-01", max: T.addDays(cur, 6), "aria-label": "Period ends with the week containing this date" });
    var latest = el("button", { type: "button", class: "fz-btn tk-small", text: "Latest" });
    date.addEventListener("change", function () { if (!T.isKey(date.value)) return; var k = T.weekOfDate(date.value); week = k >= cur ? null : k; load(); });
    latest.addEventListener("click", function () { week = null; load(); });
    function paintSeg(seg, v) { seg.querySelectorAll("button").forEach(function (b) { b.setAttribute("aria-pressed", String(b.getAttribute("data-v") === v)); }); }

    var range = el("p", { class: "lb-range", role: "status" });
    var tableBox = el("div", { class: "lb-table-wrap" });
    var g1 = el("div"), g2 = el("div"), g2note = el("p", { class: "fz-muted tk-small", hidden: true });
    var g1h = el("h3"), g2h = el("h3");
    var graphs = el("section", { class: "tk-card", "aria-label": "Graphs" },
      el("div", { class: "lb-graph" }, g1h, g1), el("div", { class: "lb-graph" }, g2h, g2note, g2));
    var feed = el("section", { class: "tk-card", "aria-label": "Recent summaries" });
    root.textContent = "";
    root.append(
      el("div", { class: "tk-card lb-controls" },
        el("div", { class: "lb-ctl" }, el("span", { text: "Period" }), periodSeg),
        el("div", { class: "lb-ctl" }, el("label", { for: "lbDate", class: "fz-muted", style: "font-size:0.8em;min-width:4.5em", text: "Ending" }), date, latest),
        el("div", { class: "lb-ctl" }, el("span", { text: "Metric" }), metricSeg)),
      range,
      el("section", { class: "tk-card", "aria-label": "Rankings" }, tableBox),
      graphs, feed);
    var c1 = null, c2 = null;

    function syncUrl() {
      var u = new URL(location.href);
      u.searchParams.set("period", period);
      if (week) u.searchParams.set("week", week); else u.searchParams.delete("week");
      if (metric !== "immersion") u.searchParams.set("metric", metric); else u.searchParams.delete("metric");
      history.replaceState(null, "", u.pathname + u.search + u.hash);
    }

    function load() {
      syncUrl();
      var end = week || cur;
      date.value = end; latest.disabled = !week;
      var ck = period + "|" + (week || "");
      var my = ++seq;
      root.classList.add("loading");
      tableBox.style.opacity = "0.5";
      var p = cache[ck] || F.api("/v1/track/leaderboard?period=" + period + (week ? "&week=" + week : ""));
      cache[ck] = p;
      p.then(function (d) {
        if (my !== seq) return;
        tableBox.style.opacity = "";
        data = d || { users: [], weeks: [] };
        paint();
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
      if (metric === "immersion" && !num(t.immersion)) return (num(t.active) ? t.active : 0) + (num(t.passive) ? t.passive : 0);
      return num(t[metric]) ? t[metric] : 0;
    }
    function paint() {
      if (!data) return;
      var M = MET[metric], fmt = M.pct ? T.fmtPct : T.fmtH;
      var from = data.from || (data.weeks && data.weeks[0]), to = data.to || week || cur;
      var P = PERIODS.filter(function (p) { return p.id === period; })[0];
      range.textContent = period === "week" ? T.weekTitle(to) + (to === cur ? " (this week)" : "")
        : period === "all" ? "All time, up to " + T.fmtKeyLong(T.addDays(to, 6))
        : P.label + ": " + (from ? T.fmtKeyLong(from) : "") + " → " + T.fmtKeyLong(T.addDays(to, 6));

      var rows = (data.users || []).map(function (u) { return { u: u, v: value(u) }; })
        .filter(function (r) { return r.v != null && (M.pct || r.v > 0); })   // nobody ranked at 0 h
        .sort(function (a, b) { return b.v - a.v || String(a.u.user && a.u.user.name).localeCompare(String(b.u.user && b.u.user.name)); });
      var missing = (data.users || []).length - rows.length;

      // table
      tableBox.textContent = "";
      if (!rows.length) {
        tableBox.appendChild(el("p", { class: "fc-empty", text: (data.users || []).length ? "Nobody has logged " + M.long.toLowerCase().replace(/ \(average\)$/, "") + " in this period." : "Nobody has logged anything in this period yet." }));
      } else {
        var tb = el("tbody"), rank = 0, prevV = null;
        rows.forEach(function (r, i) {
          if (r.v !== prevV) { rank = i + 1; prevV = r.v; }
          var mine = me && r.u.user && r.u.user.id === me.id;
          tb.appendChild(el("tr", { class: mine ? "me" : null },
            el("td", { class: "lb-rank num", text: String(rank) }),
            el("td", {}, T.userLink(r.u.user, 32)),
            el("td", { class: "num", text: fmt(r.v) }),
            el("td", { class: "num", text: String(r.u.weeks_logged | 0) })));
        });
        tableBox.appendChild(el("table", { class: "lb-table" },
          el("thead", {}, el("tr", {}, el("th", { text: "#" }), el("th", { text: "Name" }), el("th", { class: "num", text: M.label + (M.pct ? " %" : " h") }), el("th", { class: "num", text: "Weeks" }))),
          tb));
        if (missing) tableBox.appendChild(el("p", { class: "fz-muted tk-small", text: missing + " more " + (missing === 1 ? "person" : "people") + " logged this period but no " + (M.pct ? M.label + " scores." : M.long.toLowerCase() + ".") }));
      }

      // graph 1: the metric per user
      g1h.textContent = M.long + " — " + (period === "week" ? (week ? "that week" : "this week") : P.label.toLowerCase());
      var top = rows.slice(0, 20);
      var o1 = { rows: top.map(function (r) { return { label: (r.u.user && r.u.user.name) || "Someone", value: r.v, href: F.profileHref ? F.profileHref(r.u.user) : null, me: me && r.u.user && r.u.user.id === me.id, note: (r.u.weeks_logged | 0) + " week" + ((r.u.weeks_logged | 0) === 1 ? "" : "s") + " logged" }; }),
        color: M.color, fmt: fmt, max: M.pct ? 100 : 0, valueLabel: M.label, ariaLabel: M.long + " per person" };
      if (c1) c1.update(o1); else c1 = FazonCharts.hbars(g1, o1);
      graphs.hidden = !rows.length;

      // graph 2: per week, top 8
      var weeks = data.weeks || [];
      g2h.textContent = M.long + " per week — top " + Math.min(8, rows.length);
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
          fmt: function (v, tick) { return tick ? (M.pct ? v + "%" : T.fmtNum(v)) : fmt(v); }, ariaLabel: M.long + " per week, top people" };
        if (c2) c2.update(o2); else c2 = FazonCharts.lines(g2, o2);
      }
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
          list.appendChild(T.summaryCard(it));
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
