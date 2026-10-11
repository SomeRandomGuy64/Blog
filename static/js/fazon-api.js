// Shared client for the fazon.nexus API (accounts, comments, notifications, Kyōtsu sessions).
// Loaded in <head> of the main baseof (not the cinema one), so inline page scripts can use
// window.Fazon straight away. Also fills in the header's account links once the DOM is ready.
(function () {
  "use strict";

  // ---- API base ----
  // Production: <meta name="fazon-api"> (site.Params.api). On localhost / 127.0.0.1 the dev API
  // on the same host, port 8091, is used, unless ?api=http://localhost:PORT or http://127.0.0.1:PORT
  // is given (remembered for the tab, so it survives navigation). No other hosts are accepted.
  function resolveBase() {
    var meta = document.querySelector('meta[name="fazon-api"]');
    var base = (meta && meta.content) || "https://api.fazon.nexus";
    var h = location.hostname;
    if (h === "localhost" || h === "127.0.0.1") {
      base = "http://" + h + ":8091"; // same host as the page, so the session cookie is same-site
      var ok = function (v) { return typeof v === "string" && /^http:\/\/(localhost|127\.0\.0\.1):\d{1,5}$/.test(v); };
      var q = null;
      try { q = new URLSearchParams(location.search).get("api"); } catch (e) {}
      if (q) q = q.replace(/\/+$/, "");
      if (ok(q)) { base = q; try { sessionStorage.setItem("fazon-api", q); } catch (e) {} }
      else { try { var s = sessionStorage.getItem("fazon-api"); if (ok(s)) base = s; } catch (e) {} }
    }
    return base.replace(/\/+$/, "");
  }
  var BASE = resolveBase();

  // fetch wrapper: cookies included, JSON in/out. Rejects with an Error carrying .status
  // (0 = network failure / API unreachable) and the server's {"error": "..."} message.
  function api(path, opts) {
    opts = opts || {};
    var init = { method: opts.method || "GET", credentials: "include", headers: {} };
    if (opts.raw !== undefined) {           // raw upload (e.g. an avatar JPEG blob)
      init.headers["Content-Type"] = opts.type || "application/octet-stream";
      init.body = opts.raw;
    } else if (opts.body !== undefined) {
      init.headers["Content-Type"] = "application/json";
      init.body = JSON.stringify(opts.body);
    }
    if (opts.keepalive) init.keepalive = true;
    if (opts.cache) init.cache = opts.cache;   // e.g. "reload": skip the HTTP cache but refresh it
    return fetch(BASE + path, init).then(function (res) {
      return res.text().then(function (txt) {
        var data = null;
        try { data = txt ? JSON.parse(txt) : null; } catch (e) {}
        if (!res.ok) {
          var err = new Error((data && data.error) || ("Request failed (" + res.status + ")"));
          err.status = res.status; err.data = data;
          throw err;
        }
        return data;
      });
    }, function (e) {
      var err = new Error("Couldn't reach the server. Try again in a moment.");
      err.status = 0; err.cause = e;
      throw err;
    });
  }

  // /v1/me, cached. Resolves {user, unread, offline}; never rejects (offline:true when the API
  // can't be reached, so callers can quietly hide account UI).
  var meP = null;
  function me(force) {
    if (!meP || force) {
      meP = api("/v1/me").then(function (d) {
        return { user: (d && d.user) || null, unread: (d && d.unread) || 0, offline: false };
      }, function () { return { user: null, unread: 0, offline: true }; });
    }
    return meP;
  }

  // ---- small helpers shared by the pages ----
  var ESC = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };
  function esc(s) { return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) { return ESC[c]; }); }

  // el("a", {href: "/x", class: "y", onclick: fn}, "text", childNode, ...) — text is always textContent.
  // Guard rails for values that come from data: on* only ever takes a function (never an inline
  // handler string), and URL attributes refuse javascript:/vbscript:/data: (browsers ignore
  // tabs/newlines and leading spaces in a scheme, so those are stripped before the check).
  var URL_ATTR = { href: 1, src: 1, action: 1, formaction: 1, "xlink:href": 1, poster: 1 };
  function el(tag, attrs) {
    var n = document.createElement(tag);
    if (attrs) Object.keys(attrs).forEach(function (k) {
      var v = attrs[k];
      if (v == null || v === false) return;
      var lk = k.toLowerCase();
      if (k === "class") n.className = v;
      else if (k === "text") n.textContent = v;
      else if (lk.slice(0, 2) === "on") { if (typeof v === "function") n.addEventListener(lk.slice(2), v); }
      else if (URL_ATTR[lk] && /^(javascript|vbscript|data):/i.test(String(v).replace(/[\u0000-\u0020\u007f]/g, ""))) return;
      else if (k in n && typeof v !== "string") n[k] = v;
      else n.setAttribute(k, v === true ? "" : v);
    });
    for (var i = 2; i < arguments.length; i++) {
      var c = arguments[i];
      if (c == null || c === false) continue;
      n.appendChild(typeof c === "object" ? c : document.createTextNode(String(c)));
    }
    return n;
  }

  // a same-site relative path ("/x/y?z#w"), or null. Rejects "//host", "/\host", control chars.
  function safePath(p) {
    if (typeof p !== "string" || !/^\/(?![\/\\])[^\\\s\u0000-\u001f]*$/.test(p)) return null;
    try { if (new URL(p, location.origin).origin !== location.origin) return null; } catch (e) { return null; }
    return p;
  }

  function absTime(iso) {
    var d = new Date(iso);
    return isNaN(d) ? "" : d.toLocaleString(undefined, { year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
  }
  function relTime(iso) {
    var d = new Date(iso); if (isNaN(d)) return "";
    var s = Math.round((Date.now() - d.getTime()) / 1000);
    if (s < 45) return "just now";
    var m = Math.round(s / 60); if (m < 60) return m + "m ago";
    var h = Math.round(m / 60); if (h < 24) return h + "h ago";
    var dd = Math.round(h / 24); if (dd < 30) return dd + "d ago";
    return d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
  }
  function timeEl(iso, cls) {
    return el("time", { class: cls || null, datetime: iso, title: absTime(iso), text: relTime(iso) });
  }

  function loginHref(next) {
    var n = safePath(next || (location.pathname + location.search)) || "/";
    return "/account/login/?next=" + encodeURIComponent(n);
  }
  function signupHref(next) {
    var n = safePath(next || (location.pathname + location.search)) || "/";
    if (n.indexOf("/account/") === 0) n = "/";   // don't come back to login/sign-up pages
    return "/account/signup/?next=" + encodeURIComponent(n);
  }
  // moderators and admins (user.role; older APIs only sent user.admin)
  function isStaff(u) { return !!u && (u.role === "admin" || u.role === "moderator" || (!u.role && !!u.admin)); }

  // ---- avatar: picture (or initial) inside an XP progress ring, with a level badge ----
  // Fazon.avatar(user, size, {ring, badge}) → <span class="fz-av">. user = public User object
  // (avatar path relative to the API base, level, xp, level_xp, next_xp). Null user = grey blank.
  var SVGNS = "http://www.w3.org/2000/svg";
  function avatarUrl(u) { return u && typeof u.avatar === "string" && u.avatar.charAt(0) === "/" ? BASE + u.avatar : null; }
  function initialOf(u) { return (Array.from(String((u && u.name) || "?").trim())[0] || "?").toUpperCase(); }
  function levelProgress(u) {
    if (!u || u.next_xp == null) return 1;
    var span = u.next_xp - (u.level_xp || 0);
    return span > 0 ? Math.max(0, Math.min(1, ((u.xp || 0) - (u.level_xp || 0)) / span)) : 0;
  }
  function avatar(u, size, opts) {
    opts = opts || {};
    size = size || 36;
    var ring = opts.ring !== false && !!u, badge = opts.badge !== false && !!u && u.level != null;
    var sw = ring ? Math.min(7, Math.max(2, Math.round(size / 12))) : 0;      // ring stroke width
    var gap = ring ? Math.max(1, Math.round(size / 36)) : 0;     // space between ring and picture
    var box = el("span", { class: "fz-av" + (u ? "" : " fz-av-none"), style: "width:" + size + "px;height:" + size + "px" });
    var inner = el("span", { class: "fz-av-pic", style: "inset:" + (sw + gap) + "px;font-size:" + Math.round((size - 2 * (sw + gap)) * 0.48) + "px" });
    var url = avatarUrl(u);
    if (url) {
      var img = el("img", { src: url, alt: "", loading: "lazy", decoding: "async", draggable: "false" });
      img.addEventListener("error", function () { img.remove(); inner.textContent = initialOf(u); inner.classList.add("fz-av-initial"); });
      inner.appendChild(img);
    } else if (u) { inner.textContent = initialOf(u); inner.classList.add("fz-av-initial"); }
    box.appendChild(inner);
    if (ring) {
      var r = (size - sw) / 2, c = 2 * Math.PI * r, p = levelProgress(u);
      var svg = document.createElementNS(SVGNS, "svg");
      svg.setAttribute("class", "fz-av-ring"); svg.setAttribute("width", size); svg.setAttribute("height", size);
      svg.setAttribute("viewBox", "0 0 " + size + " " + size); svg.setAttribute("aria-hidden", "true");
      [["fz-av-track", null], ["fz-av-arc", p]].forEach(function (d) {
        if (d[1] === 0) return;
        var ci = document.createElementNS(SVGNS, "circle");
        ci.setAttribute("class", d[0]); ci.setAttribute("cx", size / 2); ci.setAttribute("cy", size / 2);
        ci.setAttribute("r", r); ci.setAttribute("stroke-width", sw);
        if (d[1] != null) {
          ci.setAttribute("stroke-dasharray", (c * d[1]).toFixed(2) + " " + c.toFixed(2));
          ci.setAttribute("transform", "rotate(-90 " + size / 2 + " " + size / 2 + ")");
        }
        svg.appendChild(ci);
      });
      box.appendChild(svg);
    }
    if (badge) {
      var bs = Math.min(30, Math.max(14, Math.round(size * 0.36)));
      box.appendChild(el("span", { class: "fz-av-badge", text: String(u.level),
        style: "min-width:" + bs + "px;height:" + bs + "px;font-size:" + Math.max(9, Math.round(bs * 0.6)) + "px;line-height:" + (bs - 2) + "px" }));
    }
    // hover card + profile link by default (opts.card:false / opts.link:false to turn off — pass
    // link:false when the avatar already sits inside a link or button)
    var uid = u && u.id != null ? (u.id | 0) : 0;
    var withCard = !!uid && opts.card !== false;
    if (!withCard && u) box.title = (u.name || "") + (u.level != null ? " · Level " + u.level + (u.xp != null ? " · " + u.xp + " points" : "") : "");
    if (withCard) box.setAttribute("data-user-card", String(uid));
    if (uid && opts.link !== false) {
      var a = el("a", { class: "fz-av-link", href: profileHref(u), "aria-label": (u.name || "User") + " — profile" }, box);
      if (withCard) a.setAttribute("data-user-card", String(uid));
      return a;
    }
    return box;
  }
  function profileHref(u) { return "/users/?id=" + ((u && u.id) | 0); }

  // ---- profile hover card: any element with data-user-card="<id>" (delegated, so it also works
  // for content other scripts render later). Hover-capable pointers only; ~350ms open delay,
  // stays open while the pointer is over the card, closes ~200ms after leaving; Escape closes;
  // also opens on keyboard focus. Touch: nothing — taps just follow the profile link. ----
  var cardCache = {};   // id → Promise of the /card response, for the page lifetime
  function fetchCard(id, fresh) {
    if (!cardCache[id] || fresh) cardCache[id] = api("/v1/users/" + id + "/card", fresh ? { cache: "reload" } : undefined)
      .catch(function (e) { delete cardCache[id]; throw e; });
    return cardCache[id];
  }
  // after the user edits their own profile: refetch past the browser's HTTP cache (the API lets
  // it keep cards for 60s), which also refreshes that cache, so other pages don't show the old card
  function invalidateCard(id) { if (id != null) fetchCard(id | 0, true).catch(function () {}); }
  var hoverOK = window.matchMedia ? window.matchMedia("(hover: hover)") : { matches: false };
  var uc = { el: null, trigger: null, id: 0, openT: 0, closeT: 0, seq: 0 };
  function ucEl() {
    if (!uc.el) {
      uc.el = el("div", { class: "fz-ucard", role: "dialog", "aria-label": "Profile card", hidden: true });
      uc.el.addEventListener("pointerenter", function () { clearTimeout(uc.closeT); });
      uc.el.addEventListener("pointerleave", function () { scheduleClose(); });
      document.body.appendChild(uc.el);
    }
    return uc.el;
  }
  function ucPlace() {
    if (!uc.el || uc.el.hidden || !uc.trigger || !uc.trigger.isConnected) return;
    var r = uc.trigger.getBoundingClientRect(), vw = document.documentElement.clientWidth, vh = window.innerHeight;
    if (r.bottom < 0 || r.top > vh) { ucClose(); return; }   // trigger scrolled out of view: don't leave the card stranded
    var w = uc.el.offsetWidth, h = uc.el.offsetHeight, m = 8;
    var top = r.bottom + m;                                   // below; flip above if it doesn't fit
    if (top + h > vh - m && r.top - m - h >= m) top = r.top - m - h;
    top = Math.max(m, Math.min(top, vh - h - m));
    var left = Math.max(m, Math.min(r.left, vw - w - m));    // left-aligned to the avatar, clamped
    uc.el.style.top = top + "px"; uc.el.style.left = left + "px";
  }
  function ucRender(d) {
    var u = (d && d.user) || d || {};
    var bio = d && d.bio != null ? d.bio : u.bio;
    var joined = (d && d.joined) || u.joined;
    var nComments = d && d.comments != null ? d.comments : 0, nLikes = d && d.likes != null ? d.likes : (u.xp || 0);
    var max = u.next_xp == null;
    var left = el("div", { class: "fz-uc-left" },
      avatar(u, 112, { link: false, card: false }),
      el("div", { class: "fz-uc-level", text: "Level " + (u.level || 1) }),
      el("div", { class: "fz-uc-next", text: max ? "Max level" : Math.max(0, u.next_xp - (u.xp || 0)) + " points to level up" }));
    var name = el("a", { class: "fz-uc-name", href: profileHref(u) }, el("span", { text: u.name || "User" }));
    if (u.admin) name.appendChild(el("span", { class: "cmt-tag", text: "author" }));
    var right = el("div", { class: "fz-uc-right" }, name,
      bio ? el("div", { class: "fz-uc-bio", text: bio }) : null,
      joined ? el("div", { class: "fz-uc-meta", text: "Joined " + new Date(joined).toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" }) }) : null,
      el("div", { class: "fz-uc-meta", text: nComments + (nComments === 1 ? " comment" : " comments") + " · " + nLikes + (nLikes === 1 ? " like" : " likes") }));
    uc.el.textContent = "";
    uc.el.append(left, right);
  }
  function ucOpen(trigger) {
    var id = +trigger.getAttribute("data-user-card");
    if (!id) return;
    var c = ucEl(), seq = ++uc.seq;
    uc.trigger = trigger; uc.id = id;
    fetchCard(id).then(function (d) {
      if (seq !== uc.seq) return;
      ucRender(d);
      c.hidden = false;
      ucPlace();
    }, function () {});
  }
  function ucClose() {
    clearTimeout(uc.openT); clearTimeout(uc.closeT);
    uc.seq++; uc.trigger = null;
    if (uc.el) uc.el.hidden = true;
  }
  function scheduleClose() { clearTimeout(uc.openT); clearTimeout(uc.closeT); uc.closeT = setTimeout(ucClose, 200); }
  function scheduleOpen(t, delay) {
    clearTimeout(uc.closeT);
    if (uc.trigger === t && uc.el && !uc.el.hidden) return;
    clearTimeout(uc.openT);
    uc.openT = setTimeout(function () { ucOpen(t); }, delay);
  }
  function cardTrigger(node) {
    var t = node && node.closest ? node.closest("[data-user-card]") : null;
    // the outermost of nested triggers (link wrapping the avatar span) so moving between them doesn't flicker
    while (t && t.parentElement && t.parentElement.closest("[data-user-card]") &&
           t.parentElement.closest("[data-user-card]").getAttribute("data-user-card") === t.getAttribute("data-user-card")) {
      t = t.parentElement.closest("[data-user-card]");
    }
    return t;
  }
  document.addEventListener("pointerover", function (e) {
    if (e.pointerType !== "mouse" || !hoverOK.matches) return;
    if (uc.el && uc.el.contains(e.target)) return;
    var t = cardTrigger(e.target);
    if (t) scheduleOpen(t, 350);
  });
  document.addEventListener("pointerout", function (e) {
    if (e.pointerType !== "mouse") return;
    var t = cardTrigger(e.target);
    if (!t) return;
    var to = e.relatedTarget;
    if (to && (t.contains(to) || (uc.el && uc.el.contains(to)))) return;
    scheduleClose();
  });
  document.addEventListener("focusin", function (e) {
    if (!hoverOK.matches) return;
    var t = cardTrigger(e.target);
    if (t && (t.matches("a,button,[tabindex]"))) scheduleOpen(t, 150);
  });
  document.addEventListener("focusout", function (e) {
    var t = cardTrigger(e.target);
    if (t && uc.trigger === t && !(uc.el && uc.el.contains(e.relatedTarget))) scheduleClose();
  });
  document.addEventListener("keydown", function (e) { if (e.key === "Escape" && uc.el && !uc.el.hidden) ucClose(); });
  window.addEventListener("scroll", function () { if (uc.el && !uc.el.hidden) ucPlace(); }, { passive: true });
  window.addEventListener("resize", function () { if (uc.el && !uc.el.hidden) ucPlace(); });

  // ---- header: Log in / Log out nav links + notifications bell (next to the theme toggle) ----
  var VERB = { reply: "replied to your comment on", like: "liked your comment on", comment: "commented on", page_like: "liked your article" };
  var unreadListeners = [];
  function setUnread(n) {
    n = Math.max(0, n | 0);
    meP = (meP || Promise.resolve({ user: null, unread: 0 })).then(function (m) { m.unread = n; return m; });
    paintBadge(n);
    unreadListeners.forEach(function (f) { try { f(n); } catch (e) {} });
  }
  function paintBadge(n) {
    var b = document.getElementById("fzBellBadge"), bell = document.getElementById("fzBell");
    if (!b || !bell) return;
    b.hidden = !(n > 0);
    b.textContent = n > 9 ? "9+" : String(n);
    bell.setAttribute("aria-label", n > 0 ? "Notifications (" + n + " unread)" : "Notifications");
  }
  function logout() {
    return api("/v1/auth/logout", { method: "POST" }).catch(function () {}).then(function () { location.reload(); });
  }

  // where a notification points: "/articles/x/#comment-12" (an article like: just "/articles/x/")
  function notifHref(n) {
    var p = safePath(n.page || "") || "/";
    return p + (n.comment_id ? "#comment-" + (n.comment_id | 0) : "");
  }
  // follow a notification link: mark it read first, and if it's on the current page just
  // jump to the comment (fazon-comments.js scrolls + highlights on hashchange)
  function openNotif(n, href) {
    var go = function () {
      var u = new URL(href, location.href);
      if (u.pathname === location.pathname && u.search === location.search) {
        if (location.hash === u.hash) window.dispatchEvent(new HashChangeEvent("hashchange"));
        else location.hash = u.hash;
      } else location.href = href;
    };
    if (n.read) { go(); return; }
    n.read = true;
    api("/v1/notifications/read", { method: "POST", body: { ids: [n.id] }, keepalive: true })
      .then(function (d) { if (d && typeof d.unread === "number") setUnread(d.unread); }, function () {})
      .then(go);
  }

  // ---- dropdown panels (bell + account menu): one open at a time; outside click / Escape close ----
  var openMenu = null;
  function dropdown(wrap, btn, panel, onOpen) {
    var m = {
      open: false,
      place: function () {
        // fixed panel under its button, right-aligned to it; phone width: 16px gutters
        var r = btn.getBoundingClientRect(), vw = document.documentElement.clientWidth;
        var top = Math.max(8, r.bottom + 8);
        panel.style.top = top + "px";
        if (vw <= 520) { panel.style.left = "16px"; panel.style.right = "16px"; }
        else { panel.style.left = "auto"; panel.style.right = Math.max(16, vw - r.right) + "px"; }
        panel.style.maxHeight = Math.max(160, window.innerHeight - top - 16) + "px";
      },
      show: function () {
        if (openMenu && openMenu !== m) openMenu.close(false);
        openMenu = m; m.open = true;
        panel.hidden = false; btn.setAttribute("aria-expanded", "true");
        if (onOpen) onOpen();
        m.place();
      },
      close: function (focusBtn) {
        if (!m.open) return;
        m.open = false; if (openMenu === m) openMenu = null;
        panel.hidden = true; btn.setAttribute("aria-expanded", "false");
        if (focusBtn) btn.focus();
      }
    };
    btn.addEventListener("click", function () { if (m.open) m.close(false); else m.show(); });
    document.addEventListener("click", function (e) { if (m.open && !wrap.contains(e.target)) m.close(false); });
    return m;
  }
  document.addEventListener("keydown", function (e) { if (openMenu && e.key === "Escape") { e.preventDefault(); openMenu.close(true); } });
  window.addEventListener("resize", function () { if (openMenu) openMenu.place(); });
  window.addEventListener("scroll", function () { if (openMenu) openMenu.place(); }, { passive: true });

  function initBell() {
    var wrap = document.getElementById("fzBellWrap"), bell = document.getElementById("fzBell"),
        panel = document.getElementById("fzBellPanel");
    if (!wrap || !bell || !panel) return;
    var loadSeq = 0, menu;

    function item(n) {
      var href = notifHref(n);
      var a = el("a", { class: "fz-np-item" + (n.read ? "" : " unread"), href: href },
        el("span", { class: "fz-np-dot", "aria-hidden": "true" }),
        avatar(n.actor || null, 30, { ring: false, badge: false, link: false }),
        el("span", { class: "fz-np-text" },
          el("span", { class: "fz-np-line" },
            el("b", { text: (n.actor && n.actor.name) || "Someone" }), " ", VERB[n.kind] || "commented on", " ",
            el("b", { text: n.page_title || n.page || "a page" })),
          n.snippet ? el("span", { class: "fz-np-snip", text: "“" + n.snippet + "”" }) : null,
          el("span", { class: "fz-np-time" }, timeEl(n.created), n.read ? null : el("span", { class: "fz-sr", text: " (unread)" }))));
      a.addEventListener("click", function (e) {
        if (e.ctrlKey || e.metaKey || e.shiftKey || e.button !== 0) return;
        e.preventDefault();
        a.classList.remove("unread");
        menu.close(false);
        openNotif(n, href);
      });
      return a;
    }
    function render() {
      var seq = ++loadSeq;
      var all = el("button", { type: "button", class: "fz-linkbtn fz-np-all", text: "Mark all as read", disabled: true });
      var list = el("div", { class: "fz-np-list" }, el("p", { class: "fz-np-empty", text: "Loading…" }));
      panel.textContent = "";
      panel.append(
        el("div", { class: "fz-np-head" }, el("span", { class: "fz-np-title", text: "Notifications" }), all),
        list,
        el("div", { class: "fz-np-foot" }, el("a", { href: "/account/notifications/", text: "See all" })));
      all.addEventListener("click", function () {
        all.disabled = true;
        api("/v1/notifications/read", { method: "POST", body: { all: true } }).then(function (d) {
          list.querySelectorAll(".fz-np-item.unread").forEach(function (x) { x.classList.remove("unread"); var s = x.querySelector(".fz-sr"); if (s) s.remove(); });
          setUnread(d && typeof d.unread === "number" ? d.unread : 0);
        }, function () { all.disabled = false; });
      });
      api("/v1/notifications?limit=20").then(function (d) {
        if (seq !== loadSeq) return;
        var ns = (d && d.notifications) || [];
        list.textContent = "";
        if (!ns.length) list.appendChild(el("p", { class: "fz-np-empty", text: "No notifications yet" }));
        ns.forEach(function (n) { list.appendChild(item(n)); });
        if (d && typeof d.unread === "number") setUnread(d.unread);
        all.disabled = !(d && d.unread > 0);
        if (menu.open) menu.place();
      }, function (e) {
        if (seq !== loadSeq) return;
        list.textContent = "";
        list.appendChild(el("p", { class: "fz-np-empty fz-error", text: e.status === 401 ? "You've been logged out." : e.message }));
      });
    }
    menu = dropdown(wrap, bell, panel, render);
    wrap.hidden = false;

    // keep the count fresh without hammering the API: when the tab comes back into view
    // (at most every 15s), and once a minute while it's visible
    var last = Date.now();
    function refresh(minGap) {
      if (document.visibilityState !== "visible" || Date.now() - last < minGap) return;
      last = Date.now();
      me(true).then(function (m) { if (!m.offline && m.user) paintBadge(m.unread); });
    }
    document.addEventListener("visibilitychange", function () { refresh(15000); });
    window.addEventListener("focus", function () { refresh(15000); });
    setInterval(function () { refresh(55000); }, 60000);
  }

  var acct = null;   // {btn, who} once the account menu exists
  function paintAccount(user) {
    if (!acct || !user) return;
    var name = String(user.name || user.email || "?");
    acct.btn.textContent = "";
    acct.btn.appendChild(avatar(user, 32, { link: false, card: false }));
    acct.btn.setAttribute("aria-label", "Account: " + name);
    acct.btn.title = name;
    acct.who.textContent = "";
    acct.who.append(avatar(user, 44, { link: false, card: false }),
      el("span", { class: "fz-acct-id" },
        el("b", { text: name }),
        user.email ? el("span", { class: "fz-acct-email", text: user.email }) : null,
        user.level != null ? el("span", { class: "fz-acct-email", text: "Level " + user.level + " · " + (user.xp || 0) + " points" }) : null));
    acct.profile.href = profileHref(user);
  }
  function initAccount(user) {
    var wrap = document.getElementById("fzAcctWrap");
    if (!wrap) return;
    wrap.textContent = "";
    if (!user) {
      // [Sign up] [Log in]; on narrow phones only "Log in" (style.css), whose page links to sign-up
      wrap.append(el("a", { href: signupHref(), class: "fz-login-pill fz-signup-pill", text: "Sign up" }),
        el("a", { href: loginHref(), class: "fz-login-pill", text: "Log in" }));
      wrap.hidden = false;
      return;
    }
    var btn = el("button", { type: "button", class: "fz-acct-btn", id: "fzAcctBtn", "aria-haspopup": "true",
      "aria-expanded": "false", "aria-controls": "fzAcctPanel" });
    var who = el("div", { class: "fz-acct-who" });
    var prof = el("a", { class: "fz-acct-item", href: profileHref(user), text: "Your profile" });
    var panel = el("div", { class: "fz-panel fz-acct-panel", id: "fzAcctPanel", role: "region", "aria-label": "Account", hidden: true },
      who, prof,
      el("a", { class: "fz-acct-item", href: "/account/notifications/", text: "Notifications" }),
      isStaff(user) ? el("a", { class: "fz-acct-item", href: "/admin/", text: "Admin" }) : null,
      el("button", { type: "button", class: "fz-acct-item", text: "Log out", onclick: function () { logout(); } }));
    acct = { btn: btn, who: who, profile: prof };
    paintAccount(user);
    wrap.append(btn, panel);
    dropdown(wrap, btn, panel);
    wrap.hidden = false;
  }
  // after a rename / new picture: update the cached /v1/me user (keeping email) and the header
  function updateUser(user) {
    if (!user) return;
    invalidateCard(user.id);
    meP = (meP || Promise.resolve({ user: null, unread: 0 })).then(function (m) {
      if (m.user && m.user.id === user.id) m.user = Object.assign({}, m.user, user, { email: user.email || m.user.email });
      paintAccount(m.user);
      return m;
    });
  }

  function initHeader() {
    me().then(function (m) {
      if (m.offline) return;  // API unreachable: show nothing
      if (m.user) {
        initBell(); paintBadge(m.unread);
        // nav links for logged-in users only (menu params.authOnly in hugo.toml); logout reloads
        document.querySelectorAll(".site-nav [data-fz-auth]").forEach(function (a) { a.hidden = false; });
      }
      initAccount(m.user);
      var row = document.getElementById("fzClusterRow");
      if (row) row.hidden = false;
    });
  }

  window.Fazon = {
    base: BASE,
    api: api,
    me: me,
    setUnread: setUnread,
    onUnread: function (f) { unreadListeners.push(f); },
    logout: logout,
    notifHref: notifHref,
    avatar: avatar,
    avatarUrl: avatarUrl,
    profileHref: profileHref,
    updateUser: updateUser,
    invalidateCard: invalidateCard,
    openNotif: openNotif,
    loginHref: loginHref,
    signupHref: signupHref,
    isStaff: isStaff,
    esc: esc,
    el: el,
    safePath: safePath,
    relTime: relTime,
    absTime: absTime,
    timeEl: timeEl
  };

  me();  // start the request early
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", initHeader);
  else initHeader();
})();
