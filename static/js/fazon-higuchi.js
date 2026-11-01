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

  function init(data) {
    var videos = data.videos, watched = new Set(data.watched);
    var num = function () { return el("b"); }, label = function () { return el("span"); };
    var sWatched = num(), sWatchedN = label(), sLeft = num(), sLeftN = label(), sPct = num(), sTotal = label();
    var fill = el("i", { class: "hg-fill" });
    var filter = el("select", { "aria-label": "Show" },
      el("option", { value: "", text: "Everything" }),
      el("option", { value: "z", text: "Zatsudan" }),
      el("option", { value: "m", text: "Members" }));
    var hide = el("input", { type: "checkbox" });
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
    }

    function visible() {
      videos.forEach(function (v) {
        v.row.hidden = !!(filter.value && v.src !== filter.value) || (hide.checked && watched.has(v.id));
      });
    }

    function mark(v, on) {
      if (on) watched.add(v.id); else watched.delete(v.id);
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
      v.row = el("tr", { class: watched.has(v.id) ? "done" : "" },
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

    filter.addEventListener("change", function () { stats(); visible(); });
    hide.addEventListener("change", visible);
    next.addEventListener("click", function () {
      var v = videos.find(function (v) { return !watched.has(v.id) && !v.row.hidden; });
      if (!v) return;
      v.row.scrollIntoView({ block: "center", behavior: "smooth" });
      v.box.focus({ preventScroll: true });
    });

    root.textContent = "";
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
      el("table", {},
        el("thead", {}, el("tr", {},
          el("th", { class: "hg-n", text: "#" }),
          el("th", { class: "hg-w", text: "Watched" }),
          el("th", { text: "Date" }),
          el("th", { text: "Type" }),
          el("th", { text: "Title" }),
          el("th", { class: "hg-len", text: "Length" }))),
        body));
    stats();
  }
})();
