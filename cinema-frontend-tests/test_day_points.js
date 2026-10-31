// Cinema Planner: a different start and end point per day. Solver checks (exact search, brute force, capacity) and planner.js
// on a stub page with fake postcodes.io and Routes APIs (no real key, API or network).
// Run:  deno run --allow-read cinema-frontend-tests/test_day_points.js
const fs = globalThis.Deno ? null : await import('node:fs');
const read = f => { const u = new URL(f, import.meta.url); return fs ? fs.readFileSync(u, 'utf8') : Deno.readTextFileSync(u); };
const FILES = { SOLVER: read('../static/cinema/solver.js'), ROUTES: read('../static/cinema/routes.js'), DATES: read('../static/cinema/dates.js'), PLANNER: read('../static/cinema/planner.js') };
const SOLVER = new Function(FILES.SOLVER + '\nreturn SOLVER;')();
const { solve } = new Function('const S=(' + SOLVER.toString() + ')();return S;')();   // as the Web Worker builds it
let fails = 0;
const ok = (c, m) => { if (!c) { fails++; console.log('FAIL', m); } else console.log('ok  ', m); };
let seed = 11; const rnd = () => (seed = (seed * 16807) % 2147483647) / 2147483647, ri = n => Math.floor(rnd() * n);
const strip = r => JSON.stringify({ ...r, ms: 0 });
let sid = 0;
function mkT(nc, f) { const N = nc + 1, T = new Int32Array(N * N); for (let a = 0; a < N; a++) for (let b = 0; b < N; b++) T[a * N + b] = a === b ? 0 : f(a, b); return T; }
function closure(T, N) { for (let k = 0; k < N; k++) for (let a = 0; a < N; a++) { const ak = T[a * N + k]; if (ak < 0) continue;
  for (let b = 0; b < N; b++) { const kb = T[k * N + b]; if (kb < 0) continue; const v = ak + kb, ab = T[a * N + b]; if (ab < 0 || v < ab) T[a * N + b] = v; } } }
// closes a day's legs the way the solver's input requires, written independently of planner.js
function closeLegs(L, T, nc, back) { const v = Int32Array.from(L), N = nc + 1;
  for (let k = 0; k < nc; k++) { if (L[k] < 0) continue; for (let c = 0; c < nc; c++) { const kc = c === k ? 0 : back ? T[c * N + k] : T[k * N + c]; if (kc < 0) continue; const w = L[k] + kc; if (v[c] < 0 || w < v[c]) v[c] = w; } }
  return v; }
const film = (nd, byDay) => ({ days: Array.from({ length: nd }, (_, d) => byDay[d] || []) });
function randShows(nd, nc) { return film(nd, Object.fromEntries(Array.from({ length: nd }, (_, d) => [d, Array.from({ length: ri(3) }, () => { const st = 600 + ri(600); return { i: sid++, c: ri(nc), st, en: st + 90 + ri(60) }; })]))); }
// film -> skip | (day, showing), every order of each day: count, then priority, then travel + waiting, using each day's own legs
function brute(P) { const n = P.films.length, nc = P.nc, N = nc + 1, tr = (a, b) => a === b ? 0 : P.T[a * N + b];
  const to = (d, c) => P.ds && P.ds[d] ? P.ds[d][c] : P.T[nc * N + c], fr = (d, c) => P.de && P.de[d] ? P.de[d][c] : P.T[c * N + nc];
  const perms = a => a.length < 2 ? [a] : a.flatMap((x, i) => perms([...a.slice(0, i), ...a.slice(i + 1)]).map(p => [x, ...p]));
  const dayCost = (d, list) => { const w = P.win[d]; if (!w || list.length > (P.maxDay || 99)) return Infinity; let mc = Infinity;
    for (const o of perms(list)) { let t = w.e, c = 0, loc = -1, good = true;
      for (const s of o) { const tt = loc < 0 ? to(d, s.c) : tr(loc, s.c); if (tt < 0) { good = false; break; }
        const arr = loc < 0 ? t + tt : t + P.buf + tt; if (arr > s.st + P.late) { good = false; break; }
        c += tt + (loc < 0 ? 0 : Math.max(0, s.st - arr)); t = s.en; loc = s.c; }
      if (!good) continue; const th = fr(d, loc); if (th >= 0 && t + P.buf + th <= w.l) mc = Math.min(mc, c + th); }
    return mc; };
  let best = -1, bestC = Infinity; const pick = new Array(n), opts = P.films.map(f => { const o = [null]; f.days.forEach((l, d) => l.forEach(s => o.push([d, s]))); return o; });
  (function rec(i) { if (i === n) { const by = new Map(); for (const x of pick) if (x) (by.get(x[0]) || by.set(x[0], []).get(x[0])).push(x[1]);
      let cost = 0; for (const [d, l] of by) { const c = dayCost(d, l); if (c === Infinity) return; cost += c; }
      let cnt = 0, mv = 0; pick.forEach((x, f) => { if (x) { cnt++; mv += 2 ** (n - 1 - f); } });
      const k = P.mode === 'strict' ? mv : cnt * 2 ** 28 + mv; if (k > best || (k === best && cost < bestC)) { best = k; bestC = cost; } return; }
    for (const o of opts[i]) { pick[i] = o; rec(i + 1); } })(0);
  return [best, bestC]; }
