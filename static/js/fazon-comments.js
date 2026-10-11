// Comment thread for articles (layouts/_partials/comments.html). Everything user-supplied is
// put in the DOM with textContent (via Fazon.el); bodies keep their newlines via CSS pre-wrap.
(function () {
  "use strict";
  var F = window.Fazon, el = F && F.el;
  var root = document.getElementById("comments");
  if (!F || !root) return;

  // The API takes the page's title from the published site (/comment-pages.json), not from us.
  var PAGE = root.dataset.page;
  var MAX = 5000;
  var listEl = document.getElementById("cmtList");
  var composerEl = document.getElementById("cmtComposer");
  var headEl = document.getElementById("commentsH");
  var user = null, comments = [], byId = {};

  var runes = function (s) { return Array.from(s).length; };
  var loginLink = function (text) { return el("a", { href: F.loginHref(location.pathname + location.search + "#comments"), text: text || "Log in" }); };

  function countLive() { return comments.filter(function (c) { return !c.deleted; }).length; }
  function updateHeading() {
    var n = countLive();
    headEl.textContent = n === 0 ? "Comments" : n === 1 ? "1 comment" : n + " comments";
  }
  function index() { byId = {}; comments.forEach(function (c) { byId[c.id] = c; }); }
  function childrenOf(id) { return comments.filter(function (c) { return c.parent === id; }); }

  // drop deleted comments that no longer have any live descendants (what the API would do)
  function prune() {
    var live = {};
    var hasLive = function (c) {
      if (c.id in live) return live[c.id];
      live[c.id] = false;
      var r = !c.deleted || childrenOf(c.id).some(hasLive);
      return (live[c.id] = r);
    };
    comments = comments.filter(hasLive);
    index();
  }

  // ---- composer (top level or inline reply) ----
  function composer(opts) {
    var ta = el("textarea", { class: "fz-textarea", rows: opts.parent ? 3 : 4,
      placeholder: opts.parent ? "Write a reply…" : "Add a comment…", "aria-label": opts.parent ? "Reply" : "Comment" });
    var count = el("span", { class: "cmt-count", text: "0 / " + MAX });
    var err = el("span", { class: "fz-error", role: "alert" });
    var post = el("button", { class: "fz-btn primary", type: "button", text: opts.parent ? "Reply" : "Post", disabled: true });
    var cancel = opts.onCancel ? el("button", { class: "fz-btn", type: "button", text: "Cancel", onclick: opts.onCancel }) : null;
    var sending = false;
    function update() {
      var n = runes(ta.value), t = ta.value.trim();
      count.textContent = n + " / " + MAX;
      count.classList.toggle("over", n > MAX);
      post.disabled = sending || !t || runes(t) > MAX;
    }
    function send() {
      update();
      if (post.disabled) return;
      sending = true; update(); err.textContent = "";
      post.textContent = "Sending…";
      F.api("/v1/comments", { method: "POST", body: { page: PAGE, parent: opts.parent || null, body: ta.value.trim() } })
        .then(function (c) {
          ta.value = "";
          opts.onDone(c);
        }, function (e) {
          err.textContent = "";
          if (e.status === 401) { err.append("Your login has expired — ", loginLink("log in again"), "."); }
          else err.textContent = e.message;
        })
        .then(function () { sending = false; post.textContent = opts.parent ? "Reply" : "Post"; update(); });
    }
    ta.addEventListener("input", update);
    ta.addEventListener("keydown", function (e) { if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); send(); } });
    post.addEventListener("click", send);
    var box = el("div", { class: opts.parent ? "cmt-reply-form" : "" }, ta,
      el("div", { class: "cmt-composer-foot" }, count, err, cancel, post));
    box.focusInput = function () { ta.focus(); };
    return box;
  }

  // muted by a moderator (user.muted): no composer, no replies; likes still work
  var MUTED = "You've been blocked from commenting.";
  function renderTopComposer() {
    composerEl.textContent = "";
    if (user && user.muted) {
      composerEl.appendChild(el("p", { class: "cmt-empty cmt-muted", text: MUTED }));
    } else if (user) {
      composerEl.appendChild(composer({ onDone: function (c) { addComment(c); } }));
    } else {
      composerEl.appendChild(el("p", { class: "cmt-empty" }, loginLink("Log in to comment"), "."));
    }
  }

  // ---- one comment (and its subtree) ----
  function hint(actions, verb) {
    var old = actions.querySelector(".cmt-hint"); if (old) old.remove();
    actions.appendChild(el("span", { class: "cmt-hint fz-muted" }, loginLink(), " to " + verb + "."));
  }

  function renderComment(c, level) {
    var wrap = el("div", { class: "cmt" + (c.deleted ? " cmt-deleted" : ""), id: "comment-" + c.id });
    var head = el("div", { class: "cmt-head" });
    if (c.deleted) head.appendChild(el("span", { class: "cmt-author fz-muted", text: "[deleted]" }));
    else {
      head.appendChild(c.author
        ? el("a", { class: "cmt-author", href: F.profileHref(c.author), "data-user-card": String(c.author.id | 0), text: c.author.name || "Someone" })
        : el("span", { class: "cmt-author", text: "Someone" }));
      if (c.author && c.author.admin) head.appendChild(el("span", { class: "cmt-tag", text: "author" }));
    }
    head.appendChild(el("a", { href: "#comment-" + c.id, class: "fz-muted", style: "color:inherit" }, F.timeEl(c.created)));
    var body = el("div", { class: "cmt-body", text: c.deleted ? "This comment was deleted." : c.body });
    var main = el("div", { class: "cmt-main" }, head, body);
    var av = c.deleted || !c.author ? F.avatar(null, 36) : F.avatar(c.author, 36);   // links to the profile + hover card
    av.classList.add("cmt-av");
    var self = el("div", { class: "cmt-self" }, av, main);
    var replySlot = el("div");
    var kids = el("div", { class: "cmt-children" + (level + 1 > 4 ? " deep" : "") + (level + 1 > 8 ? " flat" : "") });

    if (!c.deleted) {
      var actions = el("div", { class: "cmt-actions" });
      var like = el("button", { class: "fz-linkbtn cmt-like", type: "button" });
      var paintLike = function () {
        like.textContent = "♥ " + (c.likes || 0);
        like.classList.toggle("on", !!c.liked);
        like.setAttribute("aria-pressed", c.liked ? "true" : "false");
        like.title = c.liked ? "Unlike" : "Like";
      };
      paintLike();
      like.addEventListener("click", function () {
        if (!user) { hint(actions, "like comments"); return; }
        like.disabled = true;
        F.api("/v1/comments/" + c.id + "/like", { method: c.liked ? "DELETE" : "POST" }).then(function (d) {
          c.likes = d.likes; c.liked = d.liked; paintLike();
        }, function (e) {
          if (e.status === 401) hint(actions, "like comments");
          else { var old = actions.querySelector(".fz-error"); if (old) old.remove(); actions.appendChild(el("span", { class: "fz-error", text: e.message })); }
        }).then(function () { like.disabled = false; });
      });
      actions.appendChild(like);

      var replyBtn = el("button", { class: "fz-linkbtn", type: "button", text: "Reply" });
      replyBtn.addEventListener("click", function () {
        if (!user) { hint(actions, "reply"); return; }
        if (user.muted) {
          var old = actions.querySelector(".cmt-hint"); if (old) old.remove();
          actions.appendChild(el("span", { class: "cmt-hint fz-muted", text: MUTED }));
          return;
        }
        if (replySlot.firstChild) { replySlot.textContent = ""; return; }
        var form = composer({
          parent: c.id,
          onCancel: function () { replySlot.textContent = ""; },
          onDone: function (nc) { replySlot.textContent = ""; addComment(nc, kids, level + 1); }
        });
        replySlot.appendChild(form);
        form.focusInput();
      });
      actions.appendChild(replyBtn);

      if (user && (c.mine || user.admin || user.role === "moderator")) {   // moderators may delete any comment
        var del = el("button", { class: "fz-linkbtn", type: "button", text: "Delete" });
        del.addEventListener("click", function () {
          if (!confirm("Delete this comment?")) return;
          del.disabled = true;
          F.api("/v1/comments/" + c.id, { method: "DELETE" }).then(function () {
            c.deleted = true; c.body = ""; c.author = null; c.likes = 0; c.liked = false;
            prune(); renderList();
          }, function (e) {
            del.disabled = false;
            var old = actions.querySelector(".fz-error"); if (old) old.remove();
            actions.appendChild(el("span", { class: "fz-error", text: e.message }));
          });
        });
        actions.appendChild(del);
      }
      main.appendChild(actions);
    }

    wrap.append(self, replySlot, kids);
    childrenOf(c.id).forEach(function (k) { kids.appendChild(renderComment(k, level + 1)); });
    return wrap;
  }

  function renderList() {
    listEl.textContent = "";
    var roots = comments.filter(function (c) { return c.parent == null || !(c.parent in byId); });
    roots.forEach(function (c) { listEl.appendChild(renderComment(c, 0)); });
    if (!roots.length) listEl.appendChild(el("p", { class: "cmt-empty", text: "No comments yet." }));
    updateHeading();
  }

  // a freshly posted comment: append it in place (keeps any other open reply boxes intact)
  function addComment(c, container, level) {
    comments.push(c); byId[c.id] = c;
    if (!container) {
      var empty = listEl.querySelector(":scope > .cmt-empty"); if (empty) empty.remove();
      container = listEl; level = 0;
    }
    var node = renderComment(c, level);
    container.appendChild(node);
    updateHeading();
    flash(node, false);
  }

  var flashTimer;
  function flash(node, scroll) {
    if (!node) return;
    if (scroll) node.scrollIntoView({ block: "center", behavior: "smooth" });
    document.querySelectorAll(".cmt.flash").forEach(function (n) { n.classList.remove("flash"); });
    node.classList.add("flash");
    clearTimeout(flashTimer);
    flashTimer = setTimeout(function () { node.classList.remove("flash"); }, 2200);
  }
  function flashFromHash() {
    var m = /^#comment-(\d+)$/.exec(location.hash);
    if (m) flash(document.getElementById("comment-" + m[1]), true);
  }
  window.addEventListener("hashchange", flashFromHash);

  Promise.all([
    F.me(),
    F.api("/v1/comments?page=" + encodeURIComponent(PAGE)).then(function (d) { return (d && d.comments) || []; }, function (e) { return e; })
  ]).then(function (r) {
    user = r[0].user;
    if (r[1] instanceof Error) {
      listEl.textContent = "";
      listEl.appendChild(el("p", { class: "cmt-empty", text: "Comments couldn't be loaded right now." }));
      return;
    }
    comments = r[1];
    index();
    renderTopComposer();
    renderList();
    flashFromHash();
  });
})();
