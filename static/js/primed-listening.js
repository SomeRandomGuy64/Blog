// Primed Listening for the Token Coverage page's YouTube player — a port of the
// youtube-primed-listening extension (itself a port of the mpv primed-listening.lua script),
// driven through the YouTube IFrame API instead of a <video> element.
//
// When a subtitle line starts, the video pauses for max(0.5 s, visible characters × pause-per-char)
// so you can read ("prime") the sentence, then resumes on its own so you hear it straight after.
// While the script holds that pause, Space / P / K / a click on the video: 1st press holds it
// (cancels the auto-resume), 2nd press resumes. A manual pause enters the same stand-by. The
// subtitle overlay is only shown while paused.
//
// We can't reach inside YouTube's iframe, so: a transparent, focusable "catch layer" sits over the
// video (its middle band — YouTube's top bar and bottom controls stay usable) and takes clicks and
// keys; play/pause state comes from the player's onStateChange events, which arrive asynchronously
// (postMessage), hence the bookkeeping around pausedByScript / pauseAskedAt below.
//
// Usage (token-coverage.js): var pl = PrimedListening({ppc, onPpc, onOffset, onLine});
// pl.setCues(cues) · pl.attach(ytPlayer) on ready · pl.state(e.data) from onStateChange ·
// pl.detach() before the player is destroyed · pl.offset() (s added to cue times).
(function () {
  "use strict";

  var MIN_PAUSE = 0.5;     // never pause for less than this (s)
  var MIN_CHARS = 2;       // ignore lines shorter than this
  var MIN_PPC = 0.005;     // floor for pause per char
  var PPC_STEP = 0.005;
  var OFFSET_STEP = 0.25;
  var SEEK_STEP = 5;
  var HOLD_MSG = "待機 — press again to resume";
  var $ = function (id) { return document.getElementById(id); };

  // Port of visible_char_count(): strip ASS tags, remove (...) / （...）, drop whitespace, count code points.
  function visibleCharCount(text) {
    if (!text) return 0;
    var t = text
      .replace(/\{[^}]*\}/g, "")     // {ASS override tags}
      .replace(/\\[Nn]/g, "")         // \N \n line breaks
      .replace(/\([^)]*\)/g, "")      // (...)
      .replace(/（[^）]*）/g, "")       // （...）
      .replace(/\s+/g, "");
    return Array.from(t).length;
  }
  // for display: strip ASS braces + html-ish tags, keep line breaks
  function renderableLines(text) {
    if (!text) return [];
    return text.replace(/\{[^}]*\}/g, "").replace(/<[^>]+>/g, "").replace(/\\[Nn]/g, "\n").split(/\r?\n/);
  }
  function isTyping(n) {
    if (!n) return false;
    var tag = (n.tagName || "").toLowerCase();
    return n.isContentEditable || tag === "input" || tag === "textarea" || tag === "select";
  }
  function fsElement() { return document.fullscreenElement || document.webkitFullscreenElement || null; }

  window.PrimedListening = function (o) {
    var wrap = $("tcVideo"), catcher = $("tcPlCatch"), overlay = $("tcPlOverlay"), osdEl = $("tcPlOsd");
    var onBox = $("tcPlOn"), ppcIn = $("tcPpc"), offIn = $("tcOff"), fsBtn = $("tcFs");

    var ppc = isFinite(o.ppc) ? Math.max(MIN_PPC, +o.ppc) : 0.06;
    var offset = 0;              // seconds added to cue times (sync)
    var cues = [];               // [{s, e, text}] sorted by start
    var player = null;
    var enabled = false;
    var playing = false;         // our view of the player: set from state events, and optimistically when we act
    var cur = -1;                // cue index under the playhead (-1 between lines, -2 = force re-evaluation)
    var resumeTimer = null;      // auto-resume timeout while the script holds a pause
    var awaitingSecondPress = false;
    var pausedByScript = false;
    var pauseAskedAt = 0;        // when we last asked for / saw a pause (stale PLAYING events can still arrive)
    var lastTickT = 0, runTicks = 0;   // playhead at the last tick; consecutive ticks it moved forward
    var loopRaf = null, loopTimer = null, osdTimer = null, refocusTimer = null;
    var lineShown = true;
    var ro = null;

    // ------------------------------------------------------------ OSD + overlay
    function osd(msg, ms) {
      osdEl.textContent = msg;
      osdEl.classList.add("tc-pl-show");
      if (osdTimer) clearTimeout(osdTimer);
      osdTimer = setTimeout(function () { osdEl.classList.remove("tc-pl-show"); }, ms || 1200);
    }
    function renderOverlay(text) {
      overlay.textContent = "";
      renderableLines(text).forEach(function (line) {
        var sp = document.createElement("span"); sp.className = "tc-pl-l"; sp.textContent = line; overlay.appendChild(sp);
      });
    }
    function sizeOverlay() {   // scale the text to the player height for readability
      overlay.style.fontSize = Math.max(16, Math.round((wrap.clientHeight || 360) * 0.045)) + "px";
    }
    // subs are visible ONLY while paused (mpv's sub-visibility = paused); the page's own current-line
    // readout follows the same rule while we're on, so it doesn't give the line away early
    function refresh() {
      overlay.hidden = !(enabled && player && !playing && overlay.textContent);
      var show = !enabled || !player || !playing;
      if (show !== lineShown) { lineShown = show; if (o.onLine) o.onLine(show); }
    }

    // ------------------------------------------------------------ core pause logic
    function primePause(chars) {
      if (!player || chars < MIN_CHARS) return;
      var duration = Math.max(MIN_PAUSE, chars * ppc);
      pausedByScript = true; playing = false; pauseAskedAt = performance.now();
      player.pauseVideo();
      if (resumeTimer) clearTimeout(resumeTimer);
      resumeTimer = setTimeout(resumePlayback, duration * 1000);
    }
    function resumePlayback() {
      if (resumeTimer) { clearTimeout(resumeTimer); resumeTimer = null; }
      awaitingSecondPress = false;
      if (player) { pausedByScript = true; playing = true; player.playVideo(); }
      refresh();
    }
    function lockPause() {
      if (resumeTimer) { clearTimeout(resumeTimer); resumeTimer = null; }
      awaitingSecondPress = true;
      osd(HOLD_MSG);
    }
    function releaseControl() {
      if (resumeTimer) { clearTimeout(resumeTimer); resumeTimer = null; }
      awaitingSecondPress = false;
    }
    function holding() { return enabled && (awaitingSecondPress || !!resumeTimer); }
    function togglePlay() {
      if (!player) return;
      if (playing) { pausedByScript = false; pauseAskedAt = performance.now(); player.pauseVideo(); }   // manual pause → stand-by
      else player.playVideo();
    }
    // Space / P / K / click: two-press hold → resume while we hold the pause, plain play/pause otherwise
    function press() {
      if (!holding()) { togglePlay(); return; }
      if (awaitingSecondPress) resumePlayback(); else lockPause();
    }

    // ------------------------------------------------------------ cue engine
    function findCueIndex(t) {
      var lo = 0, hi = cues.length - 1, cand = -1;
      while (lo <= hi) {
        var mid = (lo + hi) >> 1;
        if (cues[mid].s + offset <= t) { cand = mid; lo = mid + 1; }
        else hi = mid - 1;
      }
      if (cand >= 0 && t < cues[cand].e + offset) return cand;
      return -1;
    }
    function cueAt(t) {
      var i = findCueIndex(t);
      // the paused position can land a hair before the start we detected (the API extrapolates the
      // time between updates) — stay on the current line rather than re-trigger it on resume
      if (cur >= 0 && i !== cur && cues[cur] && t < cues[cur].s + offset && t > cues[cur].s + offset - 0.4) return cur;
      return i;
    }
    function tick() {
      loopRaf = loopTimer = null;
      if (!enabled || !player || !player.getCurrentTime) return;
      var t = player.getCurrentTime() || 0, dt = t - lastTickT;
      lastTickT = t;
      // YouTube keeps reporting BUFFERING for ~¼ s after playback has really started, so the PLAYING
      // event can trail the audio — a clock that has kept running for a few frames (and isn't the
      // tail of a pause we just asked for) counts as playing now, like a <video> 'play' event would
      runTicks = dt > 0.001 && dt < 0.5 ? runTicks + 1 : 0;
      if (!playing && !resumeTimer && runTicks >= 3 && performance.now() - pauseAskedAt > 600) markPlaying();
      var idx = cueAt(t);
      if (idx !== cur) {
        cur = idx;
        renderOverlay(idx >= 0 ? cues[idx].text : "");
        // only prime while playing (or while re-priming during our own pause) — never start a stopped video
        if (idx >= 0 && !awaitingSecondPress && (playing || resumeTimer)) primePause(visibleCharCount(cues[idx].text));
        refresh();
      }
      schedule();
    }
    // requestAnimationFrame normally; it stalls in a hidden tab, so fall back to a timer there
    function schedule() {
      if (loopRaf != null || loopTimer != null) return;
      if (document.hidden) loopTimer = setTimeout(tick, 50);
      else loopRaf = requestAnimationFrame(tick);
    }
    function stopLoop() {
      if (loopRaf != null) { cancelAnimationFrame(loopRaf); loopRaf = null; }
      if (loopTimer != null) { clearTimeout(loopTimer); loopTimer = null; }
    }
    function startEngine() {
      stopLoop();
      cur = -1;
      cur = player && player.getCurrentTime ? cueAt(player.getCurrentTime() || 0) : -1;
      renderOverlay(cur >= 0 ? cues[cur].text : "");
      schedule();
      refresh();
    }
    function stopEngine() {
      stopLoop();
      renderOverlay("");
      refresh();
    }

    function setEnabled(state) {
      if (state === enabled) return;
      enabled = state;
      releaseControl();
      if (enabled) {
        startEngine();
        if (player && !playing) awaitingSecondPress = true;   // already paused when enabling → stand-by
        osd("Primed listening ON");
      } else {
        if (pausedByScript && !playing && player) { pausedByScript = true; playing = true; player.playVideo(); }
        stopEngine();
        osd("Primed listening OFF");
      }
      updatePanel();
    }

    // ------------------------------------------------------------ player state (async)
    function markPlaying() {
      pausedByScript = false; playing = true;
      if (enabled) releaseControl();
      refresh();
    }
    function onState(s) {
      if (!player) return;
      if (s === 1) {                        // PLAYING
        // a PLAYING event already in flight when we asked for our pause (e.g. a seek finishing) — ignore
        if (pausedByScript && resumeTimer && performance.now() - pauseAskedAt < 600) return;
        markPlaying();
      } else if (s === 2) {                 // PAUSED
        playing = false; runTicks = 0; pauseAskedAt = performance.now();
        if (enabled && !pausedByScript && !awaitingSecondPress) {   // manual pause → stand-by
          awaitingSecondPress = true;
          osd(HOLD_MSG);
        }
      } else if (s === -1 || s === 5) {     // unstarted / new video cued
        releaseControl(); pausedByScript = false; playing = false; cur = -2;
        if (enabled) awaitingSecondPress = true;
      } else if (s === 0) {                 // ended → stand-by, so seeking back doesn't auto-start it
        releaseControl(); playing = false;
        if (enabled) awaitingSecondPress = true;
      }                                     // 3 buffering and others: no change
      refresh();
    }

    // ------------------------------------------------------------ offset / ppc
    function setPpc(v) {
      ppc = Math.max(MIN_PPC, +(+v).toFixed(4));
      osd("pause per char = " + ppc.toFixed(3) + " s");
      if (o.onPpc) o.onPpc(ppc);
      updatePanel();
    }
    function setOffset(v, quiet) {
      offset = +(+v).toFixed(3);
      cur = -2;   // force re-evaluation on the next tick
      if (!quiet) osd("subtitle offset = " + (offset > 0 ? "+" : "") + offset.toFixed(2) + " s");
      if (o.onOffset) o.onOffset(offset);
      updatePanel();
    }

    // ------------------------------------------------------------ fullscreen
    function toggleFullscreen() {
      if (fsElement() === wrap) {
        var x = document.exitFullscreen || document.webkitExitFullscreen;
        if (x) x.call(document);
        return;
      }
      var r = wrap.requestFullscreen || wrap.webkitRequestFullscreen;
      if (!r) return;
      var p = r.call(wrap);
      if (p && p.catch) p.catch(function () { /* refused */ });
      catcher.focus({ preventScroll: true });
    }

    // ------------------------------------------------------------ input
    function onKey(e) {
      if (!player || isTyping(e.target) || e.ctrlKey || e.metaKey) return;
      var a = document.activeElement;
      if (fsElement() !== wrap && !(a && wrap.contains(a))) return;   // only while the video has focus
      var k = e.code, done = true;
      if (e.altKey) {
        if (k === "ArrowRight") setOffset(offset + OFFSET_STEP);
        else if (k === "ArrowLeft") setOffset(offset - OFFSET_STEP);
        else done = false;
      } else if (k === "Space" || k === "KeyK" || k === "KeyP") { if (!e.repeat) press(); }
      else if (k === "KeyN") { if (!e.repeat) setEnabled(!enabled); }
      else if (k === "KeyF") { if (!e.repeat) toggleFullscreen(); }
      else if (k === "KeyM") { if (!e.repeat) { if (player.isMuted()) player.unMute(); else player.mute(); } }
      else if (k === "ArrowRight" || k === "ArrowLeft") {
        var t = (player.getCurrentTime() || 0) + (k === "ArrowRight" ? SEEK_STEP : -SEEK_STEP);
        player.seekTo(Math.max(0, t), true);
      } else done = false;
      if (done) { e.preventDefault(); e.stopPropagation(); }
    }
    function onCatchClick(e) { e.preventDefault(); catcher.focus({ preventScroll: true }); press(); }
    // clicking YouTube's own controls moves focus into its iframe, where our keys can't follow —
    // take it back straight after so the shortcuts keep working. Its settings sheet opens over the
    // middle of the video, under the catch layer, so let clicks pass through to YouTube until the
    // pointer leaves the video
    function onWinBlur() {
      if (refocusTimer) clearTimeout(refocusTimer);
      refocusTimer = setTimeout(function () {
        refocusTimer = null;
        var f = player && player.getIframe ? player.getIframe() : null;
        if (!f || document.activeElement !== f) return;
        catcher.classList.add("tc-pl-pass");
        catcher.focus({ preventScroll: true });
      }, 60);
    }
    function onLeave() { catcher.classList.remove("tc-pl-pass"); }
    function onVisibility() { if (enabled && player) { stopLoop(); schedule(); } }

    // ------------------------------------------------------------ panel
    function updatePanel() {
      onBox.checked = enabled;
      // don't clobber a value the user is mid-way through typing
      if (document.activeElement !== ppcIn) ppcIn.value = ppc.toFixed(3);
      if (document.activeElement !== offIn) offIn.value = offset.toFixed(2);
    }
    // typed values: commit on Enter / blur / spinner, Esc reverts; keys typed here never reach shortcuts
    function bindNumber(input, commit) {
      var before = "";
      input.addEventListener("change", function () {
        var v = parseFloat(input.value);
        if (isFinite(v)) commit(v); else updatePanel();
      });
      input.addEventListener("focus", function () { before = input.value; });
      input.addEventListener("blur", function () { setTimeout(updatePanel, 0); });   // tidy the shown value
      input.addEventListener("keydown", function (e) {
        if (e.key === "Enter") input.blur();
        // restore first so the blur doesn't fire a "change" with the half-typed value
        else if (e.key === "Escape") { input.value = before; input.blur(); }
      });
      ["keydown", "keyup", "keypress"].forEach(function (type) {
        input.addEventListener(type, function (e) { e.stopPropagation(); });
      });
    }
    onBox.addEventListener("change", function () { setEnabled(onBox.checked); });
    $("tcPpcDn").addEventListener("click", function () { setPpc(ppc - PPC_STEP); });
    $("tcPpcUp").addEventListener("click", function () { setPpc(ppc + PPC_STEP); });
    $("tcOffDn").addEventListener("click", function () { setOffset(offset - OFFSET_STEP); });
    $("tcOffUp").addEventListener("click", function () { setOffset(offset + OFFSET_STEP); });
    bindNumber(ppcIn, setPpc);
    bindNumber(offIn, setOffset);
    if (wrap.requestFullscreen || wrap.webkitRequestFullscreen) fsBtn.addEventListener("click", toggleFullscreen);
    else fsBtn.hidden = true;   // iPhone Safari: no element fullscreen
    updatePanel();

    // ------------------------------------------------------------ attach / detach
    function attach(p) {
      if (player === p) return;
      if (player) detach();
      player = p;
      var st = p.getPlayerState ? p.getPlayerState() : -1;
      playing = st === 1 || st === 3;
      pausedByScript = false; awaitingSecondPress = false; cur = -2;
      catcher.addEventListener("click", onCatchClick);
      document.addEventListener("keydown", onKey);
      window.addEventListener("blur", onWinBlur);
      wrap.addEventListener("mouseleave", onLeave);
      document.addEventListener("visibilitychange", onVisibility);
      ro = new ResizeObserver(sizeOverlay); ro.observe(wrap);
      sizeOverlay();
      if (enabled) { startEngine(); if (!playing) awaitingSecondPress = true; }
      refresh();
    }
    function detach() {
      stopLoop(); releaseControl();
      [osdTimer, refocusTimer].forEach(function (t) { if (t) clearTimeout(t); });
      osdTimer = refocusTimer = null; osdEl.classList.remove("tc-pl-show");
      catcher.removeEventListener("click", onCatchClick);
      document.removeEventListener("keydown", onKey);
      window.removeEventListener("blur", onWinBlur);
      wrap.removeEventListener("mouseleave", onLeave);
      document.removeEventListener("visibilitychange", onVisibility);
      onLeave();
      if (ro) { ro.disconnect(); ro = null; }
      if (fsElement() === wrap) toggleFullscreen();
      player = null; playing = false; pausedByScript = false; cur = -1;
      renderOverlay("");
      refresh();
    }

    return {
      attach: attach,
      detach: detach,
      state: onState,
      setCues: function (c) { cues = c || []; cur = -2; setOffset(0, true); if (enabled && player) startEngine(); },
      offset: function () { return offset; }
    };
  };
})();