const keyOf = (P, r) => { const n = P.films.length; let k = 0; r.chosen.forEach(f => k += 2 ** (n - 1 - f)); return P.mode === 'strict' ? k : k + r.chosen.length * 2 ** 28; };
const costOf = r => r.days.reduce((a, d) => a + d.cost, 0);

// 1) own legs equal to home's, or all null: exactly the same answer (schedule, nodes, status) as no own legs at all
{ let same = 0, tot = 0;
  for (let it = 0; it < 150; it++) { const nc = 3, nd = 3, n = 3 + ri(5), M = Array.from({ length: nc + 1 }, () => Array.from({ length: nc + 1 }, () => rnd() < 0.1 ? -1 : 5 + ri(50))), T = mkT(nc, (a, b) => M[a][b]); closure(T, nc + 1);
    const P = { films: Array.from({ length: n }, () => randShows(nd, nc)), win: Array.from({ length: nd }, () => rnd() < 0.15 ? null : { e: 600, l: 1380 }), T, nc, buf: 10, late: rnd() < 0.3 ? 15 : 0, maxDay: rnd() < 0.2 ? 2 : 0, mode: rnd() < 0.5 ? 'most' : 'strict', budget: 20000 };
    const a = solve({ ...P }), to = Int32Array.from({ length: nc }, (_, c) => T[nc * (nc + 1) + c]), fr = Int32Array.from({ length: nc }, (_, c) => T[c * (nc + 1) + nc]);
    const b = solve({ ...P, ds: P.win.map(() => to), de: P.win.map(() => fr) }), c = solve({ ...P, ds: P.win.map(() => null), de: P.win.map(() => null) });
    tot++; if (a.status === 'optimal' && strip(a) === strip(b) && strip(a) === strip(c)) same++; }
  ok(same === tot, `solver: own legs equal to home's change nothing ${same}/${tot}`); }
// 2) brute force: random per-day start and end legs on some days, or one side only, or none
for (const [name, base, noHome] of [['home', 7, false], ['no-home', 31, true]]) { seed = base; let agree = 0, tot = 0, diff = 0;
  for (let it = 0; it < 300; it++) { const nc = 3, nd = 2 + ri(2), n = 3 + ri(3), N = nc + 1, M = Array.from({ length: N }, () => Array.from({ length: N }, () => rnd() < 0.1 ? -1 : 5 + ri(60)));
    const T = mkT(nc, (a, b) => noHome && (a === nc || b === nc) ? -1 : M[a][b]); closure(T, N);   // no-home: legs of 0 that are never routed through
    if (noHome) for (let c = 0; c < nc; c++) T[nc * N + c] = T[c * N + nc] = 0;
    const legs = (back) => Array.from({ length: nd }, () => rnd() < 0.3 ? null : closeLegs(Int32Array.from({ length: nc }, () => rnd() < 0.12 ? -1 : 3 + ri(70)), T, nc, back));
    const P = { films: Array.from({ length: n }, () => randShows(nd, nc)), win: Array.from({ length: nd }, () => rnd() < 0.15 ? null : { e: rnd() < 0.5 ? 600 : 720, l: rnd() < 0.5 ? 1410 : 1320 }), T, nc, buf: 10, late: rnd() < 0.3 ? 15 : 0, maxDay: rnd() < 0.2 ? 2 : 0, mode: rnd() < 0.5 ? 'most' : 'strict', budget: 5000, ds: legs(false), de: legs(true) };
    const r = solve(P), [bk, bc] = brute(P); tot++;
    if (keyOf(P, r) === bk && (bk <= 0 || Math.abs(costOf(r) - bc) < 1e-6) && r.status === 'optimal') agree++; else console.log('mismatch', name, it, P.mode, bk, keyOf(P, r), bc, costOf(r), r.status);
    const { ds, de, ...H } = P; if (strip(solve(H)) !== strip(r)) diff++; }
  ok(agree === tot, `solver: per-day points match brute force, ${name} (count, priority, travel) ${agree}/${tot}`);
  ok(diff > tot / 4, `solver: those per-day points really change the plans (${diff}/${tot} differ)`); }
