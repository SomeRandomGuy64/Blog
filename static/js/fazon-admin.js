// /admin/ (layouts/admin/section.html): the users list for moderators and admins, with role
// changes (admins), mute / unmute (moderators: regular users only), Kyōtsu Tokyo-mode access
// (admins; admins always have it) and account deletion (admins; confirmed in the page by typing
// the user's name). Every control is only shown when the row's "can" flags from the API allow it,
// and the API checks every rule again anyway.
// Needs js/fazon-api.js. API: GET /v1/admin/users?q=&limit=&offset=, POST …/{id}/role,
// POST …/{id}/mute|unmute, POST …/{id}/hyojungo {on}, DELETE /v1/admin/users/{id}.
(function () {
  "use strict";
  var F = window.Fazon, el = F && F.el, root = document.getElementById("adm");
  if (!F || !root) return;
  var ROLES = { user: "User", moderator: "Moderator", admin: "Admin" };
  var PAGE = 50;
  // Wide lays the same rows and controls out as one table with the search in the page bar; the
  // choice is made in row() and init(), and the whole list is rebuilt when wide flips.
  var isWide = function () { return !!(window.FazonWide && FazonWide.active()); };
  var insp3 = window.matchMedia("(min-width: 2300px)");
  var keep = { q: "", selId: 0 };
  var cur = null;
  var fmtDate = function (iso) {
    var d = new Date(iso);
    return isNaN(d) ? "" : d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
  };

  function msg() {
    root.textContent = "";
    root.appendChild(el.apply(null, ["p", { class: "adm-msg" }].concat([].slice.call(arguments))));
  }

  F.me().then(function (m) {
    if (m.offline) msg("Couldn't reach the server. Try again in a moment.");
    else if (!m.user) msg(el("a", { href: F.loginHref(), text: "Log in" }), " to see this page.");
    else if (!F.isStaff(m.user)) msg(el("b", { text: "Not allowed." }), " This page is for moderators and admins.");
    else {
      init(m.user);
      document.addEventListener("fz-layout", function () { init(m.user); });
    }
  });
  if (window.FazonWide) {
    FazonWide.key("/", "Search users", function () { if (cur && cur.search) { cur.search.focus(); cur.search.select(); } });
    FazonWide.key("j", "Next user", function () { if (cur && cur.step) cur.step(1); });
    FazonWide.key("k", "Previous user", function () { if (cur && cur.step) cur.step(-1); });
    FazonWide.key("Enter", "Open their profile", function () { if (cur && cur.open) cur.open(); });
  }

  function init(me) {
    var isAdmin = me.role === "admin";
    var wide = isWide();
    var q = keep.q, offset = 0, total = 0, seq = 0, timer = 0;
    var byId = {};
    var COLS = (isAdmin ? ["User", "Email", "Role", "Muted", "Tokyo"] : ["User", "Role", "Muted"]).concat(["Joined", "Last seen", "Comments", "Track", ""]);
    var search = el("input", { type: "search", class: "fz-input adm-search", "aria-label": "Search users",
      placeholder: isAdmin ? "Search by name or email" : "Search by name" });
    search.value = q;
    var count = el("p", { class: "adm-count fz-muted", role: "status", "aria-live": "polite" });
    var list = wide ? el("table", { class: "w-table users-table" }, el("thead", {}, el.apply(null, ["tr", {}].concat(COLS.map(function (c) {
      return el("th", { class: c === "Comments" ? "num" : null, text: c });
    }))))) : el("ul", { class: "adm-list" });
    var more = el("button", { type: "button", class: "fz-btn adm-more", text: "Load more", hidden: true });
    var listErr = el("p", { class: "fz-error", role: "alert" });

    root.textContent = "";
    if (window.FazonWide) FazonWide.tools().textContent = "";
    var help = "You're signed in as " + me.name + " (" + ROLES[me.role] + "). " +
      (isAdmin ? "Admins can change roles, mute people and delete accounts. Moderators can delete any comment and mute or unmute regular users."
        : "Moderators can delete any comment and mute or unmute regular users.") +
      " Muted people can still read, like comments and use Track, but can't post comments or replies." +
      (isAdmin ? " Tokyo mode is the Kyōtsu trainer's Tokyo pitch mode, which uses Kotu's NHK accent-dictionary recordings. " +
        "They aren't ours to share, so only admins and the accounts you turn it on for get it." : "");
    var insp = wide ? el("aside", { class: "w-pane users-insp", "aria-label": "Selected user" }) : null;
    if (wide) {
      FazonWide.tools().append(
        el("button", { type: "button", class: "w-btn sm users-help", text: "?", "data-tip": help, "aria-label": help }),
        count, search);
      root.append(el("div", { class: "w-panes users-panes" },
        el("div", { class: "w-pane users-main" }, list, listErr, el("div", { class: "adm-foot" }, more)), insp));
    } else root.append(
      el("p", { class: "adm-intro" }, "You're signed in as ", el("b", { class: "adm-you", text: me.name }), " (" + ROLES[me.role] + "). ",
        isAdmin ? "Admins can change roles, mute people and delete accounts. Moderators can delete any comment and mute or unmute regular users."
          : "Moderators can delete any comment and mute or unmute regular users."),
      el("p", { class: "adm-intro fz-muted", text: "Muted people can still read, like comments and use Track, but can't post comments or replies." }),
      isAdmin ? el("p", { class: "adm-intro fz-muted", text: "Tokyo mode is the Kyōtsu trainer's Tokyo pitch mode, which uses Kotu's NHK accent-dictionary recordings. " +
        "They aren't ours to share, so only admins and the accounts you turn it on for get it." }) : null,
      el("div", { class: "adm-bar" }, search, count),
      list, listErr, el("div", { class: "adm-foot" }, more));

    function paintCount() {
      count.textContent = total === 0 ? (q ? "No one matches." : "No users.") :
        total + (total === 1 ? " user" : " users") + (q ? " match" + (total === 1 ? "es" : "") : "");
    }
    function load(reset) {
      var my = ++seq;
      if (reset) offset = 0;
      more.disabled = true; listErr.textContent = "";
      var qs = "?limit=" + PAGE + "&offset=" + offset + (q ? "&q=" + encodeURIComponent(q) : "");
      F.api("/v1/admin/users" + qs).then(function (d) {
        if (my !== seq) return;
        if (reset) {
          if (wide) list.querySelectorAll("tbody").forEach(function (b) { b.remove(); });
          else list.textContent = "";
        }
        (d.users || []).forEach(function (a) { list.appendChild(row(a)); });
        paintSel();
        total = d.total || 0;
        offset += (d.users || []).length;
        paintCount();
        more.hidden = offset >= total; more.disabled = false;
      }, function (e) {
        if (my !== seq) return;
        more.disabled = false;
        if (e.status === 403 || e.status === 401) { msg(el("b", { text: "Not allowed." }), " This page is for moderators and admins."); return; }
        listErr.textContent = e.message;
      });
    }
    search.addEventListener("input", function () {
      clearTimeout(timer);
      timer = setTimeout(function () { var v = search.value.trim(); if (v !== q) { q = keep.q = v; load(true); } }, 300);
    });
    more.addEventListener("click", function () { load(false); });
    load(true);

    // ---- wide: the selected row, and the inspector on a very wide window ----
    function rowsNow() { return [].slice.call(list.querySelectorAll("tr[data-id]")); }
    function paintSel(scroll) {
      if (!wide) return;
      var on = null;
      rowsNow().forEach(function (tr) {
        var yes = +tr.dataset.id === keep.selId;
        tr.setAttribute("aria-selected", String(yes));
        if (yes) on = tr;
      });
      if (on && scroll) on.scrollIntoView({ block: "nearest" });
      paintInsp();
    }
    function select(id, scroll) { keep.selId = id; paintSel(scroll); }
    var inspFor = 0;
    function paintInsp() {
      if (!insp || !insp3.matches) return;
      var a = byId[keep.selId];
      if (!a) { inspFor = 0; insp.textContent = ""; insp.append(el("p", { class: "w-empty", text: "Select someone to see their details." })); return; }
      var u = a.user || {}, id = u.id;
      inspFor = id;
      var line = function (k, v) { return v == null || v === "" ? null : el("tr", {}, el("th", { text: k }), el("td", {}, v)); };
      var recent = el("ul", { class: "users-recent" }, el("li", { class: "fz-muted", text: "Loading…" }));
      insp.textContent = "";
      insp.append(
        el("div", { class: "users-who" }, F.avatar(u, 48), el("div", {},
          el("a", { href: F.profileHref(u), text: u.name || "User" }),
          el("div", { class: "fz-muted", text: ROLES[a.role] || a.role }))),
        el("table", { class: "users-facts" }, el("tbody", {},
          line("Email", a.email), line("Joined", fmtDate(a.joined)), line("Last seen", a.last_active ? F.timeEl(a.last_active) : "never"),
          line("Comments", String(a.comments)), line("Track", a.track_public ? "Public" : "Hidden"),
          line("Muted", a.muted ? el("span", {}, F.timeEl(a.muted_at), a.muted_by ? " by " + a.muted_by.name : "") : "No"),
          isAdmin ? line("Tokyo mode", a.hyojungo ? "On" : "Off") : null)),
        el("div", { class: "w-sec" }, el("h2", { class: "w-sec-h", text: "Recent comments" }), recent));
      F.api("/v1/users/" + id).then(function (p) {
        if (inspFor !== id) return;
        recent.textContent = "";
        ((p && p.recent) || []).forEach(function (c) {
          recent.appendChild(el("li", {}, el("a", { href: (F.safePath(c.page || "") || "/") + "#comment-" + (c.id | 0) },
            el("span", { text: c.page_title || c.page || "a page" }), c.snippet ? el("span", { class: "fz-muted", text: "“" + c.snippet + "”" }) : null, F.timeEl(c.created))));
        });
        if (!recent.children.length) recent.append(el("li", { class: "fz-muted", text: "No comments yet." }));
      }, function () { if (inspFor === id) { recent.textContent = ""; recent.append(el("li", { class: "fz-muted", text: "Couldn't load their comments." })); } });
    }
    cur = { search: search };
    if (wide) {
      list.addEventListener("click", function (e) {
        if (e.target.closest("a, button, select, input")) return;
        var tr = e.target.closest("tr[data-id]");
        if (tr) select(+tr.dataset.id);
      });
      cur.step = function (by) {
        var rs = rowsNow(), i = rs.findIndex(function (tr) { return +tr.dataset.id === keep.selId; });
        var next = rs[i < 0 ? 0 : Math.max(0, Math.min(rs.length - 1, i + by))];
        if (next) select(+next.dataset.id, true);
      };
      cur.open = function () {
        var t = document.activeElement;     // Enter on a focused control does what it always does
        if (t && t.closest && t.closest("a, button, summary")) { t.click(); return; }
        var a = byId[keep.selId]; if (a) location.href = F.profileHref(a.user);
      };
      insp3.addEventListener("change", function () { paintSel(); });
    }

    // ---- one user ----
    function row(a) {
      var u = a.user || {}, li = el(wide ? "tbody" : "li", { class: "adm-row" + (a.muted ? " muted" : ""), id: "adm-user-" + (u.id | 0) });
      byId[u.id] = a;
      var self = u.id === me.id;
      var err = el("p", { class: "fz-error adm-err", role: "alert" });
      var slot = el("div", { class: "adm-slot" });
      function replace(next) { var n = row(next); li.replaceWith(n); paintSel(); return n; }
      function fail(e) { err.textContent = e.message; }

      var name = el("div", { class: "adm-name" },
        el("a", { href: F.profileHref(u), "data-user-card": String(u.id | 0), text: u.name || "User" }),
        a.role !== "user" ? el("span", { class: "adm-badge adm-role-" + a.role, text: ROLES[a.role] || a.role }) : null,
        a.muted ? el("span", { class: "adm-badge adm-muted", text: "Muted" }) : null,
        isAdmin && a.hyojungo && !a.can.hyojungo ? el("span", { class: "adm-badge", text: "Tokyo mode", title: "Can use the Kyōtsu trainer's Tokyo mode (admins always can)" }) : null,
        self ? el("span", { class: "fz-muted adm-self", text: "(you)" }) : null);
      var meta = el("div", { class: "adm-meta" },
        el("span", { text: "Joined " + fmtDate(a.joined) }),
        a.last_active ? el("span", {}, "Last seen ", F.timeEl(a.last_active)) : null,
        el("span", { text: a.comments + (a.comments === 1 ? " comment" : " comments") }),
        el("span", { text: a.track_public ? "Track public" : "Track hidden" }));
      var info = el("div", { class: "adm-info" }, name,
        a.email != null ? el("div", { class: "adm-email", text: a.email }) : null,
        meta,
        a.muted ? el("div", { class: "adm-mutedby fz-muted" }, "Muted ", F.timeEl(a.muted_at),
          a.muted_by ? " by " : "", a.muted_by ? el("b", { text: a.muted_by.name }) : null) : null);

      var ctl = el("div", { class: "adm-ctl" });
      if (isAdmin && !self) {
        var sel = el("select", { class: "fz-input adm-role", "aria-label": "Role for " + (u.name || "user"), disabled: !a.can.role });
        Object.keys(ROLES).forEach(function (r) { sel.appendChild(el("option", { value: r, text: ROLES[r], selected: r === a.role })); });
        if (!a.can.role) sel.title = "That's the only admin";
        sel.addEventListener("change", function () {
          var to = sel.value;
          sel.disabled = true; err.textContent = "";
          F.api("/v1/admin/users/" + u.id + "/role", { method: "POST", body: { role: to } }).then(function (d) {
            replace(d.user).querySelector(".adm-role").focus();
          }, function (e) { sel.value = a.role; sel.disabled = false; fail(e); });
        });
        ctl.appendChild(sel);
      }
      if (a.can.mute) {
        var mute = el("button", { type: "button", class: "fz-btn adm-mute" + (wide ? " sm" : ""), text: a.muted ? "Unmute" : "Mute",
          "aria-label": (a.muted ? "Unmute " : "Mute ") + (u.name || "user") });
        mute.addEventListener("click", function () {
          mute.disabled = true; err.textContent = "";
          F.api("/v1/admin/users/" + u.id + "/" + (a.muted ? "unmute" : "mute"), { method: "POST" }).then(function (d) {
            var n = replace(d.user), b = n.querySelector(".adm-mute"); if (b) b.focus();
          }, function (e) { mute.disabled = false; fail(e); });
        });
        ctl.appendChild(mute);
      }
      if (a.can.hyojungo) {
        var tok = el("button", { type: "button", class: "fz-btn adm-tokyo" + (wide ? " sm" : ""), text: (wide ? "" : "Tokyo mode: ") + (a.hyojungo ? "On" : "Off"), "aria-pressed": a.hyojungo ? "true" : "false",
          "aria-label": wide ? "Tokyo mode for " + (u.name || "user") + ": " + (a.hyojungo ? "On" : "Off") : null,
          title: (a.hyojungo ? "Turn off" : "Turn on") + " the Kyōtsu trainer's Tokyo mode for " + (u.name || "this user") });
        tok.addEventListener("click", function () {
          tok.disabled = true; err.textContent = "";
          F.api("/v1/admin/users/" + u.id + "/hyojungo", { method: "POST", body: { on: !a.hyojungo } }).then(function (d) {
            var b = replace(d.user).querySelector(".adm-tokyo"); if (b) b.focus();
          }, function (e) { tok.disabled = false; fail(e); });
        });
        ctl.appendChild(tok);
      }
      if (a.can.delete) {
        var del = el("button", { type: "button", class: "fz-btn adm-danger" + (wide ? " sm" : ""), text: wide ? "Delete…" : "Delete account…",
          "aria-label": "Delete the account of " + (u.name || "user") });
        del.addEventListener("click", function () { confirmDelete(); });
        ctl.appendChild(del);
      }

      // In-page confirmation: type the user's display name exactly.
      function confirmDelete() {
        err.textContent = "";
        var want = String(u.name || "").trim();
        var id = "adm-del-" + (u.id | 0);
        var inp = el("input", { class: "fz-input", type: "text", id: id, autocomplete: "off", spellcheck: "false" });
        var go = el("button", { type: "button", class: "fz-btn adm-danger solid", text: "Delete account", disabled: true });
        var cancel = el("button", { type: "button", class: "fz-btn", text: "Cancel" });
        var ok = function () { return inp.value.trim() === want && want !== ""; };
        var close = function () { slot.textContent = ""; ctl.hidden = false; li.classList.remove("confirming"); if (del) del.focus(); };
        inp.addEventListener("input", function () { go.disabled = !ok(); });
        inp.addEventListener("keydown", function (e) {
          if (e.key === "Enter" && ok()) { e.preventDefault(); go.click(); }
          else if (e.key === "Escape") { e.preventDefault(); close(); }
        });
        cancel.addEventListener("click", close);
        go.addEventListener("click", function () {
          if (!ok()) return;
          go.disabled = cancel.disabled = inp.disabled = true; go.textContent = "Deleting…";
          F.api("/v1/admin/users/" + u.id, { method: "DELETE" }).then(function () {
            var note = el("span", {}, el("b", { text: u.name || "User" }), "'s account was deleted.");
            var gone = wide ? el("tbody", { class: "adm-gone", tabindex: "-1" }, el("tr", {}, el("td", { colspan: COLS.length }, note)))
              : el("li", { class: "adm-row adm-gone", tabindex: "-1" }, note);
            delete byId[u.id]; if (keep.selId === u.id) keep.selId = 0;
            li.replaceWith(gone); gone.focus(); paintSel();
            total = Math.max(0, total - 1); offset = Math.max(0, offset - 1); paintCount();
          }, function (e) {
            go.textContent = "Delete account"; cancel.disabled = inp.disabled = false; go.disabled = !ok(); fail(e);
          });
        });
        ctl.hidden = true; li.classList.add("confirming");
        slot.textContent = "";
        slot.append(el("div", { class: "adm-confirm" },
          el("p", {}, el("b", {}, "Delete ", el("span", { class: "adm-q", text: want }), "'s account and all their data?"),
            " Their profile, picture, Track log, Kyōtsu history, likes, notifications and API keys go, and so do their comments; " +
            "comments others replied to stay as “[deleted]”. This can't be undone."),
          el("label", { for: id }, "Type their name (", el("b", { class: "adm-q", text: want }), ") to confirm:"),
          inp, el("div", { class: "fz-row" }, go, cancel)));
        inp.focus();
      }

      if (wide) return wideRow();
      li.append(el("div", { class: "adm-top" }, F.avatar(u, 44), info, ctl.children.length ? ctl : null), slot, err);
      return li;

      // the same controls, one cell each; the delete confirmation opens on a row of its own
      function wideRow() {
        var who = el("td", { class: "users-user" }, F.avatar(u, 20, { ring: false, badge: false, link: false }),
          el("a", { href: F.profileHref(u), "data-user-card": String(u.id | 0), text: u.name || "User" }),
          self ? el("span", { class: "fz-muted", text: "(you)" }) : null);
        var roleCell = el("td", {}, sel || el("span", { text: ROLES[a.role] || a.role }));
        var muteCell = el("td", { class: "users-mc" });
        if (a.muted) muteCell.append(el("span", { class: "adm-mutedby" }, "Muted ", F.timeEl(a.muted_at), a.muted_by ? " by " + a.muted_by.name : ""));
        if (mute) muteCell.append(a.muted ? mute : el("span", { class: "w-acts" }, mute));
        var cells = [who];
        if (isAdmin) cells.push(el("td", { class: "users-mail", text: a.email == null ? "" : a.email, title: a.email || null }));
        cells.push(roleCell, muteCell);
        if (isAdmin) cells.push(el("td", {}, tok || el("span", { class: "fz-muted", text: a.can.hyojungo === false && a.role === "admin" ? "Always" : (a.hyojungo ? "On" : "Off") })));
        cells.push(el("td", { class: "users-d", text: fmtDate(a.joined) }),
          el("td", { class: "users-d" }, a.last_active ? F.timeEl(a.last_active) : null),
          el("td", { class: "num", text: String(a.comments) }),
          el("td", { class: "users-d", text: a.track_public ? "Public" : "Hidden" }),
          el("td", { class: "users-a" }, del ? el("span", { class: "w-acts" }, del) : null));
        li.append(el.apply(null, ["tr", { "data-id": String(u.id | 0), "aria-selected": String(u.id === keep.selId) }].concat(cells)),
          el("tr", { class: "users-detail" }, el("td", { colspan: COLS.length }, slot, err)));
        return li;
      }
    }
  }
})();
