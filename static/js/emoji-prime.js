// Emoji Prime Player (tools/emoji-prime): plays the user's own video files with their emoji-prime
// web.json packs in FzPlayer (js/fz-player.js). The overlay renderer is the emojiPrime repo's (the
// YouTube extension's content.js / the offline player's player.js).
//
// Videos come from a folder (File System Access API, Chromium: the handle is kept in IndexedDB so
// the folder comes back next visit after one click) or from files picked by hand (any browser).
// A folder lists every video with a sibling <stem>.emojiprime/web.json (and <stem>[.ja].srt/.vtt
// as subtitles). Logged in, playing time goes to Track through POST /v1/watch/sessions with
// tool "emoji-prime" (Active immersion › Emoji Prime).
(function () {
  "use strict";
  var $ = function (id) { return document.getElementById(id); };
  var esc = FzPlayer.esc, fmt = FzPlayer.fmt, sw = FzPlayer.sw;
  var VIDEO_RE = /\.(mp4|mkv|webm|m4v|mov)$/i;
  var LAYERS = [["word", "Word pictures", "🍎"], ["rebus", "Rebus", "🧩"], ["vlog", "Illustrations", "🖼️"]];
  var ls = {
    get: function (k, d) { try { var v = localStorage.getItem("ep:" + k); return v == null ? d : JSON.parse(v); } catch (e) { return d; } },
    set: function (k, v) { try { localStorage.setItem("ep:" + k, JSON.stringify(v)); } catch (e) { /* private mode */ } },
    del: function (k) { try { localStorage.removeItem("ep:" + k); } catch (e) { /* private mode */ } },
  };
  var S = {
    enabled: ls.get("enabled", true),
    layers: Object.assign({ word: true, rebus: true, vlog: true }, ls.get("layers", {})),
    sizePct: ls.get("sizePct", 100),
    autoplay: ls.get("autoplay", true),
    theater: ls.get("theater", false),
  };
  var save = function (k) { ls.set(k, S[k]); };

  // ------------------------------------------------------------------ IndexedDB (folder handle, thumbnails)
  var dbP = null;
  function db() {
    if (dbP) return dbP;
    dbP = new Promise(function (ok, bad) {
      var r = indexedDB.open("fz-emoji-prime", 1);
      r.onupgradeneeded = function () { r.result.createObjectStore("kv"); r.result.createObjectStore("thumbs"); };
      r.onsuccess = function () { ok(r.result); };
      r.onerror = function () { bad(r.error); };
    });
    return dbP;
  }
  function idb(store, mode, fn) {
    return db().then(function (d) {
      return new Promise(function (ok, bad) {
        var tx = d.transaction(store, mode), req = fn(tx.objectStore(store));
        tx.oncomplete = function () { ok(req && req.result); };
        tx.onerror = function () { bad(tx.error); };
      });
    }).catch(function () { return null; });
  }
  var kvGet = function (k) { return idb("kv", "readonly", function (s) { return s.get(k); }); };
  var kvSet = function (k, v) { return idb("kv", "readwrite", function (s) { return s.put(v, k); }); };

  // ------------------------------------------------------------------ state
  var library = [];      // [{key, title, folder, file?: FileSystemFileHandle, packH?, subsH?, added?, ...}]
  var current = null;
  var pack = null, maxDur = 0, offset = 0, rebusIdx = [];
  var live = new Map();
  var folderHandle = null;

  // ------------------------------------------------------------------ the player
  var player = FzPlayer.create($("epStage"), {
    keys: true,
    theater: S.theater,
    onTheater: function (v) { S.theater = v; save("theater"); document.body.classList.toggle("ep-theater", v); },
    menu: menuRows,
    onMenu: onMenu,
    tip: function (t) { var r = rebusAt(t); return r && S.enabled ? esc(r) : ""; },
    onFrame: function (force) { draw(force); },
    onEnded: function () { savePos(true); startUpNext(); },
    onState: function (s) { watch.state(s); },
  });
  document.body.classList.toggle("ep-theater", S.theater);
  var video = player.video, layer = player.layer;
  // the pictures button (like YouTube's CC button)
  var epBtn = document.createElement("button");
  epBtn.type = "button"; epBtn.className = "fzp-b fzp-tog fzp-emoji"; epBtn.textContent = "🖼️";
  epBtn.addEventListener("click", function () { setEnabled(!S.enabled); });
  player.extra.appendChild(epBtn);
  function paintEpBtn() {
    epBtn.classList.toggle("fzp-on", S.enabled && !!pack);
    epBtn.dataset.tip = pack ? "Pictures " + (S.enabled ? "on" : "off") + " (Shift+E)" : "No pack loaded";
  }

  // ------------------------------------------------------------------ overlay (port of the extension's renderer)
  function setPack(p) {
    clearAll();
    pack = p; maxDur = 0; rebusIdx = [];
    if (pack) {
      pack.items.sort(function (x, y) { return x.s - y.s; });
      pack.items.forEach(function (it, i) { maxDur = Math.max(maxDur, it.e - it.s); if (it.k === "rebus") rebusIdx.push(i); });
    }
    paintEpBtn();
  }
  function clearAll() { live.forEach(function (el) { el.remove(); }); live.clear(); }
  function firstIndexAfter(t) {
    var items = pack.items, lo = 0, hi = items.length;
    while (lo < hi) { var m = (lo + hi) >> 1; if (items[m].s <= t) lo = m + 1; else hi = m; }
    return lo;
  }
  function makeEl(it) {
    var el;
    if (it.k === "rebus") {
      el = document.createElement("div");
      el.className = "ep-sticker ep-rebus";
      it.ch.forEach(function (ch) { var s = document.createElement("span"); s.textContent = ch; s.className = "ep-chunk"; el.appendChild(s); });
    } else if (it.u) {
      el = document.createElement("img");
      el.className = "ep-sticker ep-img"; el.alt = ""; el.draggable = false;
      el.referrerPolicy = "no-referrer";
      el.onerror = function () { el.remove(); };
      el.src = it.u;
    } else {
      el = document.createElement("div");
      el.className = "ep-sticker ep-emoji"; el.textContent = it.em;
    }
    layer.appendChild(el);
    return el;
  }
  function place(el, it, rect) {
    var box = pack.box * rect.h * it.sc * (S.sizePct / 100);
    el.style.left = it.p[0] * rect.w + "px";
    el.style.top = it.p[1] * rect.h + "px";
    el.style.transform = "translate(-50%, -50%) rotate(" + (it.a || 0) + "deg)";
    el.style.setProperty("--o", Math.max(1.5, box / 70).toFixed(1) + "px");
    if (it.k === "rebus") {
      var n = Math.max(1, Array.from(it.ch.join("")).length);
      el.style.fontSize = Math.min(box * 0.8, (box * 3.2) / n) + "px";
    } else if (it.u) {
      el.style.maxHeight = box * 0.9 + "px"; el.style.maxWidth = box * 0.86 + "px";
    } else {
      var m = Math.max(1, Array.from(it.em).length);
      el.style.fontSize = Math.min(box * 0.7, (box * 1.8) / m) + "px";
    }
  }
  function draw(force) {
    if (!pack || !S.enabled) { if (live.size) clearAll(); return; }
    var t = video.currentTime - offset, items = pack.items, want = new Set();
    for (var i = firstIndexAfter(t) - 1; i >= 0 && items[i].s >= t - maxDur; i--) {
      if (t < items[i].e && S.layers[items[i].k]) want.add(i);
    }
    live.forEach(function (el, i) { if (!want.has(i)) { el.remove(); live.delete(i); } });
    var rect = player.contentRect(), hKey = rect.h.toFixed(1) + "/" + S.sizePct;
    want.forEach(function (i) {
      var it = items[i], el = live.get(i), fresh = !el;
      if (fresh) { el = makeEl(it); live.set(i, el); }
      if (fresh || force || el.dataset.h !== hKey) { place(el, it, rect); el.dataset.h = hKey; }
      if (it.k === "rebus") {
        var kids = el.children;
        for (var j = 0; j < kids.length; j++) kids[j].classList.toggle("ep-hidden", t < it.rv[j]);
      }
    });
  }
  function rebusAt(t) {
    if (!pack || !rebusIdx.length) return "";
    t -= offset;
    var lo = 0, hi = rebusIdx.length;
    while (lo < hi) { var m = (lo + hi) >> 1; if (pack.items[rebusIdx[m]].s <= t) lo = m + 1; else hi = m; }
    for (var k = lo - 1; k >= 0 && k >= lo - 4; k--) {
      var it = pack.items[rebusIdx[k]];
      if (t < it.e + 0.5) return it.ch.join("");
    }
    return "";
  }

  // ------------------------------------------------------------------ settings (the player's gear menu)
  function setEnabled(v) { S.enabled = v; save("enabled"); player.toast("Pictures " + (v ? "on" : "off")); draw(true); paintEpBtn(); player.redraw(); }
  function setLayer(k, v) { S.layers[k] = v; save("layers"); draw(true); }
  function setSize(v) { S.sizePct = Math.max(30, Math.min(300, Math.round(v))); save("sizePct"); draw(true); }
  function setOffset(v) {
    offset = Math.round(v * 100) / 100;
    if (current && !current.ephemeral) ls.set("offset:" + current.key, offset);
    draw(true);
  }
  function menuRows() {
    var h = '<div class="fzp-mi fzp-check" data-act="enabled"><span class="fzp-mic">🖼️</span><span class="fzp-mil">Pictures</span>' + sw(S.enabled) + "</div>";
    LAYERS.forEach(function (l) {
      h += '<div class="fzp-mi fzp-check" data-act="layer" data-k="' + l[0] + '"><span class="fzp-mic">' + l[2] + '</span><span class="fzp-mil">' + l[1] + "</span>" + sw(S.layers[l[0]], !S.enabled) + "</div>";
    });
    h += '<div class="fzp-mi fzp-row"><span class="fzp-mic">🔍</span><span class="fzp-mil">Picture size</span><span class="fzp-step"><button type="button" data-act="size" data-d="-10" aria-label="Smaller">–</button><span>' + S.sizePct + '%</span><button type="button" data-act="size" data-d="10" aria-label="Bigger">+</button></span></div>';
    h += '<div class="fzp-mi fzp-row"><span class="fzp-mic">⏱️</span><span class="fzp-mil">Sync offset</span><span class="fzp-step"><button type="button" data-act="offset" data-d="-0.25" aria-label="Earlier">–</button><span>' + (offset >= 0 ? "+" : "") + offset.toFixed(2) + 's</span><button type="button" data-act="offset" data-d="0.25" aria-label="Later">+</button></span></div>';
    h += '<div class="fzp-mi fzp-check" data-act="autoplay"><span class="fzp-mic">⏭️</span><span class="fzp-mil">Autoplay next</span>' + sw(S.autoplay) + "</div>";
    return h;
  }
  function onMenu(a, el) {
    if (a === "enabled") setEnabled(!S.enabled);
    else if (a === "layer") { if (S.enabled) setLayer(el.dataset.k, !S.layers[el.dataset.k]); }
    else if (a === "size") setSize(S.sizePct + Number(el.dataset.d));
    else if (a === "offset") setOffset(offset + Number(el.dataset.d));
    else if (a === "autoplay") { S.autoplay = !S.autoplay; save("autoplay"); }
  }
  // emoji-prime's own keys (the player has the YouTube ones)
  document.addEventListener("keydown", function (e) {
    if (!current || $("epWatch").hidden || !$("epDlg").hidden) return;
    var tag = (e.target.tagName || "").toLowerCase();
    if (tag === "input" || tag === "textarea" || tag === "select" || e.ctrlKey || e.metaKey || e.altKey) return;
    var k = e.key, done = true;
    if (e.shiftKey && (k === "E" || k === "e")) setEnabled(!S.enabled);
    else if (e.shiftKey && (k === "W" || k === "R" || k === "I")) {
      var key = { W: "word", R: "rebus", I: "vlog" }[k];
      setLayer(key, !S.layers[key]);
      player.toast(LAYERS.filter(function (l) { return l[0] === key; })[0][1] + " " + (S.layers[key] ? "on" : "off"));
    } else if (e.shiftKey && k === "P") { var p = prevEntry(); if (p) open(p); }
    else if (k === "-") { setSize(S.sizePct - 10); player.toast("Picture size " + S.sizePct + "%"); }
    else if (k === "=" || k === "+") { setSize(S.sizePct + 10); player.toast("Picture size " + S.sizePct + "%"); }
    else if (k === "[") { setOffset(offset - 0.25); player.toast("Sync offset " + offset.toFixed(2) + " s"); }
    else if (k === "]") { setOffset(offset + 0.25); player.toast("Sync offset " + (offset >= 0 ? "+" : "") + offset.toFixed(2) + " s"); }
    else done = false;
    if (done) { e.preventDefault(); player.redraw(); }
  });
  var KEYS = [
    ["k / Space", "Play / pause"], ["j / l, ← / →", "Back / forward 10 s, 5 s"], [", / .", "Frame step (paused)"],
    ["↑ / ↓, m", "Volume, mute"], ["0–9, Home / End", "Jump"], ["Shift + > / <", "Speed"],
    ["f, t, c", "Full screen, theater mode, subtitles"], ["Shift + N / P", "Next / previous video"],
    ["Shift + E", "Pictures on / off"], ["Shift + W / R / I", "Word pictures / rebus / illustrations"],
    ["- / =", "Smaller / bigger pictures"], ["[ / ]", "Sync offset −/+ 0.25 s"],
  ];
  $("epKeys").innerHTML = KEYS.map(function (r) { return "<dt><kbd>" + esc(r[0]) + "</kbd></dt><dd>" + esc(r[1]) + "</dd>"; }).join("");

  // ------------------------------------------------------------------ resume positions
  var lastSave = 0;
  function savePos(force) {
    if (!current || current.ephemeral || !video.duration) return;
    var now = Date.now();
    if (!force && now - lastSave < 4000) return;
    lastSave = now;
    var t = video.currentTime;
    if (t > video.duration - 20 || video.ended) { ls.del("pos:" + current.key); ls.set("done:" + current.key, true); }
    else ls.set("pos:" + current.key, Math.round(t));
    ls.set("dur:" + current.key, Math.round(video.duration));
  }
  video.addEventListener("timeupdate", function () { savePos(false); });
  video.addEventListener("pause", function () { savePos(true); });
  window.addEventListener("pagehide", function () { savePos(true); });

  // ------------------------------------------------------------------ files → entries
  function stemOf(name) { return name.replace(/\.[^.]+$/, ""); }
  function prettyTitle(name) { return stemOf(name).replace(/_/g, " "); }
  function isPack(p) { return p && p.format === "emoji-prime-web" && Array.isArray(p.items) && typeof p.box === "number"; }
  function srtToVtt(txt) {
    if (/^\uFEFF?WEBVTT/.test(txt)) return txt;
    return "WEBVTT\n\n" + txt.replace(/^\uFEFF/, "").replace(/\r\n/g, "\n")
      .replace(/(\d\d:\d\d:\d\d),(\d\d\d)/g, "$1.$2").replace(/^\d+\n(?=\d\d:\d\d:\d\d\.\d\d\d -->)/gm, "");
  }
  function subsUrl(file) {
    return file.text().then(function (t) { return URL.createObjectURL(new Blob([srtToVtt(t)], { type: "text/vtt" })); });
  }

  // Walk a folder: videos with a sibling <stem>.emojiprime/web.json, subfolders up to 5 deep.
  async function scan(dir, path, out, depth) {
    var videos = [], packs = new Map(), subs = new Map(), dirs = [];
    for await (var entry of dir.values()) {
      var name = entry.name;
      if (entry.kind === "directory") {
        if (/\.emojiprime$/.test(name)) {
          try { packs.set(name.slice(0, -11), await entry.getFileHandle("web.json")); } catch (e) { /* no pack yet */ }
        } else if (!name.startsWith(".") && name !== "_build" && depth < 5) dirs.push(entry);
      } else if (VIDEO_RE.test(name)) videos.push(entry);
      else if (/\.(srt|vtt)$/i.test(name)) subs.set(name, entry);
    }
    videos.forEach(function (v) {
      var stem = stemOf(v.name), ph = packs.get(stem);
      if (!ph) return;
      var sh = subs.get(stem + ".ja.srt") || subs.get(stem + ".ja.vtt") || subs.get(stem + ".srt") || subs.get(stem + ".vtt") || null;
      var parent = path.split("/").filter(Boolean), folder = parent.length ? parent[parent.length - 1] : dir.name;
      if (folder === stem && parent.length > 1) folder = parent[parent.length - 2];
      out.push({ key: (path ? path + "/" : "") + v.name, title: prettyTitle(v.name), folder: folder, file: v, packH: ph, subsH: sh });
    });
    for (var i = 0; i < dirs.length; i++) await scan(dirs[i], (path ? path + "/" : "") + dirs[i].name, out, depth + 1);
    return out;
  }
  var natural = function (a, b) { return a.key.localeCompare(b.key, undefined, { numeric: true, sensitivity: "base" }); };

  async function useFolder(handle) {
    folderHandle = handle;
    msg("Looking for videos with packs in " + handle.name + "…");
    var found;
    try { found = await scan(handle, "", [], 0); } catch (e) { msg("Couldn't read that folder: " + (e.message || e), true); return; }
    found.sort(natural);
    found.forEach(function (f) { f.key = handle.name + "/" + f.key; });
    library = library.filter(function (v) { return v.added; }).concat(found);
    $("epReopen").hidden = true;
    $("epFolder").textContent = "📁 " + handle.name + " · " + found.length + " video" + (found.length === 1 ? "" : "s");
    msg(found.length ? "" : "No videos with an emoji-prime pack (a <name>.emojiprime/web.json next to the video) in " + handle.name + ".");
    renderLib();
  }
  async function pickFolder() {
    try {
      var h = await window.showDirectoryPicker({ id: "emoji-prime", mode: "read" });
      await kvSet("folder", h);
      await useFolder(h);
    } catch (e) { if (e && e.name !== "AbortError") msg("Couldn't open the folder: " + (e.message || e), true); }
  }
  async function restoreFolder() {
    if (!window.showDirectoryPicker) return;
    var h = await kvGet("folder");
    if (!h) return;
    var perm = "prompt";
    try { perm = await h.queryPermission({ mode: "read" }); } catch (e) { /* old handle */ }
    if (perm === "granted") return useFolder(h);
    // the browser wants a click before it gives the folder back
    $("epReopen").textContent = "Reopen 📁 " + h.name;
    $("epReopen").hidden = false;
    $("epReopen").onclick = async function () {
      try { if ((await h.requestPermission({ mode: "read" })) === "granted") useFolder(h); } catch (e) { pickFolder(); }
    };
  }
  function msg(text, bad) { $("epMsg").textContent = text || ""; $("epMsg").classList.toggle("ep-bad", !!bad); }

  // ------------------------------------------------------------------ library view
  var thumbQueue = [], thumbBusy = false;
  function renderLib() {
    var q = $("epSearch").value.trim().toLowerCase();
    var list = library.filter(function (v) { return !q || (v.title + " " + v.folder).toLowerCase().indexOf(q) >= 0; });
    $("epSearch").hidden = library.length < 6;
    $("epGrid").innerHTML = list.map(function (v) { return card(v, "ep-card"); }).join("");
    $("epEmpty").textContent = library.length ? (list.length ? "" : "No matches.")
      : (window.showDirectoryPicker ? "Open a folder of videos that have emoji-prime packs, or add a video." :
        "Add a video and its web.json pack. (Opening a whole folder needs Chrome or Edge.)");
    list.forEach(function (v) { if (v.file) queueThumb(v); });
  }
  function card(v, cls) {
    var dur = ls.get("dur:" + v.key, 0), pos = ls.get("pos:" + v.key, 0), done = ls.get("done:" + v.key, false);
    var pct = done ? 100 : dur && pos ? Math.min(100, (pos / dur) * 100) : 0;
    return '<a class="' + cls + (current && current.key === v.key ? " ep-cur" : "") + '" href="#" data-key="' + esc(v.key) + '">' +
      '<span class="ep-thumb" data-thumb="' + esc(v.key) + '">' + (v.thumb ? '<img alt="" src="' + v.thumb + '">' : '<span class="ep-ph">🎬</span>') +
      (dur ? '<span class="ep-badge">' + fmt(dur) + "</span>" : "") +
      (pct ? '<span class="ep-prog"><span style="width:' + pct + '%"></span></span>' : "") + "</span>" +
      '<span class="ep-ct"><span class="ep-ctitle">' + esc(v.title) + '</span><span class="ep-csub">' + esc(v.folder) + "</span></span></a>";
  }
  document.addEventListener("click", function (e) {
    var a = e.target.closest && e.target.closest("a[data-key]");
    if (!a || e.ctrlKey || e.metaKey || e.button !== 0) return;
    e.preventDefault();
    var v = library.filter(function (x) { return x.key === a.dataset.key; })[0];
    if (v) open(v);
  });
  $("epSearch").addEventListener("input", renderLib);

  // a frame ~12% in (at most 10 min), cached in IndexedDB by file name, size and date
  function queueThumb(v) {
    if (v.thumb || v.thumbQueued) return;
    v.thumbQueued = true; thumbQueue.push(v); pumpThumbs();
  }
  async function pumpThumbs() {
    if (thumbBusy) return;
    var v = thumbQueue.shift();
    if (!v) return;
    thumbBusy = true;
    try {
      var f = await v.file.getFile(), id = v.key + "|" + f.size + "|" + f.lastModified;
      var url = await idb("thumbs", "readonly", function (s) { return s.get(id); });
      if (!url) {
        url = await grabFrame(f);
        if (url) idb("thumbs", "readwrite", function (s) { return s.put(url, id); });
      }
      if (url) {
        v.thumb = url;
        document.querySelectorAll('[data-thumb="' + CSS.escape(v.key) + '"]').forEach(function (el) {
          var ph = el.querySelector(".ep-ph");
          if (ph) { var img = document.createElement("img"); img.alt = ""; img.src = url; ph.replaceWith(img); }
        });
      }
    } catch (e) { /* no thumbnail, the placeholder stays */ }
    thumbBusy = false;
    pumpThumbs();
  }
  function grabFrame(file) {
    return new Promise(function (ok) {
      var v = document.createElement("video"), url = URL.createObjectURL(file), done = false;
      var finish = function (r) { if (done) return; done = true; URL.revokeObjectURL(url); v.removeAttribute("src"); v.load(); ok(r); };
      v.muted = true; v.preload = "metadata";
      v.onloadedmetadata = function () { v.currentTime = Math.min((v.duration || 60) * 0.12, 600); };
      v.onseeked = function () {
        try {
          var c = document.createElement("canvas"), w = 320, h = Math.round(w * (v.videoHeight || 9) / (v.videoWidth || 16));
          c.width = w; c.height = h; c.getContext("2d").drawImage(v, 0, 0, w, h);
          finish(c.toDataURL("image/jpeg", 0.72));
        } catch (e) { finish(null); }
      };
      v.onerror = function () { finish(null); };
      setTimeout(function () { finish(null); }, 15000);
      v.src = url;
    });
  }

  // ------------------------------------------------------------------ watching
  var openSeq = 0;
  var urls = [];
  async function open(v) {
    var seq = ++openSeq;
    cancelUpNext();
    savePos(true);
    watch.end();
    urls.forEach(function (u) { URL.revokeObjectURL(u); }); urls = [];
    setPack(null);
    current = v;
    $("epLib").hidden = true; $("epWatch").hidden = false; $("epBar").classList.add("ep-bar-min");
    $("epTitle").textContent = v.title;
    $("epMeta").innerHTML = '<span class="ep-chip">' + esc(v.folder) + '</span> <span id="epPackMeta">Loading…</span>';
    renderSide();
    var file, p = v.pack || null, sub = null;
    try {
      file = v.videoFile || await v.file.getFile();
      if (!p && v.packH) p = JSON.parse(await (await v.packH.getFile()).text());
      if (v.subsUrl) sub = v.subsUrl;
      else if (v.subsH) { sub = await subsUrl(await v.subsH.getFile()); urls.push(sub); }
    } catch (e) {
      if (seq === openSeq) $("epPackMeta").textContent = "Couldn't read the files: " + (e.message || e) + ". Reopen the folder?";
      return;
    }
    if (seq !== openSeq) return;
    if (p && !isPack(p)) p = null;
    var yt = p && /^[\w-]{11}$/.test(p.youtube || "") ? p.youtube : null;
    offset = v.ephemeral ? 0 : ls.get("offset:" + v.key, 0);
    var src = URL.createObjectURL(file); urls.push(src);
    player.load(src, { subs: sub, resume: v.ephemeral ? 0 : ls.get("pos:" + v.key, 0) });
    setPack(p);
    $("epPackMeta").textContent = p ? "🖼️ " + p.items.length + " pictures" : "No pack: just the video";
    player.setNext(nextEntry() ? function () { open(nextEntry()); } : null);
    watch.video(yt);
    draw(true);
    window.scrollTo({ top: $("ep").offsetTop - 70, behavior: "smooth" });
    document.title = v.title + " · Emoji Prime Player";
  }
  function closeWatch() {
    savePos(true);
    watch.end();
    cancelUpNext();
    player.unload();
    setPack(null);
    current = null;
    $("epWatch").hidden = true; $("epLib").hidden = false; $("epBar").classList.remove("ep-bar-min");
    document.title = "Emoji Prime Player";
    renderLib();
  }
  $("epBack").addEventListener("click", closeWatch);
  function idx() { return current ? library.indexOf(current) : -1; }
  function nextEntry() { var i = idx(); return i >= 0 ? library[i + 1] || null : null; }
  function prevEntry() { var i = idx(); return i > 0 ? library[i - 1] : null; }
  function renderSide() {
    var i = idx(), order = i >= 0 ? library.slice(i + 1).concat(library.slice(0, i)) : library;
    $("epSide").hidden = !order.length;
    $("epSideList").innerHTML = order.map(function (v) { return card(v, "ep-sitem"); }).join("");
    order.forEach(function (v) { if (v.file) queueThumb(v); });
  }
  var upTimer = null;
  function startUpNext() {
    var n = nextEntry();
    if (!n || !S.autoplay) return;
    var c = 5;
    player.toast("Up next in " + c + ": " + n.title, 1200);
    upTimer = setInterval(function () {
      c -= 1;
      if (c <= 0) { cancelUpNext(); open(n); } else player.toast("Up next in " + c + ": " + n.title + "  (Esc cancels)", 1200);
    }, 1000);
  }
  function cancelUpNext() { if (upTimer) { clearInterval(upTimer); upTimer = null; } }
  document.addEventListener("keydown", function (e) { if (e.key === "Escape" && upTimer) { cancelUpNext(); player.toast("Cancelled"); } });

  // ------------------------------------------------------------------ add a video by hand
  var dlg = { video: null, pack: null, subs: null }, added = 0;
  function sortFiles(files) {
    Array.prototype.forEach.call(files, function (f) {
      if (/\.json$/i.test(f.name)) dlg.pack = f;
      else if (/\.(srt|vtt)$/i.test(f.name)) dlg.subs = f;
      else if (/^video\//.test(f.type) || VIDEO_RE.test(f.name)) dlg.video = f;
    });
  }
  function paintDlg() {
    [["video", "epDlgVideoN"], ["pack", "epDlgPackN"], ["subs", "epDlgSubsN"]].forEach(function (r) {
      $(r[1]).textContent = dlg[r[0]] ? dlg[r[0]].name : "None chosen";
      $(r[1]).classList.toggle("ep-chosen", !!dlg[r[0]]);
    });
    $("epDlgGo").disabled = !dlg.video;
  }
  function openDlg(files) {
    dlg = { video: null, pack: null, subs: null };
    if (files) sortFiles(files);
    paintDlg();
    $("epDlg").hidden = false;
    if (!video.paused) video.pause();
    $("epDlgGo").focus();
  }
  var closeDlg = function () { $("epDlg").hidden = true; };
  $("epAdd").addEventListener("click", function () { openDlg(); });
  $("epDlgX").addEventListener("click", closeDlg);
  $("epDlgCancel").addEventListener("click", closeDlg);
  $("epDlg").addEventListener("click", function (e) { if (e.target === $("epDlg")) closeDlg(); });
  document.addEventListener("keydown", function (e) { if (e.key === "Escape" && !$("epDlg").hidden) closeDlg(); });
  [["epDlgVideo", "video"], ["epDlgPack", "pack"], ["epDlgSubs", "subs"]].forEach(function (r) {
    $(r[0]).addEventListener("change", function (e) {
      var f = e.target.files && e.target.files[0];
      if (f) dlg[r[1]] = f;
      e.target.value = ""; paintDlg();
    });
  });
  $("epDlgGo").addEventListener("click", async function () {
    if (!dlg.video) return;
    var p = null;
    if (dlg.pack) {
      try { p = JSON.parse(await dlg.pack.text()); } catch (e) { p = null; }
      if (!isPack(p)) { alert("That isn't an emoji-prime web.json pack."); return; }
    }
    var yt = p && /^[\w-]{11}$/.test(p.youtube || "") ? p.youtube : "";
    var key = yt ? "yt:" + yt : "added:" + (++added);
    var su = dlg.subs ? await subsUrl(dlg.subs) : null;
    library = library.filter(function (v) { return v.key !== key; });
    var v = { key: key, added: true, ephemeral: !yt, title: prettyTitle(dlg.video.name), folder: "Added by hand",
              videoFile: dlg.video, pack: p, subsUrl: su };
    library.unshift(v);
    closeDlg();
    open(v);
  });
  // dropping files anywhere opens the dialog with them filled in
  document.addEventListener("dragover", function (e) {
    if (!e.dataTransfer || Array.prototype.indexOf.call(e.dataTransfer.types || [], "Files") < 0) return;
    e.preventDefault(); $("epDlgCard").classList.add("ep-over");
  });
  document.addEventListener("dragleave", function (e) { if (!e.relatedTarget) $("epDlgCard").classList.remove("ep-over"); });
  document.addEventListener("drop", function (e) {
    if (!e.dataTransfer || !e.dataTransfer.files.length) return;
    e.preventDefault(); $("epDlgCard").classList.remove("ep-over");
    if ($("epDlg").hidden) openDlg(e.dataTransfer.files); else { sortFiles(e.dataTransfer.files); paintDlg(); }
  });

  // ------------------------------------------------------------------ watch time → Track
  // Logged in (and unless switched off), the time the video really plays, unmuted, goes to Track
  // (Active immersion › Emoji Prime) through POST /v1/watch/sessions with tool "emoji-prime": one
  // session per video per visit, its cumulative seconds sent ~10 s in, then about once a minute,
  // ~1.5 s after it stops, and with keepalive when the page is hidden or left. The server adds the
  // whole minutes it hasn't added yet, so repeats are harmless. Same rules as Token Coverage's.
  var watch = (function () {
    var F = window.Fazon || null;
    var BEAT_MS = 60000, IDLE_MS = 1500, FIRST_S = 10, MAX_STEP = 2, ROTATE_MS = 20 * 3600e3;
    var who = F ? "unknown" : "offline", dead = false, on = ls.get("track", true);
    var sess = null, all = [], vid = null, lastWall = performance.now(), lastVT = null;
    var idleSince = 0, idleSent = false, lastBeat = 0, busy = false, queue = [], skip = 0, bad400 = 0, err = "", noteSeen = "";
    var box = $("epTrack"), onWrap = $("epTrackOnWrap"), onBox = $("epTrackOn"), msgEl = $("epTrackMsg"), noteEl = $("epTrackNote");

    function newId() {
      var b = new Uint8Array(16), s = "";
      crypto.getRandomValues(b);
      for (var i = 0; i < b.length; i++) s += (b[i] < 16 ? "0" : "") + b[i].toString(16);
      return s;
    }
    var unsent = function (s) { return s.secs - s.sent >= 0.5; };
    var counting = function () { return who === "user" && !dead && on && !!current; };
    var playing = function () { return !video.paused && !video.ended && video.readyState >= 3 && !(video.muted || video.volume === 0); };

    function tick() {
      var now = performance.now(), dt = (now - lastWall) / 1000, vt = video.currentTime || 0, adv = lastVT == null ? 0 : vt - lastVT;
      lastWall = now; lastVT = vt;
      if (counting() && playing() && adv > 0.01) {
        var sec = Math.min(dt, adv / (video.playbackRate || 1), document.hidden ? 70 : MAX_STEP);
        if (sec > 0) {
          if (!sess) { sess = { cid: newId(), started: new Date(Date.now() - sec * 1000).toISOString(), t0: Date.now(), video: vid, secs: 0, sent: 0, since: 0, tried: false, added: null }; all.push(sess); }
          if (!unsent(sess)) sess.since = now;
          sess.secs += sec;
        }
      }
      if (sess && Date.now() - sess.t0 > ROTATE_MS) end();
      if (who !== "user" || dead) return;
      if (!playing()) { if (!idleSince) idleSince = now; } else { idleSince = 0; idleSent = false; }
      var retry = all.filter(function (s) { return s !== sess && unsent(s); });
      var due = retry.length > 0 && now - lastBeat >= BEAT_MS;
      if (sess && unsent(sess)) {
        if (sess.tried ? now - Math.max(lastBeat, sess.since) >= BEAT_MS : sess.secs >= FIRST_S) due = true;
        if (idleSince && !idleSent && now - idleSince >= IDLE_MS) { due = true; idleSent = true; }
      }
      if (!due) return;
      lastBeat = now;
      if (skip > 0) { skip--; return; }
      retry.forEach(function (s) { send(s); });
      if (sess && unsent(sess)) send(sess);
    }
    function send(s, keep) {
      if (!F || who !== "user" || dead || !(s.secs > 0)) return;
      if (busy && !keep) { if (queue.indexOf(s) < 0) queue.push(s); return; }
      var secs = s.secs, body = { client_id: s.cid, started: s.started, active_seconds: Math.round(secs * 100) / 100, tool: "emoji-prime" };
      if (s.video) body.video_id = s.video;
      s.tried = true;
      if (!keep) busy = true;
      var req;
      try { req = F.api("/v1/watch/sessions", { method: "POST", body: body, keepalive: !!keep }); } catch (e) { req = Promise.reject(e); }
      req.then(function (d) {
        s.sent = Math.max(s.sent, secs);
        if (d) s.added = d.track_added || null;
        err = ""; bad400 = 0;
        if (s.added && s.added.note && s.added.note !== noteSeen) { noteSeen = s.added.note; noteEl.textContent = noteSeen; noteEl.hidden = false; setTimeout(function () { noteEl.hidden = true; }, 15000); }
      }, function (e) {
        if (e.status === 401) { dead = true; who = "anon"; }
        else if (e.status === 429) { skip = 3; err = "Track is busy, trying again in a few minutes"; }
        else if (e.status === 400) { s.sent = s.secs; if (s === sess) sess = null; err = e.message; if (++bad400 >= 3) dead = true; }
        else err = "couldn't reach the server, will retry";
      }).then(function () {
        if (!keep) { busy = false; var n = queue.shift(); if (n) send(n); }
        paint();
      });
    }
    function end() { var s = sess; sess = null; if (s && unsent(s)) send(s); }
    function flushAll() { tick(); all.forEach(function (s) { if (unsent(s)) send(s, true); }); }
    function paint() {
      box.hidden = !current || who === "offline" || who === "unknown";
      if (box.hidden) return;
      msgEl.textContent = "";
      onWrap.hidden = who !== "user";
      if (who !== "user") {
        noteEl.hidden = true;
        var a = document.createElement("a"); a.textContent = "Log in to add your watch time to Track"; a.href = F.loginHref(location.pathname);
        msgEl.appendChild(a); return;
      }
      onBox.checked = on;
      if (!on) { msgEl.textContent = "Off: watching here isn't added to your Track."; return; }
      if (dead) { msgEl.textContent = "Not tracking: " + (err || "the server refused it"); return; }
      var secs = 0;
      all.forEach(function (s) { if (s.cur === current) secs += s.added ? (s.added.hours || 0) * 3600 : 0; });
      msgEl.appendChild(document.createTextNode(Math.round(secs / 60) + " min this video → "));
      var l = document.createElement("a"); l.href = "/track/"; l.textContent = "Active immersion › Emoji Prime"; msgEl.appendChild(l);
      if (err) msgEl.appendChild(document.createTextNode(" · " + err));
    }
    onBox.addEventListener("change", function () {
      if (!onBox.checked) { tick(); end(); }
      on = onBox.checked; ls.set("track", on); lastVT = video.currentTime; paint();
    });
    window.addEventListener("pagehide", flushAll);
    document.addEventListener("visibilitychange", function () { if (document.visibilityState === "hidden") flushAll(); });
    if (F) F.me().then(function (m) { who = m.offline ? "offline" : m.user ? "user" : "anon"; paint(); });
    setInterval(function () { tick(); if (sess) sess.cur = current; }, 1000);
    video.addEventListener("seeked", function () { lastVT = video.currentTime; });
    return {
      video: function (id) { end(); vid = id; lastVT = video.currentTime; paint(); },
      end: end,
      state: function () { tick(); },
    };
  })();

  // ------------------------------------------------------------------ start
  if (window.showDirectoryPicker) { $("epOpen").hidden = false; $("epOpen").addEventListener("click", pickFolder); }
  renderLib();
  restoreFolder();
})();