// 3) capacity bound follows the day's own legs in both directions
{ const nc = 1, T = mkT(nc, (a, b) => 600), f0 = film(2, { 0: [{ i: 1, c: 0, st: 720, en: 840 }], 1: [] }), f1 = film(2, { 0: [{ i: 2, c: 0, st: 900, en: 1020 }], 1: [] }), win = [{ e: 600, l: 1380 }, { e: 600, l: 1380 }], base = { win, T, nc, buf: 10, late: 0, maxDay: 0, mode: 'most', budget: 2000, films: [f0, f1] };
  ok(solve(base).cap[0] === 0 && solve(base).chosen.length === 0, 'cap: home 600 min away -> no film fits');
  const r = solve({ ...base, ds: [Int32Array.of(10), null], de: [Int32Array.of(10), null] });
  ok(r.cap[0] === 2 && r.chosen.length === 2 && r.days[0].cost === 70, 'cap: a near start and end on that day make room for 2 films ' + r.cap);
  ok(solve({ ...base, ds: [Int32Array.of(10), null] }).cap[0] === 0, 'cap: a near start alone is not enough, the end is still home');
  const near = { ...base, T: mkT(nc, () => 10) };
  ok(solve(near).cap[0] === 2, 'cap: home near -> 2 films');
  ok(solve({ ...near, ds: [Int32Array.of(-1), null] }).chosen.length === 0 && solve({ ...near, de: [Int32Array.of(-1), null] }).cap[0] === 0, 'cap: an unreachable own start or end rules the day out');
  ok(solve({ ...near, de: [Int32Array.of(400), null] }).chosen.join() === '0', 'cap: a far end only leaves room for the earlier film'); }

// ---------- planner.js on a stub page ----------
class El { constructor(id) { this.id = id; this.dataset = {}; this.style = {}; this.innerHTML = ''; this.textContent = ''; this.value = ''; this.children = []; this.parentNode = {}; this.L = {}; this.parentNode.classList = this.classList = { add() {}, remove() {}, toggle() {} }; }
  querySelector() { return null; } querySelectorAll() { return this.id === 'ptm' ? RAD : []; } addEventListener(t, f) { (this.L[t] ||= []).push(f); } setAttribute() {} insertAdjacentHTML() {} before() {} scrollIntoView() {} }
