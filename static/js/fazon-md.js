// Markdown for Track weekly summaries — and ONLY summaries (comments, bios, names stay plain text).
//   FazonMD.load()            → Promise<boolean>   loads the vendored marked + DOMPurify (once)
//   FazonMD.render(src)       → DocumentFragment   sanitized; plain text if the libraries aren't loaded
//   FazonMD.fill(el, src)     → Promise            plain text now, upgraded to Markdown once loaded
// Summaries are user-written and public, so the pipeline is: marked (GFM, breaks) with every raw
// HTML token escaped, headings demoted to h4–h6 and images turned into plain links → DOMPurify
// (private instance, strict allowlist, DOM fragment out) → link rewrite (http/https/mailto only,
// rel/target) → a final allowlist walk that falls back to plain text if anything unexpected is
// left. Nothing here assigns an untrusted string to innerHTML; callers append the fragment.
// Libraries: js/vendor/ (see js/vendor/README.md), loaded relative to this file on first use.
(function () {
  "use strict";
  if (window.FazonMD) return;
  var SRC = (document.currentScript && document.currentScript.src) || "/js/fazon-md.js";
  var LIBS = ["vendor/marked-18.0.6.umd.js", "vendor/purify-3.4.12.min.js"];

  // what Markdown may produce — and nothing else
  var TAGS = ["p", "br", "strong", "em", "del", "code", "pre", "blockquote", "ul", "ol", "li", "input",
    "h4", "h5", "h6", "hr", "table", "thead", "tbody", "tr", "th", "td", "a"];
  var ATTRS = ["href", "title", "align", "start", "type", "checked", "disabled"];
  var TAG_OK = {}, ATTR_OK = {
    a: { href: 1, title: 1, rel: 1, target: 1 }, th: { align: 1 }, td: { align: 1 }, ol: { start: 1 },
    input: { type: 1, checked: 1, disabled: 1 }
  };
  TAGS.forEach(function (t) { TAG_OK[t] = 1; });
  var REL = "nofollow noopener noreferrer ugc";
  var PURIFY_CFG = {
    ALLOWED_TAGS: TAGS,
    ALLOWED_ATTR: ATTRS,
    ALLOWED_URI_REGEXP: /^(?:https?|mailto):/i,   // DOMPurify tests every non-URI-safe attribute with this…
    ADD_URI_SAFE_ATTR: ["align", "start", "type"],  // …so mark the plain-value ones (left/3/checkbox) as safe
    ALLOW_DATA_ATTR: false,
    ALLOW_ARIA_ATTR: false,
    ALLOW_UNKNOWN_PROTOCOLS: false,
    ALLOW_SELF_CLOSE_IN_ATTR: false,
    SAFE_FOR_TEMPLATES: false,
    SANITIZE_DOM: true,
    SANITIZE_NAMED_PROPS: true,
    KEEP_CONTENT: true,
    FORBID_TAGS: ["script", "style", "svg", "math", "img", "picture", "source", "video", "audio", "iframe", "frame",
      "object", "embed", "form", "button", "select", "textarea", "template", "details", "summary", "link", "meta", "base"],
    FORBID_ATTR: ["style", "class", "id", "name", "src", "srcset", "action", "formaction", "xlink:href", "background", "poster"],
    RETURN_DOM_FRAGMENT: true
  };

  function esc(s) {
    return String(s).replace(/[&<>"']/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]; });
  }
  // absolute http(s)/mailto URL (as the browser would read it), or null; httpsOnly for images
  function safeUrl(u, httpsOnly) {
    if (typeof u !== "string") return null;
    var p;
    try { p = new URL(u); } catch (e) { return null; }   // no base: relative links are dropped
    if (httpsOnly ? p.protocol !== "https:" : !/^(https?|mailto):$/.test(p.protocol)) return null;
    return p.href;
  }

  var md = null, purify = null;
  function setup() {
    if (md) return true;
    var M = window.marked, D = window.DOMPurify;
    if (!M || typeof M.Marked !== "function" || typeof D !== "function" || !D.isSupported) return false;
    var p = D(window);                         // private instance: hooks don't leak to other users
    p.addHook("afterSanitizeAttributes", function (n) {
      var t = n.nodeName.toLowerCase();
      if (t === "a") {
        var h = n.getAttribute("href"), ok = h == null ? null : safeUrl(h, false);
        if (ok) { n.setAttribute("href", ok); n.setAttribute("rel", REL); n.setAttribute("target", "_blank"); }
        else { n.removeAttribute("href"); n.removeAttribute("rel"); n.removeAttribute("target"); }
      } else if (t === "input") {              // task-list boxes only: always a disabled checkbox
        n.setAttribute("type", "checkbox");
        n.setAttribute("disabled", "");
      }
    });
    var m = new M.Marked({ gfm: true, breaks: true, async: false });
    m.use({
      renderer: {
        // no raw HTML from the author: shown as text
        html: function (tok) {
          var t = esc(tok.text != null ? tok.text : tok.raw || "");
          return tok.block ? "<p>" + t.replace(/\n+$/, "").replace(/\n/g, "<br>") + "</p>\n" : t;
        },
        // # → h4 … ### and deeper → h6 (styled small in .fz-md): no giant page headings
        heading: function (tok) {
          var d = Math.min(6, (tok.depth | 0) + 3);
          return "<h" + d + ">" + this.parser.parseInline(tok.tokens) + "</h" + d + ">\n";
        },
        // images are never loaded (no tracking pixels / IP leaks): a plain https link instead
        image: function (tok) {
          var alt = esc(tok.text || "image"), u = safeUrl(tok.href, true);
          return u ? '<a href="' + esc(u) + '">' + alt + "</a>" : alt;
        }
      }
    });
    md = m; purify = p;
    return true;
  }

  // final check on the sanitized DOM: only allowlisted tags/attributes, links already vetted
  function clean(frag) {
    var bad = false;
    Array.prototype.slice.call(frag.querySelectorAll("a:not([href])")).forEach(function (a) {
      while (a.firstChild) a.parentNode.insertBefore(a.firstChild, a);
      a.parentNode.removeChild(a);
    });
    Array.prototype.forEach.call(frag.querySelectorAll("*"), function (n) {
      var t = n.nodeName.toLowerCase(), ok = ATTR_OK[t] || {};
      if (!TAG_OK[t] || n.namespaceURI !== "http://www.w3.org/1999/xhtml") { bad = true; return; }
      Array.prototype.forEach.call(n.attributes, function (a) { if (!ok[a.name]) bad = true; });
      if (t === "a" && (!safeUrl(n.getAttribute("href"), false) || n.getAttribute("rel") !== REL)) bad = true;
      if (t === "input" && (n.getAttribute("type") !== "checkbox" || !n.hasAttribute("disabled"))) bad = true;
    });
    return !bad;
  }

  function plain(src) {
    var f = document.createDocumentFragment(), d = document.createElement("div");
    d.className = "fz-md-plain";
    d.textContent = src;
    f.appendChild(d);
    return f;
  }

  function render(src) {
    src = src == null ? "" : String(src);
    if (!setup()) return plain(src);
    try {
      var frag = purify.sanitize(md.parse(src), PURIFY_CFG);
      if (frag && frag.nodeType === 11 && clean(frag)) return frag;
    } catch (e) { /* fall through */ }
    return plain(src);
  }

  var loadP = null;
  function loadScript(rel) {
    return new Promise(function (res) {
      var href = new URL(rel, SRC).href, s = document.querySelector('script[src="' + href + '"]');
      if (s && s.getAttribute("data-loaded")) return res(true);
      if (!s) { s = document.createElement("script"); s.src = href; s.async = false; document.head.appendChild(s); }
      s.addEventListener("load", function () { s.setAttribute("data-loaded", "1"); res(true); });
      s.addEventListener("error", function () { res(false); });
    });
  }
  function load() {
    if (setup()) return Promise.resolve(true);
    if (!loadP) loadP = Promise.all(LIBS.map(loadScript)).then(function () { return setup(); });
    return loadP;
  }

  // fill(el, src): el gets class fz-md and the rendered summary (plain text until/unless loaded)
  var pending = typeof WeakMap === "function" ? new WeakMap() : null;
  function fill(node, src) {
    node.classList.add("fz-md");
    var put = function () { node.textContent = ""; node.appendChild(render(src)); };
    put();
    if (md) return Promise.resolve(true);
    var tok = {};
    if (pending) pending.set(node, tok);
    return load().then(function (ok) {
      if (ok && (!pending || pending.get(node) === tok)) put();   // a later fill() on the same node wins
      return ok;
    });
  }

  window.FazonMD = { load: load, render: render, fill: fill, ready: function () { return !!md; } };
})();
