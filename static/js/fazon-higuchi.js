// /higuchi/ (layouts/higuchi/section.html): the private zatsudan + members watch list. Everything
// on the page comes from the API, which only answers for the owner account; ticks are saved to
// the account, so they follow you between browsers. The counters follow the Show filter.
// Needs js/fazon-api.js. API: GET /v1/higuchi/videos, POST /v1/higuchi/videos/{id}/watched {watched}.
(function () {
  "use strict";
  var F = window.Fazon, el = F && F.el, root = document.getElementById("hg");
  if (!F || !root) return;

  function msg() {
    root.textContent = "";
    root.appendChild(el.apply(null, ["p", { class: "hg-msg" }].concat([].slice.call(arguments))));
  }

  function hm(sec) {
    var min = Math.round(sec / 60);
    return Math.floor(min / 60) + "h " + String(min % 60).padStart(2, "0") + "m";
  }

  function len(sec) {
    return Math.round(sec / 60) < 60 ? Math.round(sec / 60) + "m" : hm(sec);
  }

  F.me().then(function (m) {
    if (m.offline) return msg("Couldn't reach the server. Try again in a moment.");
    if (!m.user) return msg(el("a", { href: F.loginHref(), text: "Log in" }), " to see this page.");
    F.api("/v1/higuchi/videos").then(init, function (e) {
      if (e.status === 403) msg(el("b", { text: "Not allowed." }));
      else msg(e.message);
    });
  });

  // What survives wide flipping: init() rebuilds the page from this and the server's ticks.
  var ui = { filter: "", hide: false, sort: "", dir: 1, sel: "" };
  var isWide = function () { return !!(window.FazonWide && FazonWide.active()); };
  var insp3 = window.matchMedia("(min-width: 2300px)");
  var cur = null, booted = false;

  function init(data) {
    var wide = isWide();
    if (!booted) {
      booted = true;
      document.addEventListener("fz-layout", function () { init(data); });
      if (window.FazonWide) {
        FazonWide.key("j", "Next video", function () { if (cur) cur.step(1); });
        FazonWide.key("k", "Previous video", function () { if (cur) cur.step(-1); });
        FazonWide.key("x", "Tick the selected video", function () { if (cur) cur.tick(); });
        FazonWide.key("Enter", "Open the selected video", function () { if (cur) cur.open(); });
        FazonWide.key("n", "Next unwatched", function () { if (cur) cur.next(); });
      }
    }
    var videos = data.videos, watched = new Set(data.watched);
    var num = function () { return el("b"); }, label = function () { return el("span"); };
    var sWatched = num(), sWatchedN = label(), sLeft = num(), sLeftN = label(), sPct = num(), sTotal = label();
    var fill = el("i", { class: "hg-fill" });
    var filter = el("select", { "aria-label": "Show" },
      el("option", { value: "", text: "Everything" }),
      el("option", { value: "z", text: "Zatsudan" }),
      el("option", { value: "m", text: "Members" }));
    filter.value = ui.filter;
    var hide = el("input", { type: "checkbox" });
    hide.checked = ui.hide;
    var next = el("button", { type: "button", class: "fz-btn", text: "Next unwatched" });
    var err = el("p", { class: "hg-err", role: "alert" });
    var body = el("tbody");

    function stats() {
      var scope = videos.filter(function (v) { return !filter.value || v.src === filter.value; });
      var seen = scope.filter(function (v) { return watched.has(v.id); });
      var sum = function (l) { return l.reduce(function (a, v) { return a + v.dur; }, 0); };
      var total = sum(scope), done = sum(seen), pct = total ? done / total * 100 : 0;
      sWatched.textContent = hm(done);
      sWatchedN.textContent = "watched, " + seen.length + " of " + scope.length + " videos";
      sLeft.textContent = hm(total - done);
      sLeftN.textContent = "left, " + (scope.length - seen.length) + " videos";
      sPct.textContent = pct.toFixed(1) + "%";
      sTotal.textContent = "of " + hm(total) + " total";
      fill.style.width = pct + "%";
      if (wide) {
        [[sWatched, sWatchedN], [sLeft, sLeftN], [sPct, sTotal]].forEach(function (p) {
          p[0].parentNode.setAttribute("data-tip", p[0].textContent + " " + p[1].textContent);
        });
        paintInsp();
      }
    }

    function visible() {
      videos.forEach(function (v) {
        v.row.hidden = !!(filter.value && v.src !== filter.value) || (hide.checked && watched.has(v.id));
      });
    }

    function mark(v, on) {
      if (on) watched.add(v.id); else watched.delete(v.id);
      data.watched = Array.from(watched);
      v.box.checked = on;
      v.row.classList.toggle("done", on);
      stats();
    }

    videos.forEach(function (v, i) {
      v.box = el("input", { type: "checkbox", "aria-label": "Watched: " + v.title });
      v.box.checked = watched.has(v.id);
      v.box.addEventListener("change", function () {
        var on = v.box.checked;
        err.textContent = "";
        mark(v, on);
        F.api("/v1/higuchi/videos/" + encodeURIComponent(v.id) + "/watched", { method: "POST", body: { watched: on } })
          .catch(function (e) {
            mark(v, !on);
            err.textContent = "Not saved: " + e.message;
          });
      });
      v.n = i + 1;
      v.row = el("tr", { class: watched.has(v.id) ? "done" : "", "data-id": v.id },
        el("td", { class: "hg-n", text: String(i + 1) }),
        el("td", { class: "hg-w" }, v.box),
        el("td", { class: "hg-date", text: v.date }),
        el("td", {}, el("div", { class: "hg-src " + v.src, text: v.src === "z" ? "Zatsudan" : "Members" }, el("small", { text: v.tag }))),
        el("td", { class: "hg-t" },
          el("a", { href: "https://www.youtube.com/watch?v=" + v.id, target: "_blank", rel: "noopener", text: v.title }),
          el("div", { class: "hg-ro", text: v.ro })),
        el("td", { class: "hg-len", text: len(v.dur) }));
      body.appendChild(v.row);
    });

    filter.addEventListener("change", function () { ui.filter = filter.value; stats(); visible(); });
    hide.addEventListener("change", function () { ui.hide = hide.checked; visible(); });
    next.addEventListener("click", goNext);
    function goNext() {
      var v = shown().find(function (v) { return !watched.has(v.id); });
      if (!v) return;
      if (wide) select(v);
      v.row.scrollIntoView({ block: "center", behavior: "smooth" });
      v.box.focus({ preventScroll: true });
    }

    // ---- wide: filter as a segmented control, sortable heads, row selection, an inspector ----
    function shown() {
      var rows = Array.prototype.slice.call(body.children), by = {};
      videos.forEach(function (v) { by[v.id] = v; });
      return rows.map(function (r) { return by[r.dataset.id]; }).filter(function (v) { return !v.row.hidden; });
    }
    function selected() { return videos.find(function (v) { return v.id === ui.sel; }); }
    function select(v, scroll) {
      ui.sel = v ? v.id : "";
      videos.forEach(function (x) { x.row.setAttribute("aria-selected", String(x === v)); });
      if (v && scroll) v.row.scrollIntoView({ block: "nearest" });
      paintInsp();
    }
    var insp = wide ? el("aside", { class: "w-pane hg-insp", "aria-label": "Selected video" }) : null;
    function paintInsp() {
      if (!insp || !insp3.matches) return;
      var v = selected(), sum = function (l) { return l.reduce(function (a, x) { return a + x.dur; }, 0); };
      insp.textContent = "";
      if (v) {
        var fact = function (k, val) { return el("tr", {}, el("th", { text: k }), el("td", { text: val })); };
        insp.append(el("div", { class: "w-sec" },
          el("h2", { class: "w-sec-h", text: "Selected" }),
          el("p", { class: "hg-i-t" }, el("a", { href: "https://www.youtube.com/watch?v=" + v.id, target: "_blank", rel: "noopener", text: v.title })),
          v.ro ? el("p", { class: "hg-ro", text: v.ro }) : null,
          el("table", { class: "hg-facts" }, el("tbody", {},
            fact("Number", String(v.n)), fact("Date", v.date), fact("Type", (v.src === "z" ? "Zatsudan" : "Members") + (v.tag ? ", " + v.tag : "")),
            fact("Length", len(v.dur)), fact("Watched", watched.has(v.id) ? "Yes" : "No")))));
      } else insp.append(el("p", { class: "w-empty", text: "Select a video to see its details." }));
      var rows = [["z", "Zatsudan"], ["m", "Members"]].map(function (s) {
        var all = videos.filter(function (x) { return x.src === s[0]; });
        var total = sum(all), done = sum(all.filter(function (x) { return watched.has(x.id); }));
        return { key: s[0], name: s[1], total: total, done: done };
      });
      var max = Math.max.apply(null, rows.map(function (r) { return r.total; }).concat(1));
      insp.append(el("div", { class: "w-sec" }, el("h2", { class: "w-sec-h", text: "Hours by source" }),
        el.apply(null, ["div", { class: "hg-src-chart" }].concat(rows.map(function (r) {
          return el("div", { class: "hg-sc", "data-tip": r.name + ": " + hm(r.done) + " watched of " + hm(r.total) },
            el("div", { class: "hg-sc-l" }, el("span", { text: r.name }), el("span", { text: hm(r.done) + " of " + hm(r.total) })),
            el("div", { class: "hg-sc-track", style: "width:" + (100 * r.total / max) + "%" },
              el("i", { class: r.key, style: "width:" + (r.total ? 100 * r.done / r.total : 0) + "%" })));
        })))));
    }
    var SORTS = { "#": ["n", 1], Date: ["date", 1], Title: ["title", 1], Length: ["dur", -1] };
    function sortRows() {
      var k = ui.sort, order = videos.slice();
      if (k) order.sort(function (a, b) {
        var c = a[k] < b[k] ? -1 : a[k] > b[k] ? 1 : 0;
        return c * ui.dir || a.n - b.n;
      });
      order.forEach(function (v) { body.appendChild(v.row); });
    }
    function wideHeads(head) {
      Array.prototype.forEach.call(head.querySelectorAll("th"), function (th) {
        var s = SORTS[th.textContent];
        if (!s) return;
        var paint = function () {
          th.setAttribute("aria-sort", ui.sort === s[0] ? (ui.dir > 0 ? "ascending" : "descending") : "none");
        };
        var go = function () {
          if (ui.sort === s[0]) ui.dir = -ui.dir; else { ui.sort = s[0]; ui.dir = s[1]; }
          Array.prototype.forEach.call(head.querySelectorAll("th"), function (t) { if (t._paint) t._paint(); });
          sortRows();
        };
        th._paint = paint; paint();
        th.tabIndex = 0;
        th.addEventListener("click", go);
        th.addEventListener("keydown", function (e) { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); go(); } });
      });
    }

    var table = el("table", { class: wide ? "w-table" : null },
      el("thead", {}, el("tr", {},
        el("th", { class: "hg-n", text: "#" }),
        el("th", { class: "hg-w", text: "Watched" }),
        el("th", { text: "Date" }),
        el("th", { text: "Type" }),
        el("th", { text: "Title" }),
        el("th", { class: "hg-len", text: "Length" }))),
      body);

    root.textContent = "";
    if (window.FazonWide) FazonWide.tools().textContent = "";
    if (wide) {
      var seg = el("div", { class: "w-seg", role: "group", "aria-label": "Show" });
      [["", "All"], ["z", "Zatsudan"], ["m", "Members"]].forEach(function (o) {
        var b = el("button", { type: "button", text: o[1], "aria-pressed": String(ui.filter === o[0]) });
        b.addEventListener("click", function () {
          filter.value = o[0];
          filter.dispatchEvent(new Event("change"));
          Array.prototype.forEach.call(seg.children, function (x) { x.setAttribute("aria-pressed", String(x === b)); });
        });
        seg.append(b);
      });
      next.className = "w-btn";
      FazonWide.tools().append(
        el("div", { class: "hg-bar-stats" },
          el("span", { class: "hg-stat" }, sWatched, " ", sWatchedN),
          el("span", { class: "hg-stat" }, sLeft, " ", sLeftN),
          el("span", { class: "hg-stat" }, sPct, " ", sTotal)),
        seg, el("label", { class: "hg-hide" }, hide, " Hide watched"), next);
      root.append(el("div", { class: "w-panes hg-panes" },
        el("div", { class: "w-pane hg-main" }, el("div", { class: "hg-bar" }, fill), err, table), insp));
      wideHeads(table);
      videos.forEach(function (v) { v.row.querySelector(".hg-t").title = v.ro ? v.title + " / " + v.ro : v.title; });
      sortRows();
      body.addEventListener("click", function (e) {
        if (e.target.closest("a, input")) return;
        var tr = e.target.closest("tr");
        if (tr) select(videos.find(function (v) { return v.id === tr.dataset.id; }));
      });
      insp3.addEventListener("change", paintInsp);
      select(selected());
      cur = {
        step: function (by) {
          var list = shown(), i = list.indexOf(selected());
          var v = list[i < 0 ? 0 : Math.max(0, Math.min(list.length - 1, i + by))];
          if (v) select(v, true);
        },
        tick: function () { var v = selected(); if (v) v.box.click(); },
        open: function () {
          var t = document.activeElement;     // Enter on a focused control does what it always does
          if (t && t.closest && t.closest("a, button, summary, th")) { t.click(); return; }
          var v = selected(); if (v) window.open("https://www.youtube.com/watch?v=" + v.id, "_blank", "noopener");
        },
        next: goNext
      };
      visible();
      stats();
      return;
    }
    cur = null;
    root.append(
      el("div", { class: "hg-head" },
        el("div", { class: "hg-stats" },
          el("div", { class: "hg-stat" }, sWatched, sWatchedN),
          el("div", { class: "hg-stat" }, sLeft, sLeftN),
          el("div", { class: "hg-stat" }, sPct, sTotal)),
        el("div", { class: "hg-bar" }, fill),
        el("div", { class: "hg-tools" },
          el("label", {}, "Show ", filter),
          el("label", {}, hide, " Hide watched"),
          next),
        err),
      table);
    visible();
    stats();
  }
})();