const RAD = ['T', 'B', 'D'].map(v => { const e = new El('r' + v); e.value = v; return e; });
const dstr = d => d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0');
const D1 = new Date(Date.now() + 864e5), D2 = new Date(Date.now() + 2 * 864e5), D1s = dstr(D1), D2s = dstr(D2);
const CINS = [['One', 53.48], ['Two', 53.49], ['Three', 53.60]].map(([name, lat]) => ({ name, chain: 'X', lat, lng: -2.24 }));
const data = shows => ({ from: D1s, to: D2s, films: { a: 'Film A', b: 'Film B', c: 'Film C' }, cinemas: CINS, shows });
const SHOWS = [{ k: 'a', c: 0, t: D1s + 'T12:00:00', e: D1s + 'T14:00:00', d: 110, x: [] }, { k: 'b', c: 1, t: D1s + 'T18:00:00', e: D1s + 'T20:00:00', d: 110, x: [] }, { k: 'c', c: 2, t: D2s + 'T15:00:00', e: D2s + 'T17:00:00', d: 110, x: [] }];
const LIB = { screenType: () => 'Standard', isScreen: () => false, stCmp: () => 0, pickAllows: () => true, esc: String, tagBadges: () => '', staleNote: () => '' };
const HOME = { pc: 'M1 1AE', lat: 53.47, lng: -2.24 }, WORK = { pc: 'M2 3AA', lat: 53.488, lng: -2.245, area: 'Deansgate' }, GYM = { pc: 'SK1 1AA', lat: 53.41, lng: -2.16, area: '' };
const PICKS = ['a', 'b', 'c'].map(k => ({ k }));
const wrapper = 'const SOLVER0=SOLVER;SOLVER=function(){const s=SOLVER0();return {solve(P){hook(P);return s.solve(P)}}};';
function page({ set = {}, key = null, shows = SHOWS, picks = PICKS, ls = null, respond = null } = {}) {
  const LS = ls || new Map(); if (!ls) { LS.set('fazon-cinema-planner', JSON.stringify({ picks, set: { pc: HOME.pc, geo: HOME, ...set } })); if (key) LS.set('fazon-cinema-gkey', key); }
  const localStorage = { getItem: k => LS.has(k) ? LS.get(k) : null, setItem: (k, v) => void LS.set(k, String(v)), removeItem: k => void LS.delete(k) };
  const t = { LS, urls: [], calls: [], P: null, MINS: { TRANSIT: 20, WALK: 99, BICYCLE: 11, DRIVE: 7 } };
  const fetch = async (u, o) => { t.urls.push(u);
    if (u.startsWith('https://api.postcodes.io/')) return respond(u, o);
    if (!u.startsWith('https://routes.googleapis.com/')) throw new Error('unexpected fetch ' + u);
    const b = JSON.parse(o.body); t.calls.push(b);
    return { ok: true, status: 200, text: async () => JSON.stringify(b.destinations.map((_, k) => ({ destinationIndex: k, condition: 'ROUTE_EXISTS', duration: t.MINS[b.travelMode] * 60 + 's' }))) }; };
  const els = new Map(), document = { getElementById: id => els.get(id) || els.set(id, new El(id)).get(id), createElement: () => new El('') };
  document.getElementById('mode').children = [];
  new Function('document', 'localStorage', 'window', 'fetch', 'location', 'history', 'Worker', 'hook', [FILES.DATES, FILES.SOLVER, wrapper, FILES.ROUTES, FILES.PLANNER, 'return CINEMA_PLANNER;'].join('\n'))(document, localStorage, { matchMedia: () => ({ matches: false }) }, fetch, { hash: '', search: '', pathname: '/' }, { replaceState() {} }, undefined, P => { t.P = P; })(data(shows), LIB);
  t.$ = document.getElementById; t.plan = async () => { t.calls = []; await t.$('pgo').onclick(); return t.$('pout').innerHTML; };
  t.saved = () => JSON.parse(LS.get('fazon-cinema-planner')); t.pair = b => [b.origins, b.destinations];
  return t; }
const legsOf = t => t.calls.flatMap(b => b.destinations.map(d => b.travelMode + ' ' + b.origins[0].waypoint.location.latLng.latitude + '>' + d.waypoint.location.latLng.latitude));
const pjson = P => JSON.stringify({ ...P, T: Array.from(P.T), budget: 0 });
const nh = s => s.replace(/solved in [\d.]+ s/, '');

// old settings, an off day, the home postcode typed again: all identical to a plan with no points, and no own legs are sent
{ const a = page(), h = await a.plan(); ok(!('ds' in a.P) && !('de' in a.P), 'plan: no points -> solver input has no ds/de');
  const b = page({ set: { pts: { [D1s]: { s: { ...HOME }, e: { ...HOME } } } } }), hb = await b.plan();
  ok(pjson(a.P) === pjson(b.P) && nh(h) === nh(hb), 'plan: a point that repeats the home postcode is just home');
  const c = page({ set: { days: Object.fromEntries([0, 1, 2, 3, 4, 5, 6].map(w => [w, { on: true, e: '10:00', l: '00:00' }])), dates: { [D2s]: { on: false, e: '10:00', l: '00:00' } }, pts: { [D2s]: { s: WORK } } } });
  const d = page({ set: { days: Object.fromEntries([0, 1, 2, 3, 4, 5, 6].map(w => [w, { on: true, e: '10:00', l: '00:00' }])), dates: { [D2s]: { on: false, e: '10:00', l: '00:00' } } } });
  const hc = await c.plan(), hd = await d.plan(); ok(pjson(c.P) === pjson(d.P) && nh(hc) === nh(hd), 'plan: a point on a day you’re not free changes nothing');
  ok(/Leave home/.test(h) && /Home by/.test(h) && !/Finish at/.test(h) && !h.includes('53.47'), 'plan: home wording, home never in a link');
  ok(a.$('pgo').textContent === 'Plan my schedule', 'plan: button normal'); }
