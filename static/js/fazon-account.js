// Profile "Your data" section (owner only): download everything stored about the account
// (GET /v1/me/export) and a danger zone to delete it (DELETE /v1/me; confirmed by typing your
// display name or "delete", in the page — no window.confirm). Needs js/fazon-api.js.
//   FazonAccount.render(sectionEl, user)   user = the /v1/me user (name, role)
(function () {
  "use strict";
  var F = window.Fazon, el = F.el;

  // The API sends the file as an attachment, but it lives on another origin (api.fazon.nexus),
  // where a plain link can't carry the download name; fetch it and save it from a blob instead.
  function download(data) {
    var name = "fazon-nexus-export-" + new Date().toISOString().slice(0, 10) + ".json";
    var url = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2) + "\n"], { type: "application/json" }));
    var a = el("a", { href: url, download: name, hidden: true });
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(function () { URL.revokeObjectURL(url); }, 10000);
    return name;
  }

  function render(sec, user) {
    var status = el("span", { class: "acc-status fz-muted", role: "status", "aria-live": "polite" });
    var exportBtn = el("button", { type: "button", class: "fz-btn", text: "Download my data" });
    exportBtn.addEventListener("click", function () {
      exportBtn.disabled = true; status.className = "acc-status fz-muted"; status.textContent = "Preparing your file…";
      F.api("/v1/me/export").then(function (d) {
        status.textContent = "Saved " + download(d) + ".";
      }, function (e) {
        status.className = "acc-status fz-error"; status.textContent = e.message;
      }).then(function () { exportBtn.disabled = false; });
    });

    var danger = el("div", { class: "acc-danger", role: "region", "aria-label": "Danger zone" });
    function closed() {
      danger.textContent = "";
      var open = el("button", { type: "button", class: "fz-btn acc-danger-btn", text: "Delete my account…" });
      // the name may have been edited on this page since: take the current one from /v1/me's cache
      open.addEventListener("click", function () { F.me().then(function (m) { if (m.user) user = m.user; confirmUI(); }); });
      danger.append(el("h3", { text: "Danger zone" }),
        el("p", {}, "Deleting your account removes your profile, picture, Track log, Kyōtsu history, likes, notifications, " +
          "API keys and comments straight away. Comments that other people replied to stay as “[deleted]” with no name, so " +
          "their replies still make sense. This can't be undone — ", el("a", { href: "#", text: "download your data", onclick: function (e) { e.preventDefault(); exportBtn.focus(); exportBtn.click(); } }), " first if you want a copy."),
        open);
      return open;
    }
    function confirmUI() {
      var name = String(user.name || "");
      var inp = el("input", { class: "fz-input", type: "text", id: "accDelConfirm", autocomplete: "off", spellcheck: "false" });
      var go = el("button", { type: "button", class: "fz-btn acc-danger-btn solid", text: "Delete my account", disabled: true });
      var cancel = el("button", { type: "button", class: "fz-btn", text: "Cancel" });
      var err = el("p", { class: "fz-error acc-err", role: "alert" });
      var ok = function () { var v = inp.value.trim(); return v !== "" && (v === name.trim() || v.toLowerCase() === "delete"); };
      inp.addEventListener("input", function () { go.disabled = !ok(); });
      inp.addEventListener("keydown", function (e) {
        if (e.key === "Enter" && ok()) { e.preventDefault(); go.click(); }
        else if (e.key === "Escape") { e.preventDefault(); closed().focus(); }
      });
      cancel.addEventListener("click", function () { closed().focus(); });
      go.addEventListener("click", function () {
        if (!ok()) return;
        go.disabled = true; cancel.disabled = true; inp.disabled = true; go.textContent = "Deleting…"; err.textContent = "";
        F.api("/v1/me", { method: "DELETE" }).then(function () {
          danger.textContent = "";
          danger.append(el("h3", { text: "Account deleted" }), el("p", { text: "Your account and its data are gone. Taking you to the home page…" }));
          setTimeout(function () { location.replace("/"); }, 1500);
        }, function (e) {
          err.textContent = e.message; go.textContent = "Delete my account";
          cancel.disabled = false; inp.disabled = false; go.disabled = !ok();
        });
      });
      danger.textContent = "";
      danger.append(el("h3", { text: "Danger zone" }),
        el("p", {}, el("b", { text: "Delete your account for good?" }), " Everything listed above goes at once and can't be brought back."),
        el("div", { class: "acc-confirm" },
          el("label", { for: "accDelConfirm" }, "Type your display name (", el("b", { class: "dk-name", text: name }), ") or “delete” to confirm:"),
          inp, el("div", { class: "fz-row" }, go, cancel), err));
      inp.focus();
    }
    closed();

    sec.textContent = "";
    sec.append(
      el("h2", { text: "Your data" }),
      el("p", { class: "acc-intro" }, "Download a copy of everything stored about your account (one JSON file), or delete the account. ",
        el("a", { href: "/privacy/", text: "Privacy" }), " explains what's stored and who can see it."),
      el("div", { class: "acc-row" }, exportBtn, status),
      danger);
  }

  window.FazonAccount = { render: render };
})();
