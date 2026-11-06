// In-page half of the HCI suite: injected into each page by run.ts and called as __hci.<name>().
// Everything here runs in the browser, so it must stay plain JS with no imports.
(() => {
  if (window.__hci) return;
  const cs = (el) => getComputedStyle(el);
  const TARGETS = 'a[href], button, input:not([type=hidden]), select, textarea, summary, [role=button], [role=link], [role=tab], [role=checkbox], [role=switch], [tabindex]:not([tabindex="-1"])';

  function visible(el) {
    if (!el.isConnected) return false;
    const r = el.getBoundingClientRect();
    if (r.width < 1 || r.height < 1) return false;
    for (let n = el; n && n.nodeType === 1; n = n.parentElement) {
      const s = cs(n);
      if (s.display === 'none' || s.visibility === 'hidden' || s.visibility === 'collapse' || +s.opacity === 0) return false;
      if (n.hidden || n.getAttribute('aria-hidden') === 'true' && n !== el) return false;
    }
    // visually-hidden pattern (1px clip) counts as invisible
    return !(r.width <= 1 && r.height <= 1);
  }

  // short, readable selector: up to three ancestors, id or first two classes each
  function sel(el) {
    const parts = [];
    for (let n = el, i = 0; n && n.nodeType === 1 && i < 4; n = n.parentElement, i++) {
      let s = n.tagName.toLowerCase();
      if (n.id) { parts.unshift(s + '#' + n.id); break; }
      const cl = [...n.classList].slice(0, 2);
      if (cl.length) s += '.' + cl.join('.');
      parts.unshift(s);
    }
    return parts.join(' > ');
  }
  const label = (el) => (el.getAttribute('aria-label') || el.textContent || el.getAttribute('title') || el.value || '').trim().replace(/\s+/g, ' ').slice(0, 40);

  // WCAG 2.5.8: an inline link inside a sentence is exempt
  function inlineInText(el) {
    if (el.tagName !== 'A' || cs(el).display !== 'inline') return false;
    const block = el.parentElement && el.parentElement.closest('p, li, td, dd, figcaption, blockquote, label');
    return !!block && block.textContent.trim().length > el.textContent.trim().length + 15;
  }

  // the rect a pointer can hit: an input also takes clicks on its <label>s
  function hitRect(el) {
    let r = el.getBoundingClientRect(), L = r.left, T = r.top, R = r.right, B = r.bottom;
    for (const lab of el.labels || []) {
      if (!visible(lab)) continue;
      const q = lab.getBoundingClientRect();
      L = Math.min(L, q.left); T = Math.min(T, q.top); R = Math.max(R, q.right); B = Math.max(B, q.bottom);
    }
    return { left: L, top: T, right: R, bottom: B, width: R - L, height: B - T };
  }

  function targets(min = 24) {
    const all = [...document.querySelectorAll(TARGETS)].filter((el) => visible(el) && !el.disabled);
    // a target nested in another (button inside a link) is one target
    const els = all.filter((el) => !all.some((o) => o !== el && o.contains(el)));
    const boxes = els.map((el) => ({ el, r: hitRect(el) }));
    const small = boxes.filter((b) => (b.r.width < min - 0.5 || b.r.height < min - 0.5) && !inlineInText(b.el));
    const centre = (r) => [r.left + r.width / 2, r.top + r.height / 2];
    const circleHitsRect = ([x, y], r, rad) => {
      const dx = Math.max(r.left - x, 0, x - r.right), dy = Math.max(r.top - y, 0, y - r.bottom);
      return dx * dx + dy * dy < rad * rad;
    };
    const out = [];
    for (const s of small) {
      const c = centre(s.r);
      // spacing exception: a 24px circle on its centre touches no other target or other small target's circle
      const crowded = boxes.some((o) => o !== s && (
        circleHitsRect(c, o.r, min / 2) ||
        (small.includes(o) && Math.hypot(c[0] - centre(o.r)[0], c[1] - centre(o.r)[1]) < min)));
      if (crowded) out.push({ sel: sel(s.el), label: label(s.el), w: Math.round(s.r.width), h: Math.round(s.r.height) });
    }
    return out;
  }

  // WCAG 1.4.10: the page itself must not scroll sideways; content inside its own scroller is fine
  function overflow() {
    const vw = document.documentElement.clientWidth, sw = document.documentElement.scrollWidth;
    const off = [];
    if (sw > vw + 1) {
      for (const el of document.body.querySelectorAll('*')) {
        if (!visible(el)) continue;
        const r = el.getBoundingClientRect();
        if (r.right <= vw + 1) continue;
        let scroller = false;
        for (let n = el.parentElement; n && n !== document.body; n = n.parentElement) {
          const ox = cs(n).overflowX;
          if (ox === 'auto' || ox === 'scroll' || ox === 'hidden' || ox === 'clip') { scroller = true; break; }
        }
        if (scroller) continue;
        // report the outermost offender only
        if (el.parentElement && el.parentElement.getBoundingClientRect().right > vw + 1 && el.parentElement !== document.body) continue;
        off.push({ sel: sel(el), right: Math.round(r.right) });
        if (off.length >= 5) break;
      }
    }
    return { vw, sw, off };
  }

  // reading text: body paragraphs in <main> with enough text to read as prose
  function typography() {
    const main = document.querySelector('main') || document.body;
    const ps = [...main.querySelectorAll('p, li')].filter((p) => visible(p) && p.textContent.trim().length > 120 && !p.closest('nav, header, footer, .w-bar, table, pre'));
    return ps.slice(0, 40).map((p) => {
      const s = cs(p), fs = parseFloat(s.fontSize);
      const lh = s.lineHeight === 'normal' ? 1.2 : parseFloat(s.lineHeight) / fs;
      const probe = document.createElement('span');
      probe.style.cssText = 'position:absolute;visibility:hidden;white-space:pre;font:inherit;letter-spacing:inherit';
      probe.textContent = 'abcdefghijklmnopqrstuvwxyz ABCDEFGHIJKLMNOPQRSTUVWXYZ 0123456789';
      p.append(probe);
      const charW = probe.getBoundingClientRect().width / probe.textContent.length;
      probe.remove();
      const pad = parseFloat(s.paddingLeft) + parseFloat(s.paddingRight);
      return { sel: sel(p), fs, lh: +lh.toFixed(2), cpl: Math.round((p.clientWidth - pad) / charW), width: Math.round(p.clientWidth), family: s.fontFamily.split(',')[0].replace(/["']/g, '') };
    });
  }

  // WCAG 2.1.1 heuristic: looks clickable (pointer cursor) but keyboard can't reach it
  function pointerOnly() {
    const out = [];
    for (const el of document.body.querySelectorAll('*')) {
      if (cs(el).cursor !== 'pointer' || !visible(el)) continue;
      if (el.matches(TARGETS) || el.closest(TARGETS) || el.closest('label') || el.tagName === 'LABEL' && el.htmlFor) continue;
      if (el.parentElement && cs(el.parentElement).cursor === 'pointer' && !el.parentElement.matches(TARGETS)) continue;   // inherited from a parent we already report
      out.push({ sel: sel(el), label: label(el) || el.tagName.toLowerCase() });
    }
    return out.slice(0, 10);
  }

  function nav() {
    return [...document.querySelectorAll('.site-nav a')].filter((a) => !a.hidden).map((a) => ({ text: a.textContent.trim(), href: a.getAttribute('href'), current: a.getAttribute('aria-current') }));
  }

  const VAGUE = /^(click here|click|here|read more|more|link|this|this link|go|details|learn more)$/i;
  function links() {
    const vague = [], byText = new Map();
    for (const a of document.querySelectorAll('a[href]')) {
      if (!visible(a)) continue;
      const name = (a.getAttribute('aria-label') || a.textContent).trim().replace(/\s+/g, ' ');
      if (VAGUE.test(name)) vague.push({ sel: sel(a), text: name });
      if (!name) continue;
      const set = byText.get(name) || new Set();
      set.add(new URL(a.href, location.href).href.replace(/#.*$/, ''));
      byText.set(name, set);
    }
    const ambiguous = [...byText].filter(([, s]) => s.size > 1).map(([t, s]) => ({ text: t, targets: s.size }));
    return { vague, ambiguous };
  }

  function headings() {
    const hs = [...document.querySelectorAll('h1, h2, h3, h4, h5, h6')].filter(visible).map((h) => ({ level: +h.tagName[1], text: h.textContent.trim().slice(0, 50), sel: sel(h) }));
    const skips = [];
    for (let i = 1; i < hs.length; i++) if (hs[i].level > hs[i - 1].level + 1) skips.push(`h${hs[i - 1].level} "${hs[i - 1].text}" → h${hs[i].level} "${hs[i].text}"`);
    return { h1: hs.filter((h) => h.level === 1).map((h) => h.text), skips };
  }

  function animations() {
    return document.getAnimations().filter((a) => a.playState === 'running').map((a) => {
      const t = a.effect && a.effect.getTiming ? a.effect.getTiming() : {};
      return { sel: a.effect && a.effect.target ? sel(a.effect.target) : '?', name: a.animationName || a.transitionProperty || 'animation', duration: t.duration, iterations: t.iterations };
    }).slice(0, 10);
  }

  // WCAG 1.4.12: override spacing and look for text that newly gets clipped
  function clipped() {
    const out = new Set();
    for (const el of document.body.querySelectorAll('*')) {
      if (!visible(el) || !el.childNodes.length) continue;
      const s = cs(el);
      const clipX = s.overflowX !== 'visible', clipY = s.overflowY !== 'visible';
      if (!clipX && !clipY) continue;
      if (s.overflowX === 'auto' || s.overflowX === 'scroll' || s.overflowY === 'auto' || s.overflowY === 'scroll') continue;
      if ((clipX && el.scrollWidth > el.clientWidth + 2) || (clipY && el.scrollHeight > el.clientHeight + 2)) out.add(el);
    }
    return out;
  }
  function textSpacing() {
    const before = clipped();
    const st = document.createElement('style');
    st.textContent = '* { line-height: 1.5 !important; letter-spacing: 0.12em !important; word-spacing: 0.16em !important; } p { margin-bottom: 2em !important; }';
    document.head.append(st);
    const after = clipped();
    st.remove();
    return [...after].filter((el) => !before.has(el) && el.textContent.trim()).slice(0, 8).map((el) => ({ sel: sel(el), text: el.textContent.trim().slice(0, 40) }));
  }

  // 2.4.7 / 2.4.11 for whatever has focus now
  const FOCUS_PROPS = ['outlineStyle', 'outlineWidth', 'outlineColor', 'boxShadow', 'backgroundColor', 'borderTopColor', 'borderBottomColor', 'color', 'textDecorationLine', 'filter', 'transform'];
  function snap(el) {
    const s = cs(el), o = {};
    for (const p of FOCUS_PROPS) o[p] = s[p];
    if (o.outlineStyle === 'none' || parseFloat(o.outlineWidth) === 0) { o.outlineStyle = 'none'; o.outlineWidth = '0px'; o.outlineColor = ''; }
    // a focus ring drawn on a wrapper via :focus-within counts too
    const p = el.parentElement;
    if (p) { const ps = cs(p); o.parent = ps.outlineStyle + ps.boxShadow + ps.backgroundColor + ps.borderTopColor; }
    return JSON.stringify(o);
  }
  function focusState() {
    const el = document.activeElement;
    if (!el || el === document.body || el === document.documentElement) return { none: true };
    const id = el.__hciId || (el.__hciId = Math.random().toString(36).slice(2));
    const focused = snap(el);
    el.blur();
    const blurred = snap(el);
    el.focus({ preventScroll: true });
    const r = el.getBoundingClientRect();
    const vw = innerWidth, vh = innerHeight;
    const onScreen = r.bottom > 0 && r.right > 0 && r.top < vh && r.left < vw && r.width > 0 && r.height > 0;
    // fully covered by a fixed/sticky box (a page bar, a header)?
    let covered = false, by = '';
    if (onScreen) {
      const pts = [[0.5, 0.5], [0.15, 0.2], [0.85, 0.2], [0.15, 0.8], [0.85, 0.8]].map(([fx, fy]) => [r.left + r.width * fx, r.top + r.height * fy]).filter(([x, y]) => x >= 0 && y >= 0 && x < vw && y < vh);
      covered = pts.length > 0 && pts.every(([x, y]) => {
        const hit = document.elementFromPoint(x, y);
        if (!hit || hit === el || el.contains(hit) || hit.contains(el)) return false;
        for (let n = hit; n && n.nodeType === 1; n = n.parentElement) {
          const pos = cs(n).position;
          if (pos === 'fixed' || pos === 'sticky') { by = sel(n); return !n.contains(el); }
        }
        return false;
      });
    }
    return { id, sel: sel(el), label: label(el), indicator: focused !== blurred, onScreen, covered, by };
  }

  function layoutState() {
    const pressed = document.querySelector('.fz-layout-seg button[aria-pressed="true"]');
    const seg = document.querySelector('.fz-layout-seg');
    return { wide: document.documentElement.classList.contains('layout-wide'), pressed: pressed ? pressed.dataset.layout : null, segVisible: !!seg && visible(seg), light: document.body.classList.contains('light-mode') };
  }

  window.__hci = { targets, overflow, typography, pointerOnly, nav, links, headings, animations, textSpacing, focusState, layoutState };
})();