// saved settings: anything odd is dropped, past dates go like set.dates, old saves load
{ const past = dstr(new Date(Date.now() - 3 * 864e5));
  const t = page({ set: { dates: { [past]: { on: true } }, pts: { [past]: { s: WORK }, [D1s]: { s: 7, e: { pc: 5 }, zz: 1 }, [D2s]: { e: GYM }, nonsense: { s: WORK } } } }); await t.plan(); t.$('pobj').onchange();
  const p = t.saved().set.pts; ok(Object.keys(p).join() === D2s && p[D2s].e.pc === 'SK1 1AA' && !Object.keys(t.saved().set.dates).length, 'load: past and malformed points dropped, good ones kept ' + JSON.stringify(p));
  for (const v of [null, [], 'x', 5]) { const u = page({ set: { pts: v } }); await u.plan(); ok(u.$('pout').innerHTML.includes('Seeing'), 'load: pts = ' + JSON.stringify(v) + ' plans fine'); } }
// no key: straight-line estimates like home's, walking under the limit, no requests
{ const t = page({ set: { pts: { [D1s]: { s: WORK }, [D2s]: { e: GYM } } } }), h = await t.plan();
  const km = 12742 * Math.asin(Math.sqrt(Math.sin((WORK.lat - 53.48) * Math.PI / 360) ** 2 + Math.cos(WORK.lat * Math.PI / 180) * Math.cos(53.48 * Math.PI / 180) * Math.sin((WORK.lng + 2.24) * Math.PI / 360) ** 2));
  ok(km < 1.5 && t.P.ds[0][0] === Math.round(km * 17), `est: ${km.toFixed(2)} km start leg walks like home's: ${t.P.ds[0][0]} min`);
  ok(t.P.ds[1] === null && t.P.de[0] === null && t.P.de[1] instanceof Int32Array, 'est: only the set sides get own legs');
  const far = Math.round(15 + 12742 * Math.asin(Math.sqrt(Math.sin((GYM.lat - 53.60) * Math.PI / 360) ** 2 + Math.cos(GYM.lat * Math.PI / 180) * Math.cos(53.60 * Math.PI / 180) * Math.sin((GYM.lng + 2.24) * Math.PI / 360) ** 2)) * 2.6);
  ok(t.P.de[1][2] === far, 'est: public transport formula beyond the walking limit ' + far);
  ok(!t.urls.length, 'est: no network at all');
  ok(/Leave M2 3AA/.test(h) && /Finish at SK1 1AA/.test(h) && /~\d.* to SK1 1AA/.test(h), 'itinerary: Leave <label> / Finish at <label>, estimates marked ~');
  ok(h.split('Leave home').length === 2 && h.split('Home by').length === 2, 'itinerary: the unset sides still say home ' + h.split('Home by').length);
  ok(h.includes(`origin=${WORK.lat},${WORK.lng}&destination=53.48,-2.24`) && h.includes(`origin=53.6,-2.24&destination=${GYM.lat},${GYM.lng}`), 'itinerary: Maps links use the real points');
  ok(!/<a[^>]*(M2|SK1|%20)/.test(h), 'itinerary: no postcode in any link');
  ok(/href="https:\/\/www\.google\.com\/maps\/dir\/\?api=1&destination=53\.6,-2\.24/.test(h) && /api=1&origin=53\.49,-2\.24&travelmode/.test(h), 'itinerary: legs from and to home stay without home’s coordinates'); }
// key: only the missing legs, under the current profile
{ const t = page({ key: 'TESTKEY' }); await t.plan(); const first = legsOf(t).filter(x => x[0] === 'T');
  ok(first.length === 3 * 2 + 1, 'key: home plan fetches home <-> 3 cinemas + the one linked pair ' + first.length);
  const ls = t.LS, cfg = t.saved(); cfg.set.pts = { [D1s]: { s: WORK } }; ls.set('fazon-cinema-planner', JSON.stringify(cfg));
  const u = page({ ls }); await u.plan(); const l = legsOf(u);
  ok(l.filter(x => x.startsWith('TRANSIT')).join() === `TRANSIT ${WORK.lat}>53.48,TRANSIT ${WORK.lat}>53.49`, 'key: a new start on day 1 fetches just its 2 cinemas that day ' + l);
  ok(!l.some(x => x.includes('53.47') || x.endsWith('>' + WORK.lat)), 'key: nothing to or from home, nothing back to the start, nothing re-billed');
  ok(u.urls.every(x => !/M1|M2|SK1|lat|53\./.test(x)), 'key: coordinates and postcodes stay out of request URLs');
  const v = page({ ls }); await v.plan(); ok(!v.calls.length, 'key: a fresh page reuses it all');
  v.$('ptm').onchange({ target: { value: 'B' } }); RAD.forEach(r => r.checked = r.value === 'B'); await v.plan();
  const b = legsOf(v); ok(b.length && b.every(x => x.startsWith('BICYCLE')) && b.filter(x => x.startsWith('BICYCLE ' + WORK.lat)).length === 2, 'key: another way of travelling fetches under its own prefix');
  ok(Object.keys(JSON.parse(ls.get('fazon-cinema-gtimes'))).some(k => k.startsWith('B|' + WORK.lat.toFixed(4))), 'key: cached under B|');
  v.$('ptm').onchange({ target: { value: 'T' } }); await v.plan(); ok(!v.calls.length, 'key: back to transit: nothing fetched');
  // both days away, both sides: home isn't fetched at all, and a point on two days and both sides is one node
  const w = page({ key: 'K2', set: { pts: { [D1s]: { s: WORK, e: WORK }, [D2s]: { s: WORK, e: GYM } } } }); await w.plan(); const m = legsOf(w).filter(x => x.startsWith('TRANSIT'));
  ok(!m.some(x => x.includes('53.47')), 'key: no day uses home -> no home legs');
  ok(new Set(m).size === m.length && m.length === 2 + 2 + 1 + 1 + 1, 'key: no duplicates: work<->1,2 + work->3 + 3->gym + 1->2 ' + m.length);
  ok(!m.some(x => x.includes(`${WORK.lat}>${GYM.lat}`) || x.includes(`${GYM.lat}>${WORK.lat}`)), 'key: never point to point'); }
// closure: via that day's cinemas only, fetched times can't be undercut by another day's estimates, journey limit, brute force on the real input
{ const put = (LS, tbl) => { const o = {}, day = Math.floor(Date.now() / 864e5); for (const [k, v] of Object.entries(tbl)) o[k] = [v, day]; LS.set('fazon-cinema-gtimes', JSON.stringify(o)); };
  const ll = p => p.lat.toFixed(4) + ',' + p.lng.toFixed(4), c = CINS.map(ll), w = ll(WORK);
  const shows = [SHOWS[0], SHOWS[1], { ...SHOWS[2], t: D1s + 'T23:50:00', e: D1s + 'T23:51:00', c: 2 }];
  const t = page({ key: 'K', shows: SHOWS, set: { pts: { [D1s]: { s: WORK } } } });
  put(t.LS, { [`T|${w}>${c[0]}`]: 10, [`T|${w}>${c[1]}`]: 100, [`T|${c[0]}>${c[1]}`]: 15, [`T|${c[1]}>${c[0]}`]: 15 }); 
  const u = page({ ls: t.LS }); await u.plan();
  ok(u.P.ds[0][1] === 25, 'closure: start -> Two goes via One, 10 + 15 = 25, not 100 ' + u.P.ds[0][1]);
  const v = page({ key: 'K', set: { maxLeg: 20, pts: { [D1s]: { s: WORK } } } }); put(v.LS, { [`T|${w}>${c[0]}`]: 10, [`T|${w}>${c[1]}`]: 100, [`T|${c[0]}>${c[1]}`]: 15 });
  const x = page({ ls: v.LS }), hx = await x.plan(); ok(x.P.ds[0][1] === -1 && x.P.ds[0][0] === 10, 'max journey: applied after the closure, like every other leg ' + x.P.ds[0]);
  // Three has a candidate only on day 2: its estimate from a day-1 point must not undercut the fetched time to One
  const y = page({ key: 'K', set: { pts: { [D1s]: { s: { pc: 'OL1 1AA', lat: 53.60, lng: -2.241, area: '' } } } } }), o = '53.6000,-2.2410';
  put(y.LS, { [`T|${o}>${c[0]}`]: 90, [`T|${o}>${c[1]}`]: 95, [`T|${c[2]}>${c[0]}`]: 20 });
  const z = page({ ls: y.LS }); await z.plan(); ok(z.P.ds[0][0] === 90, 'closure: no route via a cinema with no showing that day ' + z.P.ds[0][0]);
  ok(!x.$('pout').innerHTML.includes('Planner error') && /journey limit/.test(hx), 'max journey: the film left out says why');
  let good = 0, tot = 0; seed = 5;
  for (let it = 0; it < 40; it++) { const legs = {}, pts = [WORK, GYM], ns = [...c, ...pts.map(ll)];   // not metric on purpose: Google's times aren't
    for (const a of ns) for (const b of ns) if (a !== b) legs[`T|${a}>${b}`] = 5 + ri(70);
    const tt = page({ key: 'K', set: { late: it % 3 ? 0 : 15, buf: 10, pts: { [D1s]: { s: WORK, e: GYM }, [D2s]: it % 2 ? { e: WORK } : { s: GYM } } } }); put(tt.LS, legs); tt.LS.set('fazon-cinema-gkey', 'K'); const p2 = page({ ls: tt.LS }); await p2.plan();
    const P = p2.P, r = solve(P), [bk, bc] = brute(P); tot++; if (keyOf(P, r) === bk && (bk <= 0 || Math.abs(costOf(r) - bc) < 1e-6) && r.status === 'optimal') good++; else console.log('e2e mismatch', it, bk, keyOf(P, r), bc, costOf(r), r.status); }
  ok(good === tot, `plan: real solver input with Google-style non-metric legs matches brute force ${good}/${tot}`); }
// "Home by" uses the real end point; the note says so
{ const late = [{ k: 'a', c: 0, t: D1s + 'T21:00:00', e: D1s + 'T23:00:00', d: 110, x: [] }], LATE = { ...GYM, lat: 52.0, lng: -1.0 };
  const near = page({ shows: late, picks: [{ k: 'a' }] }), hn = await near.plan(); ok(/Seeing 1 of 1/.test(hn), 'home by: reachable from home');
  const t = page({ shows: late, picks: [{ k: 'a' }], set: { pts: { [D1s]: { e: LATE } } } }), h = await t.plan();
  ok(/Seeing 0 of 1/.test(h) && /too late to reach the day’s end point by your “Home by” time/.test(h) && /with travel from and to each day’s start and end/.test(h), 'home by: a far end point rules the showing out and the note says so');
  const u = page({ shows: late, picks: [{ k: 'a' }], set: { dates: { [D1s]: { on: true, e: '18:00', l: '00:00' } }, pts: { [D1s]: { s: LATE } } } }), hu = await u.plan(); ok(/Seeing 0 of 1/.test(hu) && !/too late to reach/.test(hu), 'leave from: a far start rules it out at the other end'); }
// no-home mode: set sides are honoured, the other stays the first / last cinema
{ const a = page({ set: { noHome: true, pc: '', geo: null } }), ha = await a.plan(); ok(!/Leave home|Home by|out \d/.test(ha) && !('ds' in a.P), 'no-home: unchanged without points');
  const s = page({ set: { noHome: true, pc: '', geo: null, pts: { [D1s]: { s: WORK } } } }), hs = await s.plan();
  ok(/Leave M2 3AA/.test(hs) && !/Finish at|Home by/.test(hs) && s.P.ds[0] && !s.P.ds[1] && s.P.T[3 * 4 + 0] === 0, 'no-home: start only -> leaves it, ends at the last film ');
  const e = page({ set: { noHome: true, pc: '', geo: null, pts: { [D2s]: { e: GYM } } } }), he = await e.plan();
  ok(/Finish at SK1 1AA/.test(he) && !/Leave M|Leave SK|Leave home/.test(he) && /travel/.test(he), 'no-home: end only -> starts at the first cinema, travel shown');
  const m = page({ set: { noHome: true, pts: { [D1s]: { s: { ...HOME } } } } }), hm = await m.plan(); ok(/Leave M1 1AE/.test(hm) && m.P.ds[0], 'no-home: the saved home postcode is a real point here'); }
// typing postcodes: the page's own handlers against a fake postcodes.io
{ const tr = (d) => ({ closest: () => ({ dataset: { pd: d } }) }); const lookups = [];
  const respond = async (u, o) => { const pc = JSON.parse(o.body).postcodes[0]; lookups.push([u, pc]); if (pc === 'OL9 9ZZ') throw new TypeError('offline');
    const known = pc === 'BL1 1AA'; return { ok: true, status: 200, json: async () => ({ result: [{ query: pc, result: known ? { postcode: 'BL1 1AA', latitude: 53.58, longitude: -2.43, admin_ward: 'Halliwell', admin_district: 'Bolton' } : null }] }) }; };
  const t = page({ respond }), P = t.$('pdays'), type = async (k, value, d = D1s) => { P.L.change.at(-1)({ target: { ...tr(d), dataset: { pt: k }, value, closest: tr(d).closest } }); await new Promise(r => setTimeout(r, 5)); };
  await type('s', 'nonsense'); ok(t.saved().set.pts[D1s].s.err === 'fmt' && !lookups.length, 'type: bad format -> no lookup');
  ok(t.$('pgo').disabled && /Check the postcode on/.test(t.$('pgo').textContent), 'type: an unresolved point stops the plan, never quietly home: ' + t.$('pgo').textContent);
  await t.$('pgo').onclick(); ok(!t.P, 'type: and pressing Plan does nothing');
  await type('s', 'zz1 1zz'); ok(t.saved().set.pts[D1s].s.err === 'nf' && lookups.at(-1)[1] === 'ZZ1 1ZZ' && t.$('pgo').disabled, 'type: not found');
  await type('s', 'OL9 9ZZ'); ok(t.saved().set.pts[D1s].s.err === 'net' && t.$('pgo').disabled, 'type: postcodes.io unreachable');
  ok(/Check the postcode/.test(t.$('pgo').textContent), 'type: still blocked');
  await type('s', 'bl11aa'); const p = t.saved().set.pts[D1s].s; ok(p.pc === 'BL1 1AA' && p.lat === 53.58 && p.area === 'Halliwell, Bolton' && !p.err && !t.$('pgo').disabled && t.$('pgo').textContent === 'Plan my schedule', 'type: found, tidied, plan allowed ' + JSON.stringify(p));
  const n = lookups.length; await type('e', 'BL1 1AA', D2s); ok(lookups.length === n && t.saved().set.pts[D2s].e.lat === 53.58, 'type: a known postcode is not looked up again');
  ok(t.urls.every(u => u === 'https://api.postcodes.io/postcodes'), 'type: the postcode goes in the POST body, not the URL');
  const h = await t.plan(); ok(/Leave BL1 1AA/.test(h) && /Finish at BL1 1AA/.test(h), 'type: used by the plan');
  await type('s', ''); await type('e', '', D2s); ok(!Object.keys(t.saved().set.pts).length, 'type: clearing the boxes clears the day'); 
  await t.$('pgo').onclick(); ok(!('ds' in t.P), 'type: and the plan is back to no points'); }
// the stale-answer race: a slow lookup must not overwrite a newer entry
{ let release; const gate = new Promise(r => release = r), tr = { closest: () => ({ dataset: { pd: D1s } }) };
  const respond = async () => { await gate; return { ok: true, status: 200, json: async () => ({ result: [{ result: { postcode: 'BL1 1AA', latitude: 53.58, longitude: -2.43 } }] }) }; };
  const t = page({ respond }), P = t.$('pdays'), go = v => P.L.change.at(-1)({ target: { ...tr, dataset: { pt: 's' }, value: v } });
  go('BL1 1AA'); ok(t.$('pgo').disabled, 'race: Plan waits for the lookup'); go(''); release(); await new Promise(r => setTimeout(r, 5));
  ok(!t.saved().set.pts[D1s], 'race: a late answer doesn’t bring a cleared point back'); }
console.log(fails ? fails + ' FAILED' : 'ALL PASSED');
