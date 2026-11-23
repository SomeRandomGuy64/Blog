// Kansai pitch tool (layouts/tools/kansai-pitch.html): search box → GET /pitch/search, a word →
// GET /pitch/word?forms=1. All data and logic are in fazond (Fazon-Backend internal/pitch).
(function () {
  "use strict";
  var BASE = (window.Fazon && window.Fazon.base) || "https://api.fazon.nexus";
  var $ = function (id) { return document.getElementById(id); };
  var q = $("kpQ"), sugg = $("kpSugg"), regionSel = $("kpRegion"), status = $("kpStatus"), results = $("kpResults"),
    formsOut = $("kpForms"), controls = $("kpControls");
  var KIND = { godan: "五段 verb", ichidan: "一段 verb", kuru: "来る verb", suru: "する verb", adjective: "形容詞 adjective", other: "" };
  var REGION_NAME = { kyoto: "Kyoto", osaka: "Osaka", kinki: "Kinki", hyogo: "Hyōgo", kagawa: "Kagawa", kochi: "Kōchi", tokushima: "Tokushima" };
  var BASIS = { attested: "attested", rule: "Kyoto rule", guess: "best guess" };

  // wide (js/layout.js, css/wide-tools.css): the same markup, drawn denser, with the conjugation
  // toggles in the page bar and shortcuts on the ? sheet
  function wide() { return !!(window.FazonWide && FazonWide.active()); }

  function store(k, v) { try { if (v === undefined) return localStorage.getItem(k); localStorage.setItem(k, v); } catch (e) { return null; } }
  function esc(s) { return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]; }); }

  function get(path) {
    return fetch(BASE + path, { credentials: "omit" }).then(function (r) {
      return r.json().catch(function () { return {}; }).then(function (b) {
        if (!r.ok) { var e = new Error(b.error || ("HTTP " + r.status)); e.status = r.status; throw e; }
        return b;
      });
    });
  }

  // ---- drawing ----
  var SMALL = "ャュョァィゥェォヮゃゅょぁぃぅぇぉゎ";
  function morae(k) { var out = []; for (var ch of k) { if (SMALL.indexOf(ch) >= 0 && out.length) out[out.length - 1] += ch; else out.push(ch); } return out; }
  function hira(s) { return String(s).replace(/[ァ-ヶ]/g, function (c) { return String.fromCharCode(c.charCodeAt(0) - 96); }); }
  function svgFor(lv, n, step, drop) {
    if (!lv) return "";
    var w = Math.max(n * step, 8) + (drop ? step / 2 : 0);
    var pts = [].map.call(lv, function (c, i) { return [i * step + step / 2, c === "H" ? 3 : 14]; });
    if (drop) pts.push([n * step + step / 4, 14]);
    var line = pts.map(function (p) { return p.join(","); }).join(" ");
    var dots = pts.slice(0, n).map(function (p, i) {
      var hi = lv[i] === "H";
      return '<circle cx="' + p[0] + '" cy="' + p[1] + '" r="2.6" fill="' + (hi ? "var(--fz-accent)" : "var(--bg-color)") + '" stroke="' + (hi ? "var(--fz-accent)" : "var(--fz-muted)") + '" stroke-width="1.4"/>';
    }).join("");
    return '<svg viewBox="0 0 ' + w + ' 18" width="' + w + '" aria-hidden="true"><polyline points="' + line + '" fill="none" stroke="var(--fz-accent)" stroke-width="1.6" stroke-linejoin="round"' + (drop ? "" : "") + '/>' + dots + "</svg>";
  }
  function unitHTML(kana, lv, drop, small) {
    var ms = morae(kana), step = small ? 18 : 22;
    var label = lv ? kana + " " + lv + (drop ? "↓" : "") : kana;
    return '<span class="kp-unit" role="img" aria-label="' + esc(label) + '">' + svgFor(lv, ms.length, step, drop) + '<span class="kp-kana" lang="ja" aria-hidden="true">' +
      ms.map(function (m) { return "<span>" + esc(m) + "</span>"; }).join("") + "</span></span>";
  }

  // ---- one word ----
  var current = null; // {word, reading}
  var lastData = null;
  var shown = null;   // the word marked as picked

  function wordCard(w, i, many, on) {
    var p = w.pitch, h = "";
    h += '<div class="kp-head"><span class="kp-lemma" lang="ja">' + esc(w.word) + '</span><span class="kp-reading" lang="ja">' + esc(w.reading) + " · " + esc(w.romaji) + "</span></div>";
    h += '<div class="kp-main">';
    if (p) {
      h += '<span class="kp-pw">' + unitHTML(p.reading, p.levels, p.final_drop, false) + "</span>";
      h += '<span class="kp-hl">' + esc(p.pattern) + "</span>";
    } else {
      h += '<span class="kp-none">No Kansai pitch known for this word' + (w.tokyo_kernel == null ? ", and no Tokyo accent to guess from" : "") + ".</span>";
    }
    h += "</div>";
    var chips = [];
    if (KIND[w.kind]) chips.push('<span class="kp-chip">' + KIND[w.kind] + "</span>");
    if (w.register) chips.push('<span class="kp-chip">' + (w.register === "H" ? "high register 高起" : "low register 低起") + "</span>");
    if (p) {
      chips.push('<span class="kp-chip ' + p.basis + '">' + BASIS[p.basis] + "</span>");
      if (p.source_name) chips.push('<span class="kp-chip' + (p.fallback ? " fallback" : "") + '">' + esc(p.source_name) + "</span>");
      chips.push('<span class="kp-chip" title="The MvJ note type\'s Keihan notation">MvJ ' + esc(p.mvj) + "</span>");
    }
    if (w.tokyo_kernel != null) chips.push('<span class="kp-chip" title="Tokyo accent (Kanjium)">Tokyo ' + w.tokyo_kernel + "</span>");
    h += '<div class="kp-chips">' + chips.join("") + "</div>";
    if (p && p.note) h += '<p class="kp-note">' + esc(p.note) + "</p>";
    if (w.variants && w.variants.length) {
      var reg = regionSel.value;
      var open = wide() ? !many || on : w.variants.length > 1 && !many;
      h += '<details class="kp-vars"' + (open ? " open" : "") + "><summary>All recorded pitches (" + w.variants.length + ')</summary><table class="w-table"><thead><tr><th>Region</th><th>Pitch</th><th>Source</th></tr></thead><tbody>';
      w.variants.forEach(function (v) {
        h += '<tr class="' + (v.region === reg ? "cur" : "") + '"><td>' + esc(REGION_NAME[v.region] || v.region) + '</td><td><span class="kp-pw kp-small">' + unitHTML(v.reading, v.levels, v.final_drop, true) +
          '</span> <span class="kp-hl">' + esc(v.pattern) + "</span></td><td>" + esc(v.source_name) + "</td></tr>";
      });
      h += "</tbody></table></details>";
    }
    var cls = "kp-word" + (many && (w.forms || wide()) ? " pick" : "") + (on && many ? " on" : "");
    return '<article class="' + cls + '" data-i="' + i + '">' + h + "</article>";
  }

  function renderForms(w) {
    if (!w || !w.forms || !w.forms.length) { formsOut.innerHTML = ""; controls.hidden = true; return; }
    controls.hidden = false;
    var arch = $("kpArch").checked, letters = $("kpLetters").checked, small = wide(), h = "";
    w.forms.forEach(function (sec) {
      var rows = sec.forms.filter(function (f) { return arch || !f.archaic; });
      if (!rows.length) return;
      h += '<section class="kp-group"><h2>' + esc(sec.title) + '</h2><div class="kp-rows">';
      rows.forEach(function (f) {
        h += '<div class="kp-row"><div class="name" lang="ja">' + esc(f.name) + (f.archaic ? '<span class="arch">classical</span>' : "") + '</div><div class="forms">';
        f.variants.forEach(function (v) {
          h += '<div class="kp-variant"><span class="kp-pw' + (small ? " kp-small" : "") + '">' + v.units.map(function (u) { return unitHTML(u.kana, u.levels, false, small); }).join("") +
            '</span><span class="kp-rom">' + esc(v.romaji) + "</span>" +
            (letters ? '<span class="kp-hl">' + v.units.map(function (u) { return u.levels || "?"; }).join(" + ") + "</span>" : "") + "</div>";
        });
        h += "</div></div>";
      });
      h += "</div></section>";
    });
    formsOut.innerHTML = '<h2 class="kp-formsh">Kyoto conjugations of <span lang="ja">' + esc(w.word) + "</span></h2>" + h;
  }

  function render() {
    var list = lastData || [];
    if (!list.length) { results.innerHTML = ""; results.classList.remove("kp-many"); renderForms(null); return; }
    var many = list.length > 1;
    results.classList.toggle("kp-many", many);
    var cur = list.find(function (w) { return current && w.word === current.word && w.reading === current.reading; });
    var pick = cur && cur.forms ? cur : list.find(function (w) { return w.forms; });
    // wide: any word can be picked, and one always is; its notes, variants and forms are what show
    shown = wide() ? cur || pick || list[0] : cur;
    if (wide()) pick = shown;
    results.innerHTML = list.map(function (w, i) { return wordCard(w, i, many, w === shown); }).join("");
    renderForms(pick);
  }

  var seq = 0;
  function lookup(word, reading) {
    var my = ++seq;
    current = { word: word, reading: reading || "" };
    status.className = "kp-status"; status.textContent = "Looking up " + word + "…";
    var path = "/pitch/word?forms=1&region=" + encodeURIComponent(regionSel.value) + "&w=" + encodeURIComponent(word) + (reading ? "&r=" + encodeURIComponent(reading) : "");
    get(path).then(function (d) {
      if (my !== seq) return;
      lastData = d.results || [];
      status.textContent = lastData.length ? (lastData.length > 1 ? lastData.length + " words — pick one to see its conjugations." : "") : "Nothing found for " + word + ". Try its dictionary form, or pick a suggestion.";
      if (lastData.length === 1) current = { word: lastData[0].word, reading: lastData[0].reading };
      render();
      store("kp-word", JSON.stringify(current));
      var u = new URL(location.href); u.searchParams.set("w", word); if (reading) u.searchParams.set("r", reading); else u.searchParams.delete("r");
      history.replaceState(null, "", u);
    }).catch(function (e) {
      if (my !== seq) return;
      status.className = "kp-status bad";
      status.textContent = e.status === 429 ? e.message : e.status ? "Couldn't look that up: " + e.message : "The pitch server isn't reachable right now. Try again in a minute.";
    });
  }

  results.addEventListener("click", function (e) {
    var card = e.target.closest(".kp-word.pick");
    if (!card || e.target.closest("details")) return;
    var w = lastData[+card.dataset.i];
    current = { word: w.word, reading: w.reading };
    render();
    if (!wide()) formsOut.scrollIntoView({ behavior: "smooth", block: "start" });
  });

  // ---- search box ----
  var list = [], cur = -1, timer = 0, sseq = 0;
  function showSugg() {
    clearTimeout(timer);
    var v = q.value.trim();
    if (!v) { sugg.hidden = true; return; }
    timer = setTimeout(function () {
      var my = ++sseq;
      get("/pitch/search?limit=12&q=" + encodeURIComponent(v)).then(function (d) {
        if (my !== sseq || q.value.trim() !== v) return;
        list = d.results || []; cur = list.length ? 0 : -1;
        if (!list.length) { sugg.hidden = true; return; }
        sugg.innerHTML = list.map(function (h, i) {
          return '<button type="button" role="option" data-i="' + i + '" class="' + (i === cur ? "on" : "") + '"><span class="w" lang="ja">' + esc(h.word) + '</span><span class="r" lang="ja">' +
            esc(h.reading) + " · " + esc(h.romaji) + '</span><span class="k">' + (KIND[h.kind] || (h.attested ? "attested" : "")) + "</span></button>";
        }).join("");
        sugg.hidden = false;
      }).catch(function () { sugg.hidden = true; });
    }, 150);
  }
  function pick(h) { q.value = h.word; sugg.hidden = true; lookup(h.word, h.reading); }
  q.addEventListener("input", showSugg);
  q.addEventListener("keydown", function (e) {
    if (e.key === "Enter") {
      e.preventDefault();
      if (!sugg.hidden && list[cur]) pick(list[cur]);
      else if (q.value.trim()) { sugg.hidden = true; lookup(q.value.trim()); }
      return;
    }
    if (sugg.hidden) return;
    if (e.key === "ArrowDown") { cur = Math.min(cur + 1, list.length - 1); e.preventDefault(); }
    else if (e.key === "ArrowUp") { cur = Math.max(cur - 1, 0); e.preventDefault(); }
    else if (e.key === "Escape") { sugg.hidden = true; return; } else return;
    [].forEach.call(sugg.children, function (b, i) { b.classList.toggle("on", i === cur); });
  });
  sugg.addEventListener("click", function (e) { var b = e.target.closest("button"); if (b) pick(list[+b.dataset.i]); });
  document.addEventListener("click", function (e) { if (!e.target.closest(".kp-search")) sugg.hidden = true; });

  ["kpArch", "kpLetters"].forEach(function (id) { $(id).addEventListener("change", function () { render(); }); });
  regionSel.addEventListener("change", function () { store("kp-region", regionSel.value); if (current) lookup(current.word, current.reading); });

  ["書く", "参加", "東京", "食べる", "雨", "悲しい", "かく"].forEach(function (w) {
    var b = document.createElement("button"); b.type = "button"; b.lang = "ja"; b.textContent = w;
    b.onclick = function () { q.value = w; lookup(w); };
    $("kpExamples").appendChild(b);
  });

  // ---- wide ----
  var controlsHome = controls.parentNode;
  function placeControls() {
    var bar = wide() && FazonWide.tools();
    if (bar) bar.append(controls); else controlsHome.insertBefore(controls, formsOut);
  }
  // j / k: the next or previous word when a search matches several
  function stepWord(by) {
    var list = lastData || [];
    if (list.length < 2) return;
    var i = list.indexOf(shown);
    var w = list[i < 0 ? 0 : Math.min(list.length - 1, Math.max(0, i + by))];
    current = { word: w.word, reading: w.reading };
    render();
    var on = results.querySelector(".kp-word.on");
    if (on) on.scrollIntoView({ block: "nearest" });
  }
  function toggle(id) { var b = $(id); if (controls.hidden) return; b.checked = !b.checked; render(); }
  document.addEventListener("fz-layout", function () { placeControls(); render(); });
  placeControls();
  if (window.FazonWide) {
    FazonWide.key("/", "Search", function () { q.focus(); q.select(); });
    FazonWide.key("j", "Next word, when several match", function () { stepWord(1); });
    FazonWide.key("k", "Previous word", function () { stepWord(-1); });
    FazonWide.key("r", "Next region", function () {
      var n = regionSel.options.length; if (!n) return;
      regionSel.selectedIndex = (regionSel.selectedIndex + 1) % n;
      regionSel.dispatchEvent(new Event("change"));
    });
    FazonWide.key("c", "Show or hide classical forms", function () { toggle("kpArch"); });
    FazonWide.key("l", "Show or hide H/L letters", function () { toggle("kpLetters"); });
  }

  // regions from the server; the saved one if it still exists
  get("/pitch/regions").then(function (d) {
    regionSel.innerHTML = d.regions.map(function (r) { return '<option value="' + esc(r.id) + '" title="' + esc(r.note) + '">' + esc(r.name) + "</option>"; }).join("");
    var saved = store("kp-region");
    if (saved && d.regions.some(function (r) { return r.id === saved; })) regionSel.value = saved;
  }).catch(function () {}).then(start);

  function start() {
    var u = new URLSearchParams(location.search), w = u.get("w"), r = u.get("r") || "";
    if (!w) { try { var s = JSON.parse(store("kp-word") || "null"); if (s && s.word) { w = s.word; r = s.reading; } } catch (e) {} }
    w = w || "書く";
    q.value = w;
    lookup(w, r);
  }
})();
