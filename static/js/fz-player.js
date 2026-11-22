// FzPlayer: a YouTube-style player for a local video file (a <video> with our own controls).
// Used by the Emoji Prime tool and by Token Coverage (local files). A port of the emojiPrime repo's
// offline player (player/player.js), as a component.
//
//   const p = FzPlayer.create(hostEl, opts)
//   p.load(src, { subs: vttUrl|null, resume: seconds|0 })   p.unload()
//   p.video · p.layer (an empty box sized to the visible picture, for overlays) · p.root
//   p.toast(msg) · p.redraw() (re-render the menu) · p.setNext(fnOrNull) · p.destroy()
//   p.yt: the YouTube IFrame API subset primed-listening.js / token-coverage.js use
//         (getPlayerState, getCurrentTime, getDuration, getPlaybackRate, seekTo, playVideo,
//          pauseVideo, isMuted, mute, unMute, getIframe, destroy) — its state changes go to
//          opts.onState(ytState).
//
// opts:
//   keys: true         handle the YouTube keyboard shortcuts on the page (false: the host does it)
//   fullscreenEl       what full screen enlarges (default: the player); a host's wrapper keeps its overlays
//   onTheater(on)      show a theater-mode button; the host does the layout
//   theater            its starting state
//   menu(page)         extra settings rows: returns HTML (data-act on clickable rows) for page "main"
//   onMenu(act, el)    a click on one of those rows; return true to keep the menu where it is
//   tip(t)             extra HTML for the progress-bar tooltip at time t (e.g. the rebus there)
//   onTime(t), onFrame(force)  called on every frame while playing / on seeks, pauses and resizes
//   onSpeed(rate), onEnded(), onState(ytState), onError(msg)
(function () {
  "use strict";
  var SPEEDS = [0.25, 0.5, 0.75, 1, 1.25, 1.5, 1.75, 2];
  var P = {
    play: "M12 26V10l14 8z",
    pause: "M11 10h5v16h-5zm9 0h5v16h-5z",
    replay: "M18 11V7l-5 5 5 5v-4c3.3 0 6 2.7 6 6s-2.7 6-6 6-6-2.7-6-6h-2c0 4.4 3.6 8 8 8s8-3.6 8-8-3.6-8-8-8z",
    next: "M12 24l8.5-6L12 12v12zm10-12v12h2V12h-2z",
    vol: "M8 21h4l5 5V10l-5 5H8v6zm13.5-3c0-1.8-1-3.3-2.5-4v8c1.5-.7 2.5-2.2 2.5-4zM19 8.2v2.1c2.9.9 5 3.6 5 6.7s-2.1 5.8-5 6.7v2.1c4-.9 7-4.5 7-8.8s-3-7.9-7-8.8z",
    volLow: "M8 21h4l5 5V10l-5 5H8v6zm13.5-3c0-1.8-1-3.3-2.5-4v8c1.5-.7 2.5-2.2 2.5-4z",
    mute: "M8 21h4l5 5V10l-5 5H8v6zm19-6.6L25.6 13 22 16.6 18.4 13 17 14.4l3.6 3.6-3.6 3.6 1.4 1.4 3.6-3.6 3.6 3.6 1.4-1.4-3.6-3.6z",
    gear: "M23.9 18.9a6.6 6.6 0 0 0 0-1.8l2-1.5c.2-.1.2-.4.1-.6l-1.9-3.3c-.1-.2-.4-.3-.6-.2l-2.3 1a7 7 0 0 0-1.6-.9l-.4-2.5a.5.5 0 0 0-.5-.4h-3.8a.5.5 0 0 0-.5.4l-.4 2.5c-.6.2-1.1.5-1.6.9l-2.3-1c-.2-.1-.5 0-.6.2l-1.9 3.3c-.1.2-.1.5.1.6l2 1.5a6.6 6.6 0 0 0 0 1.8l-2 1.5c-.2.1-.2.4-.1.6l1.9 3.3c.1.2.4.3.6.2l2.3-1c.5.4 1 .7 1.6.9l.4 2.5c0 .2.3.4.5.4h3.8c.2 0 .5-.2.5-.4l.4-2.5c.6-.2 1.1-.5 1.6-.9l2.3 1c.2.1.5 0 .6-.2l1.9-3.3c.1-.2.1-.5-.1-.6l-2-1.5zM18 21.5a3.5 3.5 0 1 1 0-7 3.5 3.5 0 0 1 0 7z",
    theater: "M8 12h20v12H8z M10 14v8h16v-8z",
    theaterOff: "M10 13h16v10H10z M12 15v6h12v-6z",
    fs: "M10 16h2v-4h4v-2h-6v6zm2 4h-2v6h6v-2h-4v-4zm12 4h-4v2h6v-6h-2v4zm-4-14v2h4v4h2v-6h-6z",
    fsExit: "M14 14h-4v2h6v-6h-2v4zm-4 8h4v4h2v-6h-6v2zm10 4h2v-4h4v-2h-6v6zm2-12v-4h-2v6h6v-2h-4z",
    cc: "M25 10H11c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V12c0-1.1-.9-2-2-2zm-9 7h-1.5v-.5h-2v3h2V19H16v1c0 .6-.4 1-1 1h-3c-.6 0-1-.4-1-1v-4c0-.6.4-1 1-1h3c.6 0 1 .4 1 1v1zm7 0h-1.5v-.5h-2v3h2V19H23v1c0 .6-.4 1-1 1h-3c-.6 0-1-.4-1-1v-4c0-.6.4-1 1-1h3c.6 0 1 .4 1 1v1z",
  };
  var svg = function (n) { return '<svg viewBox="0 0 36 36" aria-hidden="true"><path fill-rule="evenodd" d="' + P[n] + '"/></svg>'; };
  var esc = function (s) { return String(s).replace(/[&<>"]/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]; }); };
  function fmt(t) {
    if (!isFinite(t) || t < 0) t = 0;
    t = Math.floor(t);
    var h = Math.floor(t / 3600), m = Math.floor((t % 3600) / 60), s = t % 60;
    var p2 = function (n) { return (n < 10 ? "0" : "") + n; };
    return h ? h + ":" + p2(m) + ":" + p2(s) : m + ":" + p2(s);
  }
  var ls = {
    get: function (k, d) { try { var v = localStorage.getItem("fzp:" + k); return v == null ? d : JSON.parse(v); } catch (e) { return d; } },
    set: function (k, v) { try { localStorage.setItem("fzp:" + k, JSON.stringify(v)); } catch (e) { /* private mode */ } },
  };

  var HTML =
    '<video class="fzp-video" preload="auto" playsinline></video>' +
    '<div class="fzp-layer"></div>' +
    '<div class="fzp-catch"></div>' +
    '<div class="fzp-bezel"></div>' +
    '<div class="fzp-seek fzp-seek-l"></div><div class="fzp-seek fzp-seek-r"></div>' +
    '<div class="fzp-toast" role="status"></div>' +
    '<div class="fzp-spin" hidden></div>' +
    '<div class="fzp-chrome">' +
      '<div class="fzp-prog" role="slider" aria-label="Seek" tabindex="-1"><div class="fzp-track">' +
        '<div class="fzp-buf"></div><div class="fzp-hov"></div><div class="fzp-played"></div></div>' +
        '<div class="fzp-scrub"></div><div class="fzp-tip"></div></div>' +
      '<div class="fzp-ctl"><div class="fzp-ctl-l">' +
        '<button type="button" class="fzp-b" data-b="play"></button>' +
        '<button type="button" class="fzp-b" data-b="next" hidden></button>' +
        '<div class="fzp-vol"><button type="button" class="fzp-b" data-b="mute"></button>' +
          '<div class="fzp-vslide"><div class="fzp-vtrack"><div class="fzp-vfill"></div><div class="fzp-vknob"></div></div></div></div>' +
        '<div class="fzp-time"><span class="fzp-cur">0:00</span> / <span class="fzp-dur">0:00</span></div>' +
      '</div><div class="fzp-ctl-r">' +
        '<span class="fzp-extra"></span>' +
        '<button type="button" class="fzp-b fzp-tog" data-b="cc" hidden></button>' +
        '<button type="button" class="fzp-b" data-b="gear"></button>' +
        '<button type="button" class="fzp-b" data-b="theater" hidden></button>' +
        '<button type="button" class="fzp-b" data-b="fs"></button>' +
      '</div></div>' +
    '</div>' +
    '<div class="fzp-menu" hidden></div>' +
    '<div class="fzp-ctip"></div>';

  function create(host, opts) {
    opts = opts || {};
    var root = document.createElement("div");
    root.className = "fzp";
    root.tabIndex = -1;
    root.innerHTML = HTML;
    host.appendChild(root);
    var q = function (s) { return root.querySelector(s); };
    var video = q(".fzp-video"), layer = q(".fzp-layer"), menu = q(".fzp-menu"), prog = q(".fzp-prog"), tip = q(".fzp-tip");
    var btn = function (n) { return q('[data-b="' + n + '"]'); };
    var S = { volume: ls.get("volume", 1), muted: ls.get("muted", false), speed: ls.get("speed", 1), subs: ls.get("subs", false) };
    var theater = !!opts.theater, next = null, rafId = null, dragging = false, menuPage = "main", resumeAt = 0, loaded = false;
    var fsEl = opts.fullscreenEl || root;

    // ---------------------------------------------------------------- small helpers
    var toastTimer = null;
    function toast(msg, ms) {
      var el = q(".fzp-toast");
      el.textContent = msg; el.classList.add("fzp-show");
      clearTimeout(toastTimer);
      toastTimer = setTimeout(function () { el.classList.remove("fzp-show"); }, ms || 1300);
    }
    function bezel(html) {
      var b = q(".fzp-bezel");
      b.innerHTML = html; b.classList.remove("fzp-go"); void b.offsetWidth; b.classList.add("fzp-go");
    }
    function contentRect() {
      var pw = root.clientWidth, ph = root.clientHeight, vw = video.videoWidth || 16, vh = video.videoHeight || 9;
      var s = Math.min(pw / vw, ph / vh), w = vw * s, h = vh * s;
      return { left: (pw - w) / 2, top: (ph - h) / 2, w: w, h: h };
    }
    function fitLayer() {
      var r = contentRect();
      layer.style.left = r.left + "px"; layer.style.top = r.top + "px";
      layer.style.width = r.w + "px"; layer.style.height = r.h + "px";
      return r;
    }
    function frame(force) { fitLayer(); if (opts.onFrame) opts.onFrame(!!force); }

    // ---------------------------------------------------------------- frame loop + progress
    function tick() {
      rafId = null;
      frame(false);
      updateProgress();
      if (opts.onTime) opts.onTime(video.currentTime);
      if (!video.paused) rafId = requestAnimationFrame(tick);
    }
    function startRaf() { if (rafId == null) rafId = requestAnimationFrame(tick); }
    function updateProgress(previewT) {
      var d = video.duration || 0, t = previewT != null ? previewT : video.currentTime, pct = d ? (t / d) * 100 : 0;
      q(".fzp-played").style.width = pct + "%";
      q(".fzp-scrub").style.left = pct + "%";
      q(".fzp-cur").textContent = fmt(t);
      var buf = 0;
      for (var i = 0; i < video.buffered.length; i++) {
        if (video.buffered.start(i) <= video.currentTime + 0.5) buf = Math.max(buf, video.buffered.end(i));
      }
      q(".fzp-buf").style.width = d ? (buf / d) * 100 + "%" : "0";
    }
    function posToTime(e) {
      var r = prog.getBoundingClientRect(), x = Math.min(Math.max(0, e.clientX - r.left), r.width);
      return { t: (x / r.width) * (video.duration || 0), x: x, w: r.width };
    }
    function showTip(e) {
      var p = posToTime(e);
      q(".fzp-hov").style.width = (p.x / p.w) * 100 + "%";
      var extra = opts.tip ? opts.tip(p.t) : "";
      tip.innerHTML = (extra ? '<span class="fzp-tip-x">' + extra + "</span>" : "") + fmt(p.t);
      var tw = tip.offsetWidth || 50;
      tip.style.left = Math.min(Math.max(p.x, tw / 2), p.w - tw / 2) + "px";
      return p.t;
    }
    prog.addEventListener("mousemove", showTip);
    prog.addEventListener("mousedown", function (e) {
      if (e.button !== 0 || !loaded) return;
      dragging = true; prog.classList.add("fzp-drag");
      var wasPaused = video.paused;
      video.pause();
      var move = function (ev) { var t = showTip(ev); updateProgress(t); video.currentTime = t; };
      move(e);
      var up = function (ev) {
        dragging = false; prog.classList.remove("fzp-drag");
        video.currentTime = posToTime(ev).t;
        if (!wasPaused) video.play();
        document.removeEventListener("mousemove", move); document.removeEventListener("mouseup", up);
      };
      document.addEventListener("mousemove", move); document.addEventListener("mouseup", up);
      e.preventDefault();
    });

    // ---------------------------------------------------------------- volume
    var vslide = q(".fzp-vslide");
    function applyVolume() {
      video.volume = S.volume; video.muted = S.muted;
      var v = S.muted ? 0 : S.volume;
      q(".fzp-vfill").style.width = v * 100 + "%"; q(".fzp-vknob").style.left = v * 100 + "%";
      btn("mute").innerHTML = svg(v === 0 ? "mute" : v < 0.5 ? "volLow" : "vol");
      btn("mute").dataset.tip = S.muted ? "Unmute (m)" : "Mute (m)";
    }
    function setVolume(v, show) {
      S.volume = Math.min(1, Math.max(0, Math.round(v * 100) / 100)); S.muted = S.volume === 0;
      ls.set("volume", S.volume); ls.set("muted", S.muted);
      applyVolume();
      if (show) bezel(Math.round(S.volume * 100) + "%");
    }
    function toggleMute() {
      S.muted = !S.muted;
      if (!S.muted && S.volume === 0) S.volume = 0.5;
      ls.set("muted", S.muted); ls.set("volume", S.volume);
      applyVolume(); bezel(svg(S.muted ? "mute" : "vol"));
    }
    btn("mute").addEventListener("click", toggleMute);
    vslide.addEventListener("mousedown", function (e) {
      var track = q(".fzp-vtrack");
      var move = function (ev) { var r = track.getBoundingClientRect(); setVolume((ev.clientX - r.left) / r.width); };
      vslide.classList.add("fzp-drag"); move(e);
      var up = function () { vslide.classList.remove("fzp-drag"); document.removeEventListener("mousemove", move); document.removeEventListener("mouseup", up); };
      document.addEventListener("mousemove", move); document.addEventListener("mouseup", up);
      e.preventDefault();
    });

    // ---------------------------------------------------------------- play / seek / speed
    function togglePlay() { if (!loaded) return; if (video.paused || video.ended) video.play(); else video.pause(); }
    var seekTimers = {}, seekAcc = { l: 0, r: 0 };
    function seekBy(dt) {
      if (!loaded) return;
      video.currentTime = Math.min(Math.max(0, video.currentTime + dt), video.duration || 0);
      var side = dt < 0 ? "l" : "r", el = q(".fzp-seek-" + side);
      seekAcc[side] += Math.abs(dt);
      el.textContent = (dt < 0 ? "◀◀ " : "▶▶ ") + seekAcc[side] + " seconds";
      el.classList.add("fzp-show");
      clearTimeout(seekTimers[side]);
      seekTimers[side] = setTimeout(function () { el.classList.remove("fzp-show"); seekAcc[side] = 0; }, 650);
      wake();
    }
    function setSpeed(v) {
      S.speed = v; ls.set("speed", v); video.playbackRate = v; toast(v + "x");
      if (opts.onSpeed) opts.onSpeed(v);
    }
    function stepSpeed(dir) {
      var i = SPEEDS.indexOf(S.speed), j = Math.min(SPEEDS.length - 1, Math.max(0, (i < 0 ? 3 : i) + dir));
      setSpeed(SPEEDS[j]);
    }
    btn("play").addEventListener("click", togglePlay);
    btn("next").addEventListener("click", function () { if (next) next(); });
    var clickTimer = null;
    q(".fzp-catch").addEventListener("click", function () {
      if (!menu.hidden) { closeMenu(); return; }
      clearTimeout(clickTimer);
      clickTimer = setTimeout(togglePlay, 200);
    });
    q(".fzp-catch").addEventListener("dblclick", function () { clearTimeout(clickTimer); toggleFullscreen(); });

    // YouTube player states for the host: -1 unstarted, 0 ended, 1 playing, 2 paused, 3 buffering
    var ytState = -1;
    function setState(s) { if (s === ytState) return; ytState = s; if (opts.onState) opts.onState(s); }
    video.addEventListener("play", function () { updateButtons(); bezel(svg("play")); startRaf(); wake(); setState(1); });
    video.addEventListener("playing", function () { q(".fzp-spin").hidden = true; setState(1); });
    video.addEventListener("pause", function () {
      updateButtons();
      if (!dragging && !video.ended) bezel(svg("pause"));
      frame(true); updateProgress(); wake(true);
      if (!video.ended) setState(2);
    });
    video.addEventListener("seeked", function () { frame(true); updateProgress(); if (opts.onTime) opts.onTime(video.currentTime); q(".fzp-spin").hidden = true; });
    video.addEventListener("timeupdate", function () { if (video.paused) { frame(false); updateProgress(); } });
    video.addEventListener("progress", function () { updateProgress(); });
    video.addEventListener("waiting", function () { q(".fzp-spin").hidden = false; if (!video.paused) setState(3); });
    video.addEventListener("canplay", function () { q(".fzp-spin").hidden = true; });
    video.addEventListener("loadedmetadata", function () {
      q(".fzp-dur").textContent = fmt(video.duration);
      video.playbackRate = S.speed;
      if (resumeAt > 5 && resumeAt < video.duration - 20) { video.currentTime = resumeAt; toast("Resumed at " + fmt(resumeAt), 1800); }
      frame(true); updateProgress();
    });
    video.addEventListener("ended", function () { updateButtons(); setState(0); if (opts.onEnded) opts.onEnded(); });
    video.addEventListener("error", function () {
      if (!loaded) return;
      var err = video.error, msg = err && err.code === 4 ? "This browser can't play this video's format" : "The video failed to load";
      toast(msg, 5000);
      if (opts.onError) opts.onError(msg);
    });

    // ---------------------------------------------------------------- idle chrome + button tips
    var idleTimer = null;
    function wake(stay) {
      root.classList.remove("fzp-idle");
      clearTimeout(idleTimer);
      if (!stay && !video.paused) idleTimer = setTimeout(function () { if (!video.paused && menu.hidden && !dragging) root.classList.add("fzp-idle"); }, 2500);
    }
    root.addEventListener("mousemove", function () { wake(); });
    root.addEventListener("mouseleave", function () { if (!video.paused && menu.hidden) root.classList.add("fzp-idle"); });
    var ctip = q(".fzp-ctip");
    root.addEventListener("mouseover", function (e) {
      var b = e.target.closest && e.target.closest(".fzp-b");
      if (!b || !menu.hidden || !b.dataset.tip) { ctip.classList.remove("fzp-show"); return; }
      ctip.textContent = b.dataset.tip;
      var pr = root.getBoundingClientRect(), br = b.getBoundingClientRect();
      ctip.classList.add("fzp-show");
      var w = ctip.offsetWidth;
      ctip.style.left = Math.min(Math.max(8, br.left - pr.left + br.width / 2 - w / 2), pr.width - w - 8) + "px";
    });

    // ---------------------------------------------------------------- full screen / theater / subs
    function isFs() { return document.fullscreenElement === fsEl || document.webkitFullscreenElement === fsEl; }
    function toggleFullscreen() {
      if (isFs()) (document.exitFullscreen || document.webkitExitFullscreen).call(document);
      else { var r = (fsEl.requestFullscreen || fsEl.webkitRequestFullscreen); if (r) { var pr = r.call(fsEl); if (pr && pr.catch) pr.catch(function () {}); } }
    }
    btn("fs").addEventListener("click", toggleFullscreen);
    document.addEventListener("fullscreenchange", onFs);
    function onFs() { updateButtons(); setTimeout(function () { frame(true); }, 60); }
    if (opts.onTheater) {
      btn("theater").hidden = false;
      btn("theater").addEventListener("click", function () { setTheater(!theater); });
    }
    function setTheater(v) { theater = v; updateButtons(); if (opts.onTheater) opts.onTheater(v); setTimeout(function () { frame(true); }, 30); }
    function setSubs(v) {
      S.subs = v; ls.set("subs", v);
      for (var i = 0; i < video.textTracks.length; i++) video.textTracks[i].mode = v ? "showing" : "hidden";
      updateButtons(); toast("Subtitles " + (v ? "on" : "off"));
    }
    btn("cc").addEventListener("click", function () { setSubs(!S.subs); });
    var ro = new ResizeObserver(function () { frame(true); });
    ro.observe(root);

    function updateButtons() {
      btn("play").innerHTML = svg(video.ended ? "replay" : video.paused ? "play" : "pause");
      btn("play").dataset.tip = video.paused ? "Play (k)" : "Pause (k)";
      btn("next").innerHTML = svg("next"); btn("next").hidden = !next; btn("next").dataset.tip = "Next (Shift+N)";
      btn("cc").innerHTML = svg("cc"); btn("cc").hidden = !video.textTracks.length;
      btn("cc").classList.toggle("fzp-on", S.subs); btn("cc").dataset.tip = "Subtitles (c)";
      btn("gear").innerHTML = svg("gear"); btn("gear").dataset.tip = "Settings";
      btn("theater").innerHTML = svg(theater ? "theaterOff" : "theater");
      btn("theater").dataset.tip = theater ? "Default view (t)" : "Theater mode (t)";
      if (opts.onTheater) btn("theater").hidden = isFs();
      btn("fs").innerHTML = svg(isFs() ? "fsExit" : "fs");
      btn("fs").dataset.tip = isFs() ? "Exit full screen (f)" : "Full screen (f)";
    }

    // ---------------------------------------------------------------- settings menu
    var gear = btn("gear");
    function closeMenu() { menu.hidden = true; wake(); }
    function openMenu() { renderMenu("main"); menu.hidden = false; ctip.classList.remove("fzp-show"); wake(true); }
    gear.addEventListener("click", function (e) { e.stopPropagation(); if (!menu.hidden) closeMenu(); else openMenu(); });
    document.addEventListener("mousedown", function (e) {
      if (!menu.hidden && !menu.contains(e.target) && !gear.contains(e.target)) closeMenu();
    });
    ["click", "mousedown", "dblclick"].forEach(function (t) { menu.addEventListener(t, function (e) { e.stopPropagation(); }); });
    function renderMenu(page) {
      menuPage = page;
      var h = "";
      if (page === "main") {
        if (opts.menu) h += opts.menu("main");
        if (video.textTracks.length) h += '<div class="fzp-mi fzp-check" data-act="fzp-subs"><span class="fzp-mic">💬</span><span class="fzp-mil">Subtitles</span>' + sw(S.subs) + "</div>";
        h += '<div class="fzp-mi" data-act="fzp-page" data-p="speed"><span class="fzp-mic">🐢</span><span class="fzp-mil">Playback speed</span><span class="fzp-miv">' + (S.speed === 1 ? "Normal" : S.speed) + "</span></div>";
      } else if (page === "speed") {
        h += '<div class="fzp-mi fzp-back" data-act="fzp-page" data-p="main"><span class="fzp-mil">Playback speed</span></div>';
        SPEEDS.forEach(function (v) {
          h += '<div class="fzp-mi' + (v === S.speed ? " fzp-sel" : "") + '" data-act="fzp-speed" data-v="' + v + '"><span class="fzp-mic"></span><span class="fzp-mil">' + (v === 1 ? "Normal" : v) + "</span></div>";
        });
      }
      menu.innerHTML = h;
    }
    menu.addEventListener("click", function (e) {
      var el = e.target.closest("[data-act]");
      if (!el) return;
      var a = el.dataset.act;
      if (a === "fzp-subs") setSubs(!S.subs);
      else if (a === "fzp-page") { renderMenu(el.dataset.p); return; }
      else if (a === "fzp-speed") { setSpeed(Number(el.dataset.v)); renderMenu("main"); return; }
      else if (opts.onMenu) opts.onMenu(a, el);
      renderMenu(menuPage);
    });

    // ---------------------------------------------------------------- keyboard
    function onKey(e) {
      if (!loaded || !root.isConnected || root.offsetParent === null && !isFs()) return;
      var tag = (e.target.tagName || "").toLowerCase();
      if (tag === "input" || tag === "textarea" || tag === "select" || e.target.isContentEditable || e.ctrlKey || e.metaKey || e.altKey) return;
      var k = e.key, done = true;
      if (k === "Escape") { if (!menu.hidden) closeMenu(); else done = false; }
      else if (e.shiftKey && k === "N") { if (next) next(); }
      else if (k === ">") stepSpeed(1);
      else if (k === "<") stepSpeed(-1);
      else if (k === " " || k === "k") togglePlay();
      else if (k === "j") seekBy(-10);
      else if (k === "l") seekBy(10);
      else if (k === "ArrowLeft") seekBy(-5);
      else if (k === "ArrowRight") seekBy(5);
      else if (k === "ArrowUp") setVolume(S.volume + 0.05, true);
      else if (k === "ArrowDown") setVolume(S.volume - 0.05, true);
      else if (k === "m") toggleMute();
      else if (k === "f") toggleFullscreen();
      else if (k === "t" && opts.onTheater) setTheater(!theater);
      else if (k === "c") { if (video.textTracks.length) setSubs(!S.subs); }
      else if (k === "Home") video.currentTime = 0;
      else if (k === "End") video.currentTime = video.duration || 0;
      else if (k === "," && video.paused) video.currentTime = Math.max(0, video.currentTime - 1 / 30);
      else if (k === "." && video.paused) video.currentTime += 1 / 30;
      else if (/^[0-9]$/.test(k)) video.currentTime = (video.duration || 0) * Number(k) / 10;
      else done = false;
      if (done) { e.preventDefault(); wake(); if (!menu.hidden) renderMenu(menuPage); }
    }
    if (opts.keys !== false) document.addEventListener("keydown", onKey);

    // ---------------------------------------------------------------- load / unload
    function load(src, o) {
      o = o || {};
      unload();
      loaded = true;
      resumeAt = o.resume || 0;
      ytState = -1;
      video.src = src;
      if (o.subs) {
        var tr = document.createElement("track");
        tr.kind = "subtitles"; tr.srclang = "ja"; tr.label = "日本語"; tr.src = o.subs;
        tr.addEventListener("load", function () { tr.track.mode = S.subs ? "showing" : "hidden"; updateButtons(); });
        video.appendChild(tr);
        tr.track.mode = S.subs ? "showing" : "hidden";
      }
      updateButtons();
      if (o.autoplay !== false) { var pp = video.play(); if (pp && pp.catch) pp.catch(function () {}); }
    }
    function unload() {
      loaded = false;
      video.pause();
      video.removeAttribute("src");
      Array.prototype.slice.call(video.querySelectorAll("track")).forEach(function (t) { t.remove(); });
      video.load();
      q(".fzp-dur").textContent = "0:00"; updateProgress();
      closeMenu();
    }
    function destroy() {
      unload();
      ro.disconnect();
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("fullscreenchange", onFs);
      root.remove();
    }

    applyVolume();
    updateButtons();

    var api = {
      root: root, video: video, layer: layer, fmt: fmt,
      load: load, unload: unload, destroy: destroy, toast: toast, bezel: bezel,
      redraw: function () { if (!menu.hidden) renderMenu(menuPage); },
      frame: frame, contentRect: contentRect, extra: q(".fzp-extra"),
      setNext: function (fn) { next = fn || null; updateButtons(); },
      setTheater: function (v) { theater = !!v; updateButtons(); },
      togglePlay: togglePlay, seekBy: seekBy, toggleFullscreen: toggleFullscreen, wake: wake,
      // the YouTube IFrame API subset Token Coverage and primed listening call
      yt: {
        getPlayerState: function () { return ytState; },
        getCurrentTime: function () { return video.currentTime || 0; },
        getDuration: function () { return video.duration || 0; },
        getPlaybackRate: function () { return video.playbackRate || 1; },
        seekTo: function (t) { video.currentTime = Math.max(0, t); },
        playVideo: function () { var pp = video.play(); if (pp && pp.catch) pp.catch(function () {}); },
        pauseVideo: function () { video.pause(); },
        isMuted: function () { return video.muted || video.volume === 0; },
        mute: function () { if (!S.muted) toggleMute(); },
        unMute: function () { if (S.muted) toggleMute(); },
        getIframe: function () { return null; },
        destroy: function () { destroy(); },
      },
    };
    return api;
  }

  function sw(on, dim) { return '<span class="fzp-sw' + (on ? " fzp-on" : "") + (dim ? " fzp-dim" : "") + '"></span>'; }
  window.FzPlayer = { create: create, fmt: fmt, esc: esc, sw: sw };
})();
