// Profile "Developers" section (owner only): personal API keys for scripting your own Track data.
// Needs js/fazon-api.js (window.Fazon). Docs: /developers/. API: GET/POST /v1/me/api-keys,
// DELETE /v1/me/api-keys/{id} (session cookie, like the other /v1/me routes).
//   FazonDevKeys.render(sectionEl)
(function () {
  "use strict";
  var F = window.Fazon, el = F.el;
  var SCOPES = { read: "Read only", write: "Read & write" };
  var runes = function (s) { return Array.from(String(s || "")).length; };
  var fmtDate = function (iso) {
    var d = new Date(iso);
    return isNaN(d) ? "" : d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
  };

  // Copies text; falls back to selecting it in a hidden textarea where the async API is missing
  // or refused (http:// pages other than localhost, older browsers).
  function copyText(text) {
    if (navigator.clipboard && window.isSecureContext) return navigator.clipboard.writeText(text);
    return new Promise(function (res, rej) {
      var ta = el("textarea", { class: "dk-copybuf", readonly: true, "aria-hidden": "true" });
      ta.value = text; document.body.appendChild(ta); ta.select();
      var ok = false; try { ok = document.execCommand("copy"); } catch (e) {}
      ta.remove(); ok ? res() : rej(new Error("copy failed"));
    });
  }

  function render(sec) {
    var keys = [], max = 5, fresh = null;   // fresh = {key, api_key} just created (shown once)
    var err = el("p", { class: "fz-error dk-err", role: "alert" });
    var body = el("div", { class: "dk-body" }, el("p", { class: "fz-muted", text: "Loading…" }));
    sec.textContent = "";
    sec.append(
      el("h2", { text: "Developers" }),
      el("p", { class: "dk-intro" },
        "Personal API keys let your own scripts read and update your Track data — hours, sub-categories, summaries and goals. ",
        el("a", { href: "/developers/", text: "Read the API docs" }), "."),
      body, err);

    function load() {
      return F.api("/v1/me/api-keys").then(function (d) {
        keys = (d && d.keys) || []; max = (d && d.max) || 5; paint();
      }, function (e) { body.textContent = ""; err.textContent = e.message; });
    }

    // ---- the new key, shown once ----
    function reveal() {
      var k = fresh.key, copyBtn = el("button", { type: "button", class: "fz-btn primary dk-copy", text: "Copy" });
      var status = el("span", { class: "dk-copied", "aria-live": "polite" });
      var code = el("code", { class: "dk-key", tabindex: "0", text: k, "aria-label": "Your new API key" });
      copyBtn.addEventListener("click", function () {
        copyText(k).then(function () {
          status.classList.add("fz-sr"); status.textContent = "Copied to the clipboard"; copyBtn.textContent = "Copied ✓";
          setTimeout(function () { copyBtn.textContent = "Copy"; status.textContent = ""; }, 2500);
        }, function () {
          status.classList.remove("fz-sr"); status.textContent = "Couldn't copy — select the key and copy it by hand.";
          var r = document.createRange(); r.selectNodeContents(code);
          var s = getSelection(); s.removeAllRanges(); s.addRange(r);
        });
      });
      var done = el("button", { type: "button", class: "fz-btn", text: "Done — I've saved it" });
      done.addEventListener("click", function () { fresh = null; paint(); });
      return el("div", { class: "dk-reveal", role: "region", "aria-label": "Your new API key" },
        el("div", { class: "dk-reveal-h" }, "Your new key ", el("b", { class: "dk-name", text: "“" + fresh.api_key.name + "”" }),
          " (" + (SCOPES[fresh.api_key.scope] || fresh.api_key.scope) + ")"),
        el("p", { class: "dk-warn" }, el("b", { text: "Copy it now — you won't see it again." }),
          " Only a fingerprint of it is stored, so if you lose it, revoke it and create a new one. Keep it secret: anyone with it can " +
          (fresh.api_key.scope === "write" ? "read and change your Track data." : "read your Track data, private summaries included.")),
        el("div", { class: "dk-keyrow" }, code, copyBtn),
        status, el("div", { class: "dk-reveal-foot" }, done));
    }

    // ---- one key row, with an in-place revoke confirmation ----
    function row(k) {
      var li = el("li", { class: "dk-item" });
      function normal() {
        li.textContent = "";
        var revoke = el("button", { type: "button", class: "fz-btn dk-revoke", text: "Revoke", "aria-label": "Revoke " + k.name });
        revoke.addEventListener("click", confirmUI);
        li.append(
          el("div", { class: "dk-info" },
            el("div", { class: "dk-line1" }, el("b", { class: "dk-name", text: k.name }),
              el("span", { class: "dk-scope dk-scope-" + (k.scope === "write" ? "write" : "read"), text: SCOPES[k.scope] || k.scope })),
            el("div", { class: "dk-meta" },
              el("code", { class: "dk-prefix", text: k.prefix + "…" }),
              el("span", { text: "Created " + fmtDate(k.created) }),
              k.last_used ? el("span", {}, "Last used ", F.timeEl(k.last_used)) : el("span", { text: "Never used" }))),
          revoke);
        return revoke;
      }
      function confirmUI() {
        err.textContent = "";
        li.textContent = "";
        var yes = el("button", { type: "button", class: "fz-btn dk-danger", text: "Revoke key" });
        var no = el("button", { type: "button", class: "fz-btn", text: "Cancel" });
        li.classList.add("confirming");
        li.append(el("div", { class: "dk-confirm", role: "group", "aria-label": "Confirm revoking " + k.name },
          el("p", {}, "Revoke ", el("b", { class: "dk-name", text: k.name }), "? Scripts using it stop working immediately. This can't be undone."),
          el("div", { class: "fz-row" }, yes, no)));
        no.addEventListener("click", function () { li.classList.remove("confirming"); normal().focus(); });
        li.addEventListener("keydown", function esc(e) {
          if (e.key === "Escape" && li.classList.contains("confirming")) { e.preventDefault(); li.removeEventListener("keydown", esc); no.click(); }
        });
        yes.addEventListener("click", function () {
          yes.disabled = no.disabled = true; yes.textContent = "Revoking…";
          F.api("/v1/me/api-keys/" + (k.id | 0), { method: "DELETE" }).then(function () {
            if (fresh && fresh.api_key.id === k.id) fresh = null;
            return load();
          }, function (e) {
            err.textContent = e.message; li.classList.remove("confirming"); normal();
          });
        });
        no.focus();
      }
      normal();
      return li;
    }

    // ---- create form ----
    function form() {
      var full = keys.length >= max;
      var name = el("input", { class: "fz-input dk-name-input", type: "text", maxlength: "40", id: "dkName",
        placeholder: "e.g. Anki sync script", autocomplete: "off", disabled: full });
      var scopeName = "dkScope";
      var radio = function (v, label, hint, checked) {
        return el("label", { class: "dk-radio" },
          el("input", { type: "radio", name: scopeName, value: v, checked: checked, disabled: full }),
          el("span", {}, el("b", { text: label }), el("span", { class: "fz-muted", text: " — " + hint })));
      };
      var create = el("button", { type: "submit", class: "fz-btn primary", text: "Create key", disabled: full });
      var f = el("form", { class: "dk-form", novalidate: true },
        el("div", { class: "dk-form-h" }, el("b", { text: "New key" }),
          el("span", { class: "fz-muted dk-count", text: keys.length + " of " + max + " keys" })),
        el("label", { for: "dkName", class: "dk-label", text: "Name" }), name,
        el("fieldset", { class: "dk-scopes" }, el("legend", { class: "dk-label", text: "Access" }),
          radio("read", "Read only", "fetch your weeks and sub-categories", true),
          radio("write", "Read & write", "also log hours, summaries, goals and sub-categories", false)),
        full ? el("p", { class: "fz-muted dk-full", text: "You have " + max + " keys, the most allowed. Revoke one to create another." }) : null,
        el("div", { class: "fz-row" }, create));
      f.addEventListener("submit", function (e) {
        e.preventDefault();
        if (full) return;
        var v = name.value.trim(), sc = (f.querySelector("input[name=" + scopeName + "]:checked") || {}).value || "read";
        err.textContent = "";
        if (!v) { err.textContent = "Give the key a name, so you can tell your keys apart."; name.focus(); return; }
        if (runes(v) > 40) { err.textContent = "Key names are at most 40 characters."; name.focus(); return; }
        create.disabled = true; create.textContent = "Creating…";
        F.api("/v1/me/api-keys", { method: "POST", body: { name: v, scope: sc } }).then(function (d) {
          fresh = d; return load().then(function () {
            var c = sec.querySelector(".dk-copy"); if (c) c.focus({ preventScroll: true });
            var r = sec.querySelector(".dk-reveal"); if (r) r.scrollIntoView({ block: "nearest", behavior: "smooth" });
          });
        }, function (e) { err.textContent = e.message; create.disabled = false; create.textContent = "Create key"; });
      });
      return f;
    }

    function paint() {
      body.textContent = "";
      if (fresh) body.append(reveal());
      var list = el("ul", { class: "dk-list", "aria-label": "Your API keys" });
      keys.forEach(function (k) { list.appendChild(row(k)); });
      if (!keys.length) list.appendChild(el("li", { class: "dk-empty", text: "No API keys yet." }));
      body.append(list, form());
    }

    load();
  }

  window.FazonDevKeys = { render: render };
})();
