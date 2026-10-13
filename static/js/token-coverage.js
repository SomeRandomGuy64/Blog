// Token Coverage: reads the user's learned words out of Anki over AnkiConnect (mirroring their
// AnkiMorphs note filters), tokenises a subtitle file with kuromoji (IPADIC — the same dictionary
// family as the AnkiMorphs Japanese mecab, so lemmas line up), and reports how much of the
// media's content vocabulary is known: overall, sentence-level i+N, a rolling coverage-over-time
// chart, and — given a YouTube link — a live readout synced to the playing video, plus primed
// listening (auto-pause before each line; see primed-listening.js) driven by the same subtitles.
//
// Content-word rules follow the coverage recalc: particles, auxiliaries, conjunctions, symbols,
// interjections, fillers, numerals, single-hiragana fragments and words the dictionary doesn't
// know are never counted; proper nouns and transparent katakana loanwords are optional.
// Everything stays in the browser; the only network calls are AnkiConnect (localhost), the
// kuromoji dictionary (jsDelivr) and the YouTube player.
(function () {
  "use strict";

  var DIC_PATH = "https://cdn.jsdelivr.net/npm/kuromoji@0.1.2/dict/";
  var STORE = "tokcov-settings-v1";
  var $ = function (id) { return document.getElementById(id); };

  // katakana kept as content vocab — wasei-eigo / semantic shift / native words (from the recalc)
  var KEEP_KATA = new Set((
    "マジ ダメ バカ ネタ タコ クソ オス メス イライラ ギリギリ ノリ ネジ ケチ カエル " +
    "ドキドキ ガキ バネ ウマ ゴミ ボケ パチ ヒマ アホ クズ ネズミ エビ イカ サバ カラス " +
    "ネギ ハゲ カニ トラ シミ シワ ツボ バツ マル トロ ババ カバン イス キツネ " +
    "コツコツ ガンガン バラバラ バタバタ ゴロゴロ ワクワク ノリノリ パンパン ボロボロ " +
    "ガチャガチャ ダラダラ ハラハラ キラキラ パン ズボン タバコ バイト ギャル " +
    "マンション テンション ハイテンション パソコン キャラ スタメン コンビニ ピアス アンチ " +
    "メンタル グッズ サイン レポ コメ グロ ポジ ゴールデンウィーク クレーム スペック " +
    "リストラ カンニング ドタキャン ワンチャン").split(/\s+/));
  var POS_FUNC = new Set(["助詞", "助動詞", "記号", "感動詞", "フィラー", "接続詞", "その他"]);
  var KATA = /^[゠-ヿ]+$/, DIGIT = /[0-9０-９]/, HIRA1 = /^[ぁ-ゟ]$/;
  var BRACKET = /[\[\(（【][^\]\)）】]*[\]\)）】]/g;

  // ---------------------------------------------------------------- settings
  var DEFAULTS = { url: "http://127.0.0.1:8765", filters: null, mature: 21, knownIvl: 21, stability: false,
                   manualTag: "am-known-manually",
                   csvLemmas: [], exNames: true, exLoan: true, win: 180, ppc: 0.06 };
  var S = (function () {
    var o = {};
    try { o = JSON.parse(localStorage.getItem(STORE) || "{}") || {}; } catch (e) { o = {}; }
    for (var k in DEFAULTS) if (!(k in o)) o[k] = DEFAULTS[k];
    return o;
  })();
  function save() { try { localStorage.setItem(STORE, JSON.stringify(S)); } catch (e) { /* private mode */ } }

  // ---------------------------------------------------------------- helpers
  function status(el, msg, kind) {
    el.className = "tc-status" + (kind ? " tc-" + kind : "");
    el.textContent = msg;
  }
  function el(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text != null) n.textContent = text;
    return n;
  }
  function pct(v) { return isFinite(v) ? v.toFixed(1) + "%" : "—"; }
  function fmtN(n) { return n.toLocaleString("en"); }
  function fmtT(sec) {
    sec = Math.max(0, Math.floor(sec));
    var h = Math.floor(sec / 3600), m = Math.floor(sec / 60) % 60, s = sec % 60;
    return (h ? h + ":" + String(m).padStart(2, "0") : m) + ":" + String(s).padStart(2, "0");
  }
  function hira(s) { return (s || "").replace(/[ァ-ヶ]/g, function (c) { return String.fromCharCode(c.charCodeAt(0) - 0x60); }); }
  function tick() { return new Promise(function (r) { setTimeout(r, 0); }); }
  function q(s) { return '"' + String(s).replace(/\\/g, "\\\\").replace(/"/g, '\\"') + '"'; }

  // ---------------------------------------------------------------- tokenizer
  var tokP = null;
  function tokenizer() {
    if (tokP) return tokP;
    tokP = new Promise(function (res, rej) {
      if (!window.kuromoji) { rej(new Error("couldn't load the Japanese tokenizer (cdn.jsdelivr.net blocked?)")); return; }
      // kuromoji path.join()s the dictionary URL, which collapses "https://" to "https:/" —
      // build with a placeholder and rewrite its XHR requests back to the real CDN URL.
      var XO = XMLHttpRequest.prototype.open;
      XMLHttpRequest.prototype.open = function (m, u) {
        if (typeof u === "string" && u.indexOf("tokcov-dict/") === 0) arguments[1] = DIC_PATH + u.slice(12);
        return XO.apply(this, arguments);
      };
      window.kuromoji.builder({ dicPath: "tokcov-dict/" }).build(function (err, t) {
        XMLHttpRequest.prototype.open = XO;
        if (err) { tokP = null; rej(new Error("couldn't load the Japanese dictionary: " + (err.message || err))); }
        else res(t);
      });
    });
    return tokP;
  }

  // one subtitle token → {s surface, l lemma, r reading, f flags}; f: 1 never counted,
  // 2 proper noun, 4 transparent katakana loanword
  function classify(t) {
    var lem = t.basic_form && t.basic_form !== "*" ? t.basic_form : t.surface_form, f = 0;
    if (t.word_type !== "KNOWN" || POS_FUNC.has(t.pos) || t.pos_detail_1 === "数" ||
        DIGIT.test(lem) || DIGIT.test(t.surface_form) || HIRA1.test(lem) || !lem.trim()) f |= 1;
    if (t.pos_detail_1 === "固有名詞") f |= 2;
    if (KATA.test(lem) && !KEEP_KATA.has(lem)) f |= 4;
    return { s: t.surface_form, l: lem, r: t.reading && t.reading !== "*" ? hira(t.reading) : "", f: f };
  }
  function lemmaOf(t) { return t.basic_form && t.basic_form !== "*" ? t.basic_form : t.surface_form; }

  // ---------------------------------------------------------------- AnkiConnect
  function ac(action, params) {
    var body = { action: action, version: 6, params: params || {} };
    var key = $("tcKey").value.trim();
    if (key) body.key = key;
    return fetch(S.url, { method: "POST", body: JSON.stringify(body) })
      .then(function (r) { return r.json(); }, function () {
        throw new Error("couldn't reach AnkiConnect at " + S.url + ". Is Anki open with AnkiConnect installed? " +
          "If your browser asked about local network access, allow it.");
      })
      .then(function (j) { if (j.error) throw new Error(j.error); return j.result; });
  }
  function multi(actions) {
    return ac("multi", { actions: actions }).then(function (rs) {
      return rs.map(function (r) {
        if (r && typeof r === "object" && "error" in r) { if (r.error) throw new Error(r.error); return r.result; }
        return r;
      });
    });
  }

  var models = [];   // [{name, fields, reviewed}]

  function connect() {
    S.url = $("tcUrl").value.trim() || DEFAULTS.url; save();
    var st = $("tcConnStatus");
    status(st, "Connecting… (check Anki for a permission pop-up)");
    $("tcConnect").disabled = true;
    tokenizer().catch(function () {});   // start the dictionary download in parallel
    ac("requestPermission").then(function (p) {
      if (p.permission !== "granted") throw new Error("Anki denied access. Press Connect again and choose Yes in Anki's pop-up.");
      if (p.requireApikey && !$("tcKey").value.trim()) throw new Error("your AnkiConnect requires an API key — enter it above.");
      return ac("modelNames");
    }).then(function (names) {
      return multi(names.map(function (n) { return { action: "findNotes", params: { query: "note:" + q(n) + " -is:new" } }; }))
        .then(function (found) {
          var counts = found.map(function (ids) { return ids.length; });
          return multi(names.map(function (n) { return { action: "modelFieldNames", params: { modelName: n } }; }))
            .then(function (fields) {
              models = names.map(function (n, i) { return { name: n, fields: fields[i], reviewed: counts[i] }; })
                .sort(function (a, b) { return b.reviewed - a.reviewed || a.name.localeCompare(b.name); });
            });
        });
    }).then(function () {
      var total = models.reduce(function (a, m) { return a + m.reviewed; }, 0);
      status(st, "Connected — " + fmtN(models.length) + " note types, " + fmtN(total) + " notes you've reviewed.", "ok");
      $("tcConnect").textContent = "Reconnect";
      if (!S.filters || !S.filters.length || !S.filters.some(function (f) { return modelByName(f.model); })) S.filters = guessFilters();
      renderFilters();
      $("tcStepKnown").hidden = false;
    }).catch(function (e) {
      status(st, "✗ " + e.message, "err");
    }).then(function () { $("tcConnect").disabled = false; });
  }

  function modelByName(n) { for (var i = 0; i < models.length; i++) if (models[i].name === n) return models[i]; return null; }
  var FIELD_PREF = [/^sentence$/i, /^expression$/i, /sentence/i, /^(japanese|target|front|word|vocab)/i];
  function bestField(fields) {
    for (var p = 0; p < FIELD_PREF.length; p++)
      for (var i = 0; i < fields.length; i++) if (FIELD_PREF[p].test(fields[i])) return fields[i];
    return fields[0] || "";
  }
  function guessFilters() {
    var out = models.filter(function (m) { return m.reviewed > 0; }).slice(0, 3)
      .map(function (m) { return { model: m.name, field: bestField(m.fields), inc: "", exc: "" }; });
    return out.length ? out : [{ model: models[0] ? models[0].name : "", field: models[0] ? bestField(models[0].fields) : "", inc: "", exc: "" }];
  }

  function renderFilters() {
    var box = $("tcFilters"); box.textContent = "";
    S.filters.forEach(function (f, idx) {
      var row = el("div", "tc-filter");
      var lm = el("label", "tc-field", "Note type"), sm = el("select");
      models.forEach(function (m) {
        var o = el("option", null, m.name + "  (" + fmtN(m.reviewed) + " reviewed)"); o.value = m.name;
        if (m.name === f.model) o.selected = true;
        sm.appendChild(o);
      });
      if (!modelByName(f.model) && models[0]) { f.model = models[0].name; f.field = bestField(models[0].fields); }
      lm.appendChild(sm);
      var lf = el("label", "tc-field", "Field"), sf = el("select");
      function fillFields() {
        sf.textContent = "";
        var m = modelByName(f.model), fs = m ? m.fields : [];
        if (fs.indexOf(f.field) < 0) f.field = bestField(fs);
        fs.forEach(function (n) { var o = el("option", null, n); o.value = n; o.selected = n === f.field; sf.appendChild(o); });
      }
      fillFields(); lf.appendChild(sf);
      var li = el("label", "tc-field"), ii = el("input"), lt = el("span", null, "Only with tags ");
      lt.appendChild(el("span", "tc-opt", "(all of, space-separated)")); li.appendChild(lt);
      ii.type = "text"; ii.value = f.inc || ""; ii.placeholder = "any"; li.appendChild(ii);
      var le = el("label", "tc-field", "Skip tags"), ie = el("input"); ie.type = "text"; ie.value = f.exc || "";
      ie.placeholder = "none"; le.appendChild(ie);
      var x = el("button", "tc-x", "×"); x.type = "button"; x.title = "Remove this filter"; x.setAttribute("aria-label", "Remove this filter");
      sm.onchange = function () { f.model = sm.value; fillFields(); save(); };
      sf.onchange = function () { f.field = sf.value; save(); };
      ii.oninput = function () { f.inc = ii.value; save(); };
      ie.oninput = function () { f.exc = ie.value; save(); };
      x.onclick = function () { S.filters.splice(idx, 1); save(); renderFilters(); };
      row.appendChild(lm); row.appendChild(lf); row.appendChild(li); row.appendChild(le);
      if (S.filters.length > 1) row.appendChild(x);
      box.appendChild(row);
    });
    save();
  }

  // ---------------------------------------------------------------- known words
  var known = null;   // {learned:Set, mature:Set}

  function cleanField(html) {
    return String(html || "")
      .replace(/\[sound:[^\]]*\]/g, " ").replace(/<br\s*\/?>/gi, " ").replace(/<[^>]+>/g, "")
      .replace(/&nbsp;/g, " ").replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&quot;/g, '"').replace(/&amp;/g, "&")
      .replace(/\{\{c\d+::(.*?)(::[^}]*)?\}\}/g, "$1").replace(/\[[^\]]*\]/g, "");
  }
  // AnkiMorphs note filters: a note needs ALL the include tags and NONE of the exclude tags
  function tagClause(words, neg) {
    return (words || "").trim().split(/\s+/).filter(Boolean)
      .map(function (w) { return (neg ? " -tag:" : " tag:") + q(w); }).join("");
  }

  function loadKnown() {
    var st = $("tcKnownStatus"), btn = $("tcLoadKnown");
    var M = Math.max(1, parseInt($("tcMature").value, 10) || 21);
    var KI = Math.max(1, parseInt($("tcKnownIvl").value, 10) || 21);
    S.mature = M; S.knownIvl = KI; S.stability = $("tcStability").checked;
    S.manualTag = $("tcManualTag").value.trim(); save();
    // Card "memory strength" as AnkiMorphs computes it: new → 0, learning → 1, review → interval
    // (or FSRS stability), and a card with the known-manually tag → the "interval for known morphs"
    // setting, whatever its real interval. AnkiMorphs gives its known-AUTOMATICALLY tag the same
    // treatment, but that tag only lands on cards whose words are already known (it's circular), and
    // on a big mining collection it covers hundreds of thousands of notes — so it's left out here.
    var tags = S.manualTag ? "tag:" + q(S.manualTag) : "";
    var strong = (S.stability ? "prop:s>=" : "prop:ivl>=") + M;
    var learnedQ = " (-is:new" + (tags ? " OR " + tags : "") + ")";
    var matureQ = tags ? " ((" + strong + " -(" + tags + "))" + (KI >= M ? " OR " + tags : "") + ")" : " " + strong;
    var filters = S.filters.filter(function (f) { return f.model && f.field; });
    if (!filters.length) { status(st, "✗ add at least one note filter.", "err"); return; }
    btn.disabled = true;
    status(st, "Searching your collection…");
    var acts = [];
    filters.forEach(function (f) {
      var base = "note:" + q(f.model) + tagClause(f.inc) + tagClause(f.exc, true);
      acts.push({ action: "findNotes", params: { query: base + learnedQ } });
      acts.push({ action: "findNotes", params: { query: base + matureQ } });
    });
    var learned = new Set(), mature = new Set(), nNotes = 0;
    multi(acts).then(function (res) {
      var fieldsOf = new Map(), matureIds = new Set();   // noteId → Set(fields)
      filters.forEach(function (f, i) {
        res[2 * i].forEach(function (id) { if (!fieldsOf.has(id)) fieldsOf.set(id, new Set()); fieldsOf.get(id).add(f.field); });
        res[2 * i + 1].forEach(function (id) { matureIds.add(id); });
      });
      var ids = Array.from(fieldsOf.keys()); nNotes = ids.length;
      if (!ids.length) throw new Error("no reviewed notes matched those filters.");
      return tokenizer().then(function (tk) {
        var CH = 400, i = 0;
        function next() {
          if (i >= ids.length) return Promise.resolve();
          var batch = ids.slice(i, i + CH);
          status(st, "Reading notes " + fmtN(i + 1) + "–" + fmtN(Math.min(i + CH, ids.length)) + " of " + fmtN(ids.length) + "…");
          return ac("notesInfo", { notes: batch }).then(function (infos) {
            infos.forEach(function (n) {
              if (!n || !n.fields) return;
              var isM = matureIds.has(n.noteId);
              fieldsOf.get(n.noteId).forEach(function (fname) {
                var fv = n.fields[fname]; if (!fv) return;
                tk.tokenize(cleanField(fv.value)).forEach(function (t) {
                  var l = lemmaOf(t); learned.add(l); if (isM) mature.add(l);
                });
              });
            });
            i += CH;
            return tick().then(next);
          });
        }
        status(st, "Loading the Japanese dictionary (≈17 MB, first time only)…");
        return next();
      });
    }).then(function () {
      var csv = S.csvLemmas || [];
      csv.forEach(function (l) { learned.add(l); if (KI >= M) mature.add(l); });
      known = { learned: learned, mature: mature };
      status(st, "✓ " + fmtN(learned.size) + " learned words (" + fmtN(mature.size) + " mature) from " + fmtN(nNotes) + " notes" +
        (csv.length ? " + " + fmtN(csv.length) + " from CSV" : "") + ".", "ok");
      $("tcStepMedia").hidden = false;
      updateAnalyzeBtn();
      if (media) reclassify();
    }).catch(function (e) { status(st, "✗ " + e.message, "err"); })
      .then(function () { btn.disabled = false; });
  }

  function readCsv(files) {
    var all = new Set(S.csvLemmas || []);
    Promise.all(Array.from(files).map(function (f) { return f.text(); })).then(function (texts) {
      texts.forEach(function (txt) {
        var rows = txt.replace(/^﻿/, "").split(/\r?\n/).filter(function (r) { return r.trim(); });
        if (!rows.length) return;
        var head = rows[0].split(","), col = 0, start = 0;
        var hi = head.findIndex(function (h) { return /lemma/i.test(h); });
        if (hi >= 0) { col = hi; start = 1; } else if (/morph|inflection/i.test(rows[0])) start = 1;
        for (var i = start; i < rows.length; i++) {
          var v = (rows[i].split(",")[col] || "").replace(/^"|"$/g, "").trim();
          if (v) all.add(v);
        }
      });
      S.csvLemmas = Array.from(all); save(); showCsv();
      if (known) {
        var toMature = (S.knownIvl || 21) >= (S.mature || 21);
        S.csvLemmas.forEach(function (l) { known.learned.add(l); if (toMature) known.mature.add(l); });
        if (media) reclassify();
      }
    });
  }
  function showCsv() {
    var n = (S.csvLemmas || []).length;
    $("tcCsvStatus").textContent = n ? fmtN(n) + " extra known words from CSV (remembered on this device; like AnkiMorphs, they get the interval for known morphs)." : "";
    $("tcClearCsv").hidden = !n;
  }

  // ---------------------------------------------------------------- subtitles
  function parseTime(s) {
    var m = /(?:(\d+):)?(\d{1,2}):(\d{2})(?:[,.](\d{1,3}))?/.exec(s);
    if (!m) return NaN;
    return (+(m[1] || 0)) * 3600 + (+m[2]) * 60 + (+m[3]) + (m[4] ? +(m[4] + "00").slice(0, 3) / 1000 : 0);
  }
  function parseSrt(txt) {
    var out = [];
    txt.replace(/\r/g, "").split(/\n\s*\n/).forEach(function (block) {
      var lines = block.split("\n"), ti = lines.findIndex(function (l) { return l.indexOf("-->") >= 0; });
      if (ti < 0) return;
      var p = lines[ti].split("-->"), s = parseTime(p[0]), e = parseTime(p[1]);
      var text = lines.slice(ti + 1).join(" ").replace(/<[^>]+>/g, "").replace(/\{\\[^}]*\}/g, "");
      if (isFinite(s)) out.push({ s: s, e: isFinite(e) ? e : s + 2, text: text });
    });
    return out;
  }
  var TEXT_KEYS = ["text", "content", "utf8", "word", "line", "transcript", "sentence"];
  var START_KEYS = ["start", "startTime", "start_time", "from", "begin", "tStartMs", "start_ms", "startMs", "offset"];
  var END_KEYS = ["end", "endTime", "end_time", "to", "stop", "end_ms", "endMs"];
  function pick(o, keys) { for (var i = 0; i < keys.length; i++) if (o[keys[i]] != null) return [keys[i], o[keys[i]]]; return null; }
  function asSec(kv) {
    if (!kv) return NaN;
    var k = kv[0], v = kv[1];
    if (typeof v === "string") return /:/.test(v) ? parseTime(v) : parseFloat(v);
    return /ms$/i.test(k) ? v / 1000 : v;
  }
  function findCueArray(o, depth) {
    if (Array.isArray(o)) {
      var objs = o.filter(function (x) { return x && typeof x === "object" && !Array.isArray(x); });
      if (objs.length && objs.some(function (x) { return pick(x, TEXT_KEYS) && pick(x, START_KEYS); })) return o;
      return null;
    }
    if (!o || typeof o !== "object" || depth > 3) return null;
    var best = null, bestKey = "";
    Object.keys(o).forEach(function (k) {
      var a = findCueArray(o[k], depth + 1);
      if (a && (!best || a.length > best.length)) { best = a; bestKey = k; }
    });
    if (best) best._key = best._key || bestKey;
    return best;
  }
  function parseJson(txt) {
    var o = JSON.parse(txt);
    if (o && Array.isArray(o.events)) {   // YouTube json3
      return o.events.filter(function (ev) { return ev.segs; }).map(function (ev) {
        var s = (ev.tStartMs || 0) / 1000;
        return { s: s, e: s + (ev.dDurationMs || 2000) / 1000, text: ev.segs.map(function (g) { return g.utf8 || ""; }).join("") };
      }).filter(function (c) { return c.text.trim(); });
    }
    var arr = findCueArray(o, 0);
    if (!arr) throw new Error("couldn't find timed subtitle lines in that JSON.");
    var items = arr.filter(function (x) { return x && typeof x === "object" && (x.type == null || x.type === "word"); })
      .map(function (x) {
        var t = pick(x, TEXT_KEYS);
        return { s: asSec(pick(x, START_KEYS)), e: asSec(pick(x, END_KEYS)), text: t ? String(t[1]) : "" };
      }).filter(function (c) { return isFinite(c.s); });
    // integer starts past 10 h with no unit in the key: almost certainly milliseconds
    var maxS = items.reduce(function (m, c) { return Math.max(m, c.s); }, 0);
    if (maxS > 36000 && items.every(function (c) { return Number.isInteger(c.s); }))
      items.forEach(function (c) { c.s /= 1000; c.e /= 1000; });
    items.forEach(function (c) { if (!isFinite(c.e)) c.e = c.s + 0.3; });
    var avgLen = items.reduce(function (a, c) { return a + c.text.trim().length; }, 0) / Math.max(1, items.length);
    if (!/^(words|tokens)$/i.test(arr._key || "") && avgLen > 3) return items;
    // word-level transcript (Scribe / Soniox style) → group into lines
    var out = [], cur = null;
    items.forEach(function (w) {
      if (cur && (w.s - cur.e > 0.8 || cur.text.length >= 40)) { out.push(cur); cur = null; }
      if (!cur) cur = { s: w.s, e: w.e, text: "" };
      cur.text += w.text; cur.e = Math.max(cur.e, w.e);
      if (/[。？！?!…]\s*$/.test(w.text)) { out.push(cur); cur = null; }
    });
    if (cur) out.push(cur);
    return out;
  }

  // ---------------------------------------------------------------- analysis
  var media = null;   // {cues:[{s,e,text,toks}], dur}
  var calc = null;    // derived per-cue arrays + prefix sums against the current known set

  function updateAnalyzeBtn() { $("tcAnalyze").disabled = !(known && $("tcSubs").files.length); }

  function analyze() {
    var st = $("tcMediaStatus"), file = $("tcSubs").files[0];
    if (!file || !known) return;
    $("tcAnalyze").disabled = true;
    status(st, "Reading subtitles…");
    file.text().then(function (txt) {
      var cues = /\.json$/i.test(file.name) || /^\s*[\[{]/.test(txt) ? parseJson(txt) : parseSrt(txt);
      cues = cues.map(function (c) { return { s: c.s, e: Math.max(c.e, c.s), text: c.text.replace(BRACKET, "").replace(/　/g, " ").trim() }; })
        .filter(function (c) { return c.text; }).sort(function (a, b) { return a.s - b.s; });
      if (!cues.length) throw new Error("no subtitle lines found in " + file.name + ".");
      return tokenizer().then(function (tk) {
        var i = 0;
        function next() {
          if (i >= cues.length) return Promise.resolve();
          status(st, "Tokenising line " + fmtN(i + 1) + " of " + fmtN(cues.length) + "…");
          var end = Math.min(i + 500, cues.length);
          for (; i < end; i++) cues[i].toks = tk.tokenize(cues[i].text).map(classify);
          return tick().then(next);
        }
        return next();
      }).then(function () {
        media = { cues: cues, dur: cues.reduce(function (m, c) { return Math.max(m, c.e); }, 0), name: file.name };
        status(st, "✓ " + fmtN(cues.length) + " lines from " + file.name + ".", "ok");
        reclassify();
        if (prim) prim.setCues(cues);   // after reclassify: resetting the offset refreshes the live readout
        setupVideo();
        $("tcResults").hidden = false;
        $("tcResults").scrollIntoView({ behavior: "smooth", block: "start" });
      });
    }).catch(function (e) { status(st, "✗ " + e.message, "err"); })
      .then(updateAnalyzeBtn);
  }

  function isContent(tk) {
    return !(tk.f & 1) && !(S.exNames && (tk.f & 2)) && !(S.exLoan && (tk.f & 4));
  }

  function reclassify() {
    if (!media || !known) return;
    var L = known.learned, Mt = known.mature, cues = media.cues, n = cues.length;
    var t = new Float64Array(n), cn = new Float64Array(n + 1), cl = new Float64Array(n + 1), cm = new Float64Array(n + 1);
    var inn = { l: [0, 0, 0], m: [0, 0, 0] }, unk = new Map();
    cues.forEach(function (c, i) {
      var tot = 0, kl = 0, km = 0, ul = new Set(), um = new Set();
      c.toks.forEach(function (tk) {
        if (!isContent(tk)) { tk.st = "ign"; return; }
        tot++;
        if (Mt.has(tk.l)) { tk.st = "mat"; kl++; km++; }
        else if (L.has(tk.l)) { tk.st = "young"; kl++; um.add(tk.l); }
        else {
          tk.st = "unk"; ul.add(tk.l); um.add(tk.l);
          var u = unk.get(tk.l);
          if (!u) unk.set(tk.l, u = { l: tk.l, r: tk.r, n: 0, first: c.s });
          u.n++;
        }
      });
      t[i] = c.s; cn[i + 1] = cn[i] + tot; cl[i + 1] = cl[i] + kl; cm[i + 1] = cm[i] + km;
      if (tot) { inn.l[Math.min(2, ul.size)]++; inn.m[Math.min(2, um.size)]++; }
    });
    calc = { t: t, cn: cn, cl: cl, cm: cm, inn: inn,
             unk: Array.from(unk.values()).sort(function (a, b) { return b.n - a.n || a.first - b.first; }) };
    renderTiles(); renderUnknown(); drawChart(); if (player) updateLive(true);
  }

  // first cue index with start >= x
  function lowerBound(x) {
    var lo = 0, hi = calc.t.length;
    while (lo < hi) { var mid = (lo + hi) >> 1; if (calc.t[mid] < x) lo = mid + 1; else hi = mid; }
    return lo;
  }
  function windowCov(a, b) {
    var i = lowerBound(a), j = lowerBound(b), tot = calc.cn[j] - calc.cn[i];
    return { tot: tot, l: tot ? 100 * (calc.cl[j] - calc.cl[i]) / tot : NaN, m: tot ? 100 * (calc.cm[j] - calc.cm[i]) / tot : NaN };
  }

  // ---------------------------------------------------------------- results: tiles + table
  function tile(label, big, subNodes) {
    var d = el("div", "tc-tile");
    var lb = el("div", "tc-tile-lbl"), m = /^(i\+\d)(.*)$/.exec(label);
    if (m) { lb.appendChild(el("span", "tc-nocase", m[1])); lb.appendChild(document.createTextNode(m[2])); }   // keep "i+0" lowercase
    else lb.textContent = label;
    d.appendChild(lb);
    d.appendChild(el("div", "tc-tile-big", big));
    (subNodes || []).forEach(function (n) { d.appendChild(n); });
    return d;
  }
  function sub(html) { var p = el("div", "tc-tile-sub"); p.innerHTML = html; return p; }

  function renderTiles() {
    var box = $("tcTiles"); box.textContent = "";
    var n = calc.t.length, T = calc.cn[n], KL = calc.cl[n], KM = calc.cm[n];
    var covL = T ? 100 * KL / T : NaN, covM = T ? 100 * KM / T : NaN;
    box.appendChild(tile("Token coverage", pct(covL), [
      sub("mature only: <b>" + pct(covM) + "</b>"),
      sub(fmtN(KL) + " of " + fmtN(T) + " content words known")]));

    var s = calc.inn.l, st = s[0] + s[1] + s[2] || 1;
    var bar = el("div", "tc-split");
    ["s0", "s1", "s2"].forEach(function (c, i) { var b = el("span", c); b.style.width = (100 * s[i] / st) + "%"; bar.appendChild(b); });
    box.appendChild(tile("i+0 sentences", pct(100 * s[0] / st), [bar,
      sub('<span class="tc-dot" style="background:var(--tc-i0)"></span>i+0 <b>' + pct(100 * s[0] / st) + "</b> · " +
          '<span class="tc-dot" style="background:var(--tc-i1)"></span>i+1 <b>' + pct(100 * s[1] / st) + "</b> · " +
          '<span class="tc-dot" style="background:var(--tc-i2)"></span>i+2+ <b>' + pct(100 * s[2] / st) + "</b>"),
      sub("sentences with 0 / 1 / 2+ unknown words")]));

    var u = calc.unk, top10 = u.slice(0, 10).reduce(function (a, w) { return a + w.n; }, 0);
    box.appendChild(tile("Unknown words", fmtN(u.length), [
      sub("learning the top 10 → <b>" + pct(T ? 100 * (KL + top10) / T : NaN) + "</b>")]));

    box.appendChild(tile("Length", fmtT(media.dur), [
      sub(fmtN(n) + " lines · " + fmtN(Math.round(T / Math.max(1, media.dur / 60))) + " content words/min")]));
  }

  var unkShowAll = false;
  function renderUnknown() {
    var body = $("tcUnkBody"); body.textContent = "";
    var T = calc.cn[calc.t.length], rows = unkShowAll ? calc.unk : calc.unk.slice(0, 50);
    rows.forEach(function (w, i) {
      var tr = el("tr");
      tr.appendChild(el("td", null, String(i + 1)));
      var tw = el("td", null, w.l); tw.lang = "ja"; tr.appendChild(tw);
      var tr2 = el("td", null, w.r && w.r !== w.l ? w.r : ""); tr2.lang = "ja"; tr.appendChild(tr2);
      tr.appendChild(el("td", "tc-r", fmtN(w.n)));
      tr.appendChild(el("td", "tc-r", "+" + (T ? (100 * w.n / T).toFixed(2) : "0") + "%"));
      var td = el("td", "tc-r"), b = el("button", "tc-seek", fmtT(w.first));
      b.type = "button"; b.disabled = !player; b.onclick = function () { seek(w.first); };
      td.appendChild(b); tr.appendChild(td);
      body.appendChild(tr);
    });
    if (!rows.length) { var tr = el("tr"), td = el("td", null, "None — you know every content word in this video."); td.colSpan = 6; tr.appendChild(td); body.appendChild(tr); }
    $("tcUnkMore").hidden = unkShowAll || calc.unk.length <= 50;
    $("tcUnkMore").textContent = "Show all " + fmtN(calc.unk.length);
  }

  function downloadCsv() {
    if (!calc) return;
    var lines = ["word,reading,count,first_seconds,first_time"];
    calc.unk.forEach(function (w) { lines.push([w.l, w.r, w.n, w.first.toFixed(2), fmtT(w.first)].join(",")); });
    var a = el("a"); a.href = URL.createObjectURL(new Blob(["﻿" + lines.join("\n")], { type: "text/csv" }));
    a.download = (media.name || "subtitles").replace(/\.[^.]+$/, "") + ".unknown-words.csv";
    document.body.appendChild(a); a.click(); a.remove();
    setTimeout(function () { URL.revokeObjectURL(a.href); }, 1000);
  }

  // ---------------------------------------------------------------- chart
  var chart = null;   // {pts, x(), y(), m, w, h, svg, ...}
  var NS = "http://www.w3.org/2000/svg";
  function sv(tag, attrs, text) {
    var n = document.createElementNS(NS, tag);
    for (var k in attrs) n.setAttribute(k, attrs[k]);
    if (text != null) n.textContent = text;
    return n;
  }
  function niceStep(span, target) {
    var raw = span / target, mag = Math.pow(10, Math.floor(Math.log10(raw))), f = raw / mag;
    return (f <= 1 ? 1 : f <= 2 ? 2 : f <= 5 ? 5 : 10) * mag;
  }
  function timeStep(dur, w) {
    var target = Math.max(2, Math.floor(w / 90)), steps = [30, 60, 120, 300, 600, 900, 1200, 1800, 3600, 7200];
    for (var i = 0; i < steps.length; i++) if (dur / steps[i] <= target) return steps[i];
    return steps[steps.length - 1];
  }

  function drawChart() {
    var box = $("tcChart"); box.textContent = "";
    var W = box.clientWidth || 700, H = W < 520 ? 220 : 260, m = { l: 40, r: 64, t: 12, b: 30 };
    var win = S.win, step = win / 6, dur = media.dur, pts = [];
    for (var c = 0; c <= dur + step / 2; c += step) {
      var r = windowCov(c - win / 2, c + win / 2);
      pts.push({ c: Math.min(c, dur), a: Math.max(0, c - win / 2), b: Math.min(dur, c + win / 2), tot: r.tot, l: r.l, m: r.m });
    }
    var showL = $("tcShowL").checked, showM = $("tcShowM").checked;
    var ys = [];
    pts.forEach(function (p) { if (p.tot) { if (showL) ys.push(p.l); if (showM) ys.push(p.m); } });
    var n = calc.t.length, T = calc.cn[n], avgL = T ? 100 * calc.cl[n] / T : NaN, avgM = T ? 100 * calc.cm[n] / T : NaN;
    var ymin = ys.length ? Math.min.apply(null, ys) : 0;
    var yStep = niceStep(100 - Math.max(0, ymin - 3), 5), y0 = Math.max(0, Math.floor((ymin - 3) / yStep) * yStep);
    var pw = W - m.l - m.r, ph = H - m.t - m.b;
    var X = function (t) { return m.l + (dur ? t / dur : 0) * pw; };
    var Y = function (v) { return m.t + (1 - (v - y0) / (100 - y0)) * ph; };
    var svg = sv("svg", { viewBox: "0 0 " + W + " " + H, height: H, role: "img",
      "aria-label": "Rolling " + Math.round(win / 60) + "-minute token coverage over the video; average learned " + pct(avgL) + ", mature " + pct(avgM) });

    for (var v = y0; v <= 100 + 1e-9; v += yStep) {
      svg.appendChild(sv("line", { "class": "grid", x1: m.l, x2: m.l + pw, y1: Y(v), y2: Y(v) }));
      svg.appendChild(sv("text", { x: m.l - 6, y: Y(v) + 4, "text-anchor": "end", "class": "muted" }, Math.round(v * 10) / 10 + "%"));
    }
    svg.appendChild(sv("line", { "class": "axis", x1: m.l, x2: m.l + pw, y1: m.t + ph, y2: m.t + ph }));
    var ts = timeStep(dur, pw);
    for (var tt = 0; tt <= dur + 1e-6; tt += ts) {
      svg.appendChild(sv("line", { "class": "axis", x1: X(tt), x2: X(tt), y1: m.t + ph, y2: m.t + ph + 4 }));
      svg.appendChild(sv("text", { x: X(tt), y: m.t + ph + 18, "text-anchor": "middle", "class": "muted" }, fmtT(tt)));
    }

    function path(key) {
      var d = "", pen = false;
      pts.forEach(function (p) {
        if (!p.tot) { pen = false; return; }
        d += (pen ? "L" : "M") + X(p.c).toFixed(1) + "," + Y(p[key]).toFixed(1); pen = true;
      });
      return d;
    }
    var labels = [];
    [["m", showM, avgM], ["l", showL, avgL]].forEach(function (s) {
      if (!s[1] || !isFinite(s[2])) return;
      svg.appendChild(sv("line", { "class": "avg avg-" + s[0], x1: m.l, x2: m.l + pw, y1: Y(s[2]), y2: Y(s[2]) }));
      svg.appendChild(sv("path", { "class": "ln ln-" + s[0], d: path(s[0]) }));
      labels.push({ y: Y(s[2]), t: "avg " + s[2].toFixed(1) + "%" });
    });
    if (labels.length === 2 && Math.abs(labels[0].y - labels[1].y) < 13) {   // keep the two avg labels apart
      var mid = (labels[0].y + labels[1].y) / 2, up = labels[0].y < labels[1].y ? 0 : 1;
      labels[up].y = mid - 6.5; labels[1 - up].y = mid + 6.5;
    }
    labels.forEach(function (lb) { svg.appendChild(sv("text", { x: m.l + pw + 6, y: lb.y + 4, "class": "muted" }, lb.t)); });

    var xh = sv("line", { "class": "xhair", y1: m.t, y2: m.t + ph, visibility: "hidden" });
    var dl = sv("circle", { "class": "dot", r: 4.5, visibility: "hidden", style: "fill:var(--tc-learned)" });
    var dm = sv("circle", { "class": "dot", r: 4.5, visibility: "hidden", style: "fill:var(--tc-mature)" });
    var ph_ = sv("line", { "class": "play", y1: m.t - 4, y2: m.t + ph, visibility: "hidden" });
    svg.appendChild(ph_); svg.appendChild(xh); svg.appendChild(dm); svg.appendChild(dl);
    var hit = sv("rect", { "class": "hit", x: m.l, y: m.t, width: pw, height: ph, tabindex: 0 });
    svg.appendChild(hit);
    box.appendChild(svg);
    box.classList.toggle("tc-seekable", !!player);
    var tip = el("div", "tc-tip"); tip.hidden = true; box.appendChild(tip);

    chart = { pts: pts, X: X, Y: Y, m: m, pw: pw, ph: ph, W: W, play: ph_, cur: -1 };

    function show(i) {
      if (i < 0 || i >= pts.length) return;
      chart.cur = i;
      var p = pts[i], x = X(p.c);
      xh.setAttribute("x1", x); xh.setAttribute("x2", x); xh.setAttribute("visibility", "visible");
      [[dl, "l", showL], [dm, "m", showM]].forEach(function (d) {
        if (d[2] && p.tot) { d[0].setAttribute("cx", x); d[0].setAttribute("cy", Y(p[d[1]])); d[0].setAttribute("visibility", "visible"); }
        else d[0].setAttribute("visibility", "hidden");
      });
      tip.textContent = "";
      tip.appendChild(el("div", null, fmtT(p.a) + " – " + fmtT(p.b)));
      if (!p.tot) tip.appendChild(el("div", null, "no speech"));
      else {
        [["l", "Learned", showL], ["m", "Mature", showM]].forEach(function (s) {
          if (!s[2]) return;
          var r = el("div"); r.appendChild(el("span", "tc-sw tc-sw-" + s[0]));
          r.appendChild(document.createTextNode(s[1] + " " + pct(p[s[0]]))); tip.appendChild(r);
        });
        tip.appendChild(el("div", null, fmtN(p.tot) + " content words"));
      }
      if (player) tip.appendChild(el("div", null, "click to jump here"));
      tip.hidden = false;
      var tw = tip.offsetWidth, left = x + 12;
      if (left + tw > W) left = x - tw - 12;
      tip.style.left = Math.max(0, left) + "px"; tip.style.top = m.t + "px";
    }
    function hide() {
      [xh, dl, dm].forEach(function (n) { n.setAttribute("visibility", "hidden"); }); tip.hidden = true; chart.cur = -1;
    }
    function idxAt(evt) {
      var rc = svg.getBoundingClientRect(), x = (evt.clientX - rc.left) * (W / rc.width);
      var t = (x - m.l) / pw * dur;
      return Math.max(0, Math.min(pts.length - 1, Math.round(t / step)));
    }
    hit.addEventListener("pointermove", function (e) { show(idxAt(e)); });
    hit.addEventListener("pointerleave", hide);
    hit.addEventListener("blur", hide);
    hit.addEventListener("click", function (e) {
      var rc = svg.getBoundingClientRect(), x = (e.clientX - rc.left) * (W / rc.width);
      seek(Math.max(0, Math.min(dur, (x - m.l) / pw * dur)));
    });
    hit.addEventListener("keydown", function (e) {
      var i = chart.cur < 0 ? 0 : chart.cur;
      if (e.key === "ArrowRight") { show(Math.min(pts.length - 1, i + 1)); e.preventDefault(); }
      else if (e.key === "ArrowLeft") { show(Math.max(0, i - 1)); e.preventDefault(); }
      else if (e.key === "Enter" && chart.cur >= 0) seek(pts[chart.cur].c);
    });

    $("tcChartNote").textContent = "Each point is the coverage of a " + Math.round(win / 60) + "-minute window centred on that moment. " +
      "Dashed lines are the whole-video averages." + (player ? " Click the chart to jump the video there." : "");
    if (player) movePlayhead(lastT - subOff());
  }

  function movePlayhead(t) {
    if (!chart || !chart.play) return;
    var x = chart.X(Math.min(t, media.dur));
    chart.play.setAttribute("x1", x); chart.play.setAttribute("x2", x); chart.play.setAttribute("visibility", "visible");
  }

  // ---------------------------------------------------------------- YouTube
  var player = null, playerVid = null, pollTimer = null, lastT = 0, lastCue = -2;
  // primed listening (null if its script didn't load); its subtitle offset shifts cue times onto
  // the video's clock, so media time = video time − subOff()
  var prim = window.PrimedListening ? window.PrimedListening({
    ppc: S.ppc,
    onPpc: function (v) { S.ppc = v; save(); },
    onOffset: function () { if (player && calc) updateLive(true); },
    onLine: function (show) { $("tcLine").classList.toggle("tc-line-hid", !show); }
  }) : null;
  function subOff() { return prim ? prim.offset() : 0; }

  function ytId(u) {
    u = (u || "").trim();
    if (/^[\w-]{11}$/.test(u)) return u;
    try {
      var x = new URL(u);
      if (/(^|\.)youtu\.be$/.test(x.hostname)) return x.pathname.slice(1, 12) || null;
      if (!/(^|\.)youtube(-nocookie)?\.com$/.test(x.hostname)) return null;
      if (x.searchParams.get("v")) return x.searchParams.get("v").slice(0, 11);
      var m = x.pathname.match(/\/(embed|live|shorts|v)\/([\w-]{11})/);
      return m ? m[2] : null;
    } catch (e) { return null; }
  }
  var ytP = null;
  function loadYT() {
    if (window.YT && window.YT.Player) return Promise.resolve();
    if (ytP) return ytP;
    ytP = new Promise(function (res) {
      var prev = window.onYouTubeIframeAPIReady;
      window.onYouTubeIframeAPIReady = function () { if (prev) prev(); res(); };
      var s = document.createElement("script"); s.src = "https://www.youtube.com/iframe_api"; document.head.appendChild(s);
    });
    return ytP;
  }

  function setupVideo() {
    var raw = $("tcYt").value.trim(), id = ytId(raw), st = $("tcMediaStatus");
    if (!raw) { teardownPlayer(); return; }
    if (!id) { status(st, "✗ that doesn't look like a YouTube link — showing the chart without the video.", "err"); teardownPlayer(); return; }
    $("tcPlayerWrap").hidden = false;
    if (player && playerVid === id) return;
    loadYT().then(function () {
      if (player) { player.cueVideoById(id); playerVid = id; return; }
      player = new window.YT.Player("tcPlayer", {
        // fs:0 hides YouTube's fullscreen button: it would fullscreen only the iframe, losing our
        // overlay — our own button (and f) fullscreens the whole video box instead
        videoId: id, playerVars: { playsinline: 1, rel: 0, modestbranding: 1, fs: 0 },
        events: {
          onReady: function () { playerVid = id; if (prim) prim.attach(player); startPoll(); renderUnknown(); drawChart(); },
          onStateChange: function (e) { if (prim) prim.state(e.data); }
        }
      });
    });
  }
  function teardownPlayer() {
    $("tcPlayerWrap").hidden = true;
    if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
    if (prim) prim.detach();
    if (player) { try { player.destroy(); } catch (e) { /* already gone */ } player = null; playerVid = null;
      // put a fresh placeholder back for the next player, keeping the primed-listening layers
      var wrap = $("tcVideo"), old = wrap.querySelectorAll("iframe, #tcPlayer"), d = el("div");
      for (var i = 0; i < old.length; i++) old[i].remove();
      d.id = "tcPlayer"; wrap.insertBefore(d, wrap.firstChild); }
    if (calc) { renderUnknown(); drawChart(); }
  }
  function startPoll() {
    if (pollTimer) clearInterval(pollTimer);
    pollTimer = setInterval(function () { updateLive(false); }, 200);
    updateLive(true);
  }
  function seek(t) {   // t in subtitle (media) time
    if (!player || !player.seekTo) return;
    t = Math.max(0, t + subOff());
    player.seekTo(t, true);
    if (player.getPlayerState && player.getPlayerState() !== 1) player.playVideo();
    lastT = t; updateLive(true);
    $("tcPlayerWrap").scrollIntoView({ behavior: "smooth", block: "nearest" });
  }

  function updateLive(force) {
    if (!player || !player.getCurrentTime || !calc) return;
    var t = player.getCurrentTime() || 0;
    if (!force && Math.abs(t - lastT) < 0.05) return;
    lastT = t;
    $("tcLiveTime").textContent = fmtT(t) + " / " + fmtT(media.dur);
    t -= subOff();   // onto the subtitles' clock
    var w = windowCov(t - S.win / 2, t + S.win / 2), so = windowCov(0, t + 1e-6);
    $("tcLiveWin").textContent = w.tot ? pct(w.l) + "  ·  mature " + pct(w.m) : "—";
    $("tcLiveSoFar").textContent = so.tot ? pct(so.l) + "  ·  mature " + pct(so.m) : "—";
    $("tcLiveWin").parentNode.firstElementChild.textContent = "around here (" + Math.round(S.win / 60) + " min)";
    movePlayhead(t);
    var i = lowerBound(t + 0.05) - 1, cue = media.cues[i];
    if (cue && t > cue.e + 2) i = -1;
    if (i === lastCue && !force) return;
    lastCue = i;
    var line = $("tcLine"); line.textContent = "";
    if (i < 0) { line.appendChild(el("span", "tc-ign", "Lines appear here as they're spoken.")); return; }
    media.cues[i].toks.forEach(function (tk) {
      var cls = tk.st === "unk" ? "tc-tok tc-unk" : tk.st === "young" ? "tc-tok tc-young" : tk.st === "ign" ? "tc-tok tc-ign" : null;
      var sp = el("span", cls, tk.s);
      if (tk.st !== "ign") sp.title = tk.l + (tk.r && tk.r !== tk.l ? " (" + tk.r + ")" : "");
      line.appendChild(sp);
    });
  }

  // ---------------------------------------------------------------- wire up
  $("tcUrl").value = S.url;
  $("tcMature").value = S.mature;
  $("tcKnownIvl").value = S.knownIvl;
  $("tcManualTag").value = S.manualTag;
  $("tcStability").checked = S.stability;
  $("tcExNames").checked = S.exNames;
  $("tcExLoan").checked = S.exLoan;
  $("tcWin").value = String(S.win);
  showCsv();

  $("tcConnect").addEventListener("click", connect);
  $("tcAddFilter").addEventListener("click", function () {
    var m = models.find(function (x) { return !S.filters.some(function (f) { return f.model === x.name; }); }) || models[0];
    S.filters.push({ model: m ? m.name : "", field: m ? bestField(m.fields) : "", inc: "", exc: "" });
    renderFilters();
  });
  $("tcLoadKnown").addEventListener("click", loadKnown);
  $("tcKnownCsv").addEventListener("change", function (e) { if (e.target.files.length) readCsv(e.target.files); e.target.value = ""; });
  $("tcClearCsv").addEventListener("click", function () { S.csvLemmas = []; save(); showCsv(); status($("tcKnownStatus"), "CSV words forgotten — press “Read my known words” again to refresh.", "ok"); });
  $("tcSubs").addEventListener("change", updateAnalyzeBtn);
  $("tcAnalyze").addEventListener("click", analyze);
  $("tcYt").addEventListener("change", function () { if (media) setupVideo(); });
  $("tcExNames").addEventListener("change", function (e) { S.exNames = e.target.checked; save(); reclassify(); });
  $("tcExLoan").addEventListener("change", function (e) { S.exLoan = e.target.checked; save(); reclassify(); });
  $("tcWin").addEventListener("change", function (e) { S.win = +e.target.value; save(); if (calc) { drawChart(); updateLive(true); } });
  $("tcShowL").addEventListener("change", function () { if (calc) drawChart(); });
  $("tcShowM").addEventListener("change", function () { if (calc) drawChart(); });
  $("tcUnkMore").addEventListener("click", function () { unkShowAll = true; renderUnknown(); });
  $("tcCsvOut").addEventListener("click", downloadCsv);
  var rw = 0;
  new ResizeObserver(function (en) {
    var w = Math.round(en[0].contentRect.width);
    if (w && w !== rw) { rw = w; if (calc) drawChart(); }
  }).observe($("tcChart"));
})();
