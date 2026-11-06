// Standard | Wide (the same switch as the tracker extension's dashboard), saved in localStorage.
// Wide is a desktop layout, so html.layout-wide only goes on in a landscape window of 1200px or
// more; anywhere else the page is standard whatever is saved. Loaded blocking in <head>, so the
// page never paints in the wrong layout. Wide's styling lives in css/wide*.css.
//
// For page scripts, wide only:
//   FazonWide.active()                 is wide on right now?
//   document "fz-layout" event         fired when wide goes on or off; re-render from state
//   FazonWide.key("j", "Next row", fn) a shortcut, ignored while typing, listed on the ? sheet.
//                                      Single-character ones (and ?, g-sequences) can be turned off
//                                      on the sheet (WCAG 2.1.4); Ctrl+/ opens the sheet either way
//   FazonWide.tools()                  the page bar's tools box (#wBarTools)
//   data-tip="text" on any element     a tooltip on hover and keyboard focus
(function () {
    var root = document.documentElement;
    var desktop = window.matchMedia("(min-width: 1200px) and (min-aspect-ratio: 1001/1000)");
    var saved = "standard";
    try { saved = localStorage.getItem("fz-layout") === "wide" ? "wide" : "standard"; } catch (e) { /* storage blocked */ }

    function active() { return root.classList.contains("layout-wide"); }
    function apply() {
        var was = active();
        root.classList.toggle("layout-wide", saved === "wide" && desktop.matches);
        if (was !== active()) document.dispatchEvent(new CustomEvent("fz-layout", { detail: { wide: active() } }));
    }
    function paint() {
        document.querySelectorAll(".fz-layout-seg button[data-layout]").forEach(function (b) {
            b.setAttribute("aria-pressed", String(b.dataset.layout === saved));
        });
    }
    apply();
    desktop.addEventListener("change", apply);
    document.addEventListener("click", function (e) {
        var b = e.target.closest && e.target.closest(".fz-layout-seg button[data-layout]");
        if (!b) return;
        saved = b.dataset.layout;
        apply();
        paint();
        try { localStorage.setItem("fz-layout", saved); } catch (err) { /* ignore */ }
    });
    document.addEventListener("DOMContentLoaded", function () {
        paint();
        // the sidebar marks where you are
        var here = location.pathname;
        document.querySelectorAll(".site-nav a").forEach(function (a) {
            var to = a.pathname;
            if (to === "/" ? here === "/" : here.indexOf(to) === 0) a.setAttribute("aria-current", "page");
        });
    });

    // ---- shortcuts ----
    var keys = {}, gPending = null, charKeysOff = false;
    try { charKeysOff = localStorage.getItem("fz-keys") === "off"; } catch (e) { /* storage blocked */ }
    function charKey(k) { return k.length === 1 && k !== " "; }
    var GO = { h: ["/", "Home"], a: ["/articles/", "Articles"], k: ["/kyotsu/", "Kyōtsu"], t: ["/track/", "Track"], o: ["/tools/", "Tools"] };
    function typing(n) { return !!n && !!n.closest && !!n.closest("input, textarea, select, [contenteditable]"); }
    function go(path) {
        document.querySelectorAll(".site-nav a").forEach(function (a) { if (a.pathname === path) location.href = a.href; });
    }
    function keySheet() {
        var old = document.getElementById("wKeys");
        if (old) { old.close(); old.remove(); return; }
        var d = document.createElement("dialog"), rows = [];
        d.className = "w-keys"; d.id = "wKeys"; d.setAttribute("aria-label", "Keyboard shortcuts");
        Object.keys(keys).forEach(function (k) { rows.push([k, keys[k].label]); });
        Object.keys(GO).forEach(function (k) { rows.push(["g " + k, GO[k][1]]); });
        rows.push(["?", "This list (Ctrl+/ always works)"]);
        rows.forEach(function (r) {
            var row = document.createElement("div"), kbd = document.createElement("kbd"), t = document.createElement("span");
            row.className = "w-keys-row"; kbd.textContent = r[0] === " " ? "Space" : r[0]; t.textContent = r[1];
            row.append(kbd, t); d.append(row);
        });
        // the off switch: letters, symbols, ? and g-sequences stop; Space, Enter and Ctrl+/ still work
        var lab = document.createElement("label"), box = document.createElement("input");
        lab.className = "w-keys-off"; box.type = "checkbox"; box.checked = charKeysOff;
        box.addEventListener("change", function () {
            charKeysOff = box.checked;
            try { localStorage.setItem("fz-keys", charKeysOff ? "off" : "on"); } catch (err) { /* ignore */ }
        });
        lab.append(box, document.createTextNode(" Turn off single-key shortcuts"));
        d.append(lab);
        d.addEventListener("close", function () { d.remove(); });
        d.addEventListener("click", function (e) { if (e.target === d) d.close(); });
        document.body.append(d);
        d.showModal();
    }
    document.addEventListener("keydown", function (e) {
        if (!active() || e.defaultPrevented || typing(e.target)) return;
        if (e.ctrlKey && !e.metaKey && !e.altKey && (e.key === "/" || e.key === "?")) { e.preventDefault(); keySheet(); return; }
        if (e.ctrlKey || e.metaKey || e.altKey) return;
        if (charKeysOff && charKey(e.key)) return;
        if (gPending) {
            clearTimeout(gPending); gPending = null;
            if (GO[e.key]) { e.preventDefault(); go(GO[e.key][0]); }
            return;
        }
        if (keys[e.key]) { e.preventDefault(); keys[e.key].fn(e); return; }
        if (e.key === "?") { e.preventDefault(); keySheet(); return; }
        if (e.key === "g") gPending = setTimeout(function () { gPending = null; }, 900);
    });

    // ---- tooltips: one element on <body>, so a scrolling pane never clips them ----
    var tip = null, tipFor = null, tipWait = null;
    function hideTip() { clearTimeout(tipWait); tipFor = null; if (tip) tip.hidden = true; }
    function showTip(n) {
        if (!tip) { tip = document.createElement("div"); tip.className = "w-tip"; tip.setAttribute("role", "tooltip"); document.body.append(tip); }
        tip.textContent = n.getAttribute("data-tip"); tip.hidden = false;
        var r = n.getBoundingClientRect(), w = tip.offsetWidth, h = tip.offsetHeight, vw = root.clientWidth;
        tip.style.left = Math.max(8, Math.min(r.left + r.width / 2 - w / 2, vw - w - 8)) + "px";
        tip.style.top = (r.bottom + 6 + h > window.innerHeight ? r.top - h - 6 : r.bottom + 6) + "px";
    }
    function tipTarget(e) { return active() && e.target.closest ? e.target.closest("[data-tip]") : null; }
    document.addEventListener("mouseover", function (e) {
        var n = tipTarget(e);
        if (n === tipFor) return;
        hideTip();
        if (n) { tipFor = n; tipWait = setTimeout(function () { showTip(n); }, 350); }
    });
    document.addEventListener("focusin", function (e) {
        var n = tipTarget(e);
        if (n && n.matches(":focus-visible")) { hideTip(); tipFor = n; showTip(n); }
    });
    document.addEventListener("focusout", hideTip);
    document.addEventListener("mousedown", hideTip);
    document.addEventListener("scroll", hideTip, { passive: true, capture: true });
    document.addEventListener("keydown", function (e) { if (e.key === "Escape") hideTip(); });

    window.FazonWide = {
        active: active,
        key: function (k, label, fn) { keys[k] = { label: label, fn: fn }; },
        tools: function () { return document.getElementById("wBarTools"); }
    };
})();
