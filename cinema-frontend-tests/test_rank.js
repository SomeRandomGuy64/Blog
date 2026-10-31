// Cinema Planner: ranked cinemas and screens. Solver checks (exact search, brute force, film safety) and planner.js on a stub page
// with listings of its own (no real key, API or network).
// Run:  deno run --allow-read cinema-frontend-tests/test_rank.js
const fs = globalThis.Deno ? null : await import('node:fs');
const read = f => { const u = new URL(f, import.meta.url); return fs ? fs.readFileSync(u, 'utf8') : Deno.readTextFileSync(u); };
const FILES = { SOLVER: read('../static/cinema/solver.js'), ROUTES: read('../static/cinema/routes.js'), DATES: read('../static/cinema/dates.js'), PLANNER: read('../static/cinema/planner.js') };
const SOLVER = new Function(FILES.SOLVER + '\nreturn SOLVER;')();
const { solve } = new Function('const S=(' + SOLVER.toString() + ')();return S;')();   // as the Web Worker builds it
let fails = 0;
const ok = (c, m) => { if (!c) { fails++; console.log('FAIL', m); } else console.log('ok  ', m); };
let seed = 17; const rnd = () => (seed = (seed * 16807) % 2147483647) / 2147483647, ri = n => Math.floor(rnd() * n);
const strip = r => JSON.stringify({ ...r, ms: 0 });
function mkT(nc, f) { const N = nc + 1, T = new Int32Array(N * N); for (let a = 0; a < N; a++) for (let b = 0; b < N; b++) T[a * N + b] = a === b ? 0 : f(a, b); return T; }
function closure(T, N) { for (let k = 0; k < N; k++) for (let a = 0; a < N; a++) { const ak = T[a * N + k]; if (ak < 0) continue;
  for (let b = 0; b < N; b++) { const kb = T[k * N + b]; if (kb < 0) continue; const v = ak + kb, ab = T[a * N + b]; if (ab < 0 || v < ab) T[a * N + b] = v; } } }
function closeLegs(L, T, nc, back) { const v = Int32Array.from(L), N = nc + 1;
  for (let k = 0; k < nc; k++) { if (L[k] < 0) continue; for (let c = 0; c < nc; c++) { const kc = c === k ? 0 : back ? T[c * N + k] : T[k * N + c]; if (kc < 0) continue; const w = L[k] + kc; if (v[c] < 0 || w < v[c]) v[c] = w; } }
  return v; }
const film = (nd, byDay) => ({ days: Array.from({ length: nd }, (_, d) => byDay[d] || []) });
let sid = 0;
const PEN = [0, 15, 30, 45, 60, 90];
function randShows(nd, nc, pen) { return film(nd, Object.fromEntries(Array.from({ length: nd }, (_, d) => [d, Array.from({ length: ri(3) }, () => { const st = 600 + ri(600), o = { i: sid++, c: ri(nc), st, en: st + 90 + ri(60) }, p = pen ? PEN[ri(6)] : 0; if (p) o.p = p; return o; })]))); }
const showsOf = P => new Map(P.films.flatMap(f => f.days.flat()).map(s => [s.i, s]));
const noP = P => ({ ...P, films: P.films.map(f => ({ days: f.days.map(L => L.map(({ p, ...s }) => s)) })) });
const to = (P, d, c) => P.ds && P.ds[d] ? P.ds[d][c] : P.T[P.nc * (P.nc + 1) + c], fr = (P, d, c) => P.de && P.de[d] ? P.de[d][c] : P.T[c * (P.nc + 1) + P.nc];
// travel + waiting of one day's order, without any penalty; Infinity if it doesn't work
function walk(P, d, o) { const w = P.win[d], N = P.nc + 1; let t = w.e, c = 0, loc = -1;
  for (const s of o) { const tt = loc < 0 ? to(P, d, s.c) : s.c === loc ? 0 : P.T[loc * N + s.c]; if (tt < 0) return Infinity;
    const arr = loc < 0 ? t + tt : t + P.buf + tt; if (arr > s.st + P.late) return Infinity;
    c += tt + (loc < 0 ? 0 : Math.max(0, s.st - arr)); t = s.en; loc = s.c; }
  const th = fr(P, d, loc); return th >= 0 && t + P.buf + th <= w.l ? c + th : Infinity; }
// film -> skip | (day, showing), every order of each day: count, then priority, then travel + waiting + penalties
function brute(P) { const n = P.films.length, perms = a => a.length < 2 ? [a] : a.flatMap((x, i) => perms([...a.slice(0, i), ...a.slice(i + 1)]).map(p => [x, ...p]));
  const dayCost = (d, list) => { if (!P.win[d] || list.length > (P.maxDay || 99)) return Infinity; let mc = Infinity; for (const o of perms(list)) mc = Math.min(mc, walk(P, d, o)); return mc + list.reduce((a, s) => a + (s.p || 0), 0); };
  let best = -1, bestC = Infinity; const pick = new Array(n), opts = P.films.map(f => { const o = [null]; f.days.forEach((l, d) => l.forEach(s => o.push([d, s]))); return o; });
  (function rec(i) { if (i === n) { const by = new Map(); for (const x of pick) if (x) (by.get(x[0]) || by.set(x[0], []).get(x[0])).push(x[1]);
      let cost = 0; for (const [d, l] of by) { const c = dayCost(d, l); if (c === Infinity) return; cost += c; }
      let cnt = 0, mv = 0; pick.forEach((x, f) => { if (x) { cnt++; mv += 2 ** (n - 1 - f); } });
      const k = P.mode === 'strict' ? mv : cnt * 2 ** 28 + mv; if (k > best || (k === best && cost < bestC)) { best = k; bestC = cost; } return; }
    for (const o of opts[i]) { pick[i] = o; rec(i + 1); } })(0);
  return [best, bestC]; }
// the same answer for more films: each day's cheapest order of every film subset, then every split of the films between days
function exact(P) { const n = P.films.length, N = P.nc + 1, ND = P.win.length; let g = new Map([[0, 0]]);
  for (let d = 0; d < ND; d++) { const w = P.win[d]; if (!w) continue; const tbl = new Map(), cap = P.maxDay || 99;
    const rec = (loc, t, S, c, k) => { for (let f = 0; f < n; f++) { if (S >> f & 1) continue; for (const s of P.films[f].days[d]) { const tt = loc < 0 ? to(P, d, s.c) : s.c === loc ? 0 : P.T[loc * N + s.c]; if (tt < 0) continue;
          const arr = loc < 0 ? t + tt : t + P.buf + tt; if (arr > s.st + P.late) continue; const c2 = c + tt + (loc < 0 ? 0 : Math.max(0, s.st - arr)) + (s.p || 0), S2 = S | 1 << f, th = fr(P, d, s.c);
          if (th >= 0 && s.en + P.buf + th <= w.l && c2 + th < (tbl.get(S2) ?? Infinity)) tbl.set(S2, c2 + th); if (k + 1 < cap) rec(s.c, s.en, S2, c2, k + 1); } } };
    rec(-1, w.e, 0, 0, 0); const h = new Map(g); for (const [S, c] of g) for (const [X, x] of tbl) if (!(S & X) && c + x < (h.get(S | X) ?? Infinity)) h.set(S | X, c + x); g = h; }
  let best = -1, bestC = Infinity; for (const [S, c] of g) { let cnt = 0, mv = 0; for (let f = 0; f < n; f++) if (S >> f & 1) { cnt++; mv += 2 ** (n - 1 - f); } const k = P.mode === 'strict' ? mv : cnt * 2 ** 28 + mv; if (k > best || (k === best && c < bestC)) { best = k; bestC = c; } }
  return [best, bestC]; }
const keyOf = (P, r) => { const n = P.films.length; let k = 0; r.chosen.forEach(f => k += 2 ** (n - 1 - f)); return P.mode === 'strict' ? k : k + r.chosen.length * 2 ** 28; };
const randP = (nd, nc, n, pen, own) => { const N = nc + 1, M = Array.from({ length: N }, () => Array.from({ length: N }, () => rnd() < 0.1 ? -1 : 5 + ri(60))), T = mkT(nc, (a, b) => M[a][b]); closure(T, N);
  const legs = back => Array.from({ length: nd }, () => rnd() < 0.5 ? null : closeLegs(Int32Array.from({ length: nc }, () => rnd() < 0.1 ? -1 : 3 + ri(70)), T, nc, back));
  return { films: Array.from({ length: n }, () => randShows(nd, nc, pen)), win: Array.from({ length: nd }, () => rnd() < 0.15 ? null : { e: rnd() < 0.5 ? 600 : 720, l: rnd() < 0.5 ? 1410 : 1320 }), T, nc, buf: 10, late: rnd() < 0.3 ? 15 : 0, maxDay: rnd() < 0.2 ? 2 : 0, mode: rnd() < 0.5 ? 'most' : 'strict', budget: 5000, ...(own ? { ds: legs(false), de: legs(true) } : {}) }; };

// 1) no penalties, or penalties of 0: exactly today's answer, nodes and all
{ let same = 0, tot = 0;
  for (let it = 0; it < 120; it++) { const P = randP(3, 3, 3 + ri(5), false, it % 2), Z = { ...P, films: P.films.map(f => ({ days: f.days.map(L => L.map(s => ({ ...s, p: 0 }))) })) }; tot++; if (strip(solve(P)) === strip(solve(Z))) same++; }
  ok(same === tot, `solver: p of 0 changes nothing, schedule, nodes or memo ${same}/${tot}`); }
// 1b) and against the solver as it was before rankings, on a clock that makes timed-out searches repeatable too
{ const old = new Function(read('./solver_unranked_ref.js') + '\nreturn SOLVER;')()().solve, real = Date.now; let same = 0, tot = 0, tmo = 0, clock = 0; Date.now = () => ++clock;
  for (let it = 0; it < 200; it++) { const P = randP(3, 3, 4 + ri(8), false, it % 2); if (it % 3 === 0) { P.films = Array.from({ length: 26 }, () => ({ days: P.win.map(() => Array.from({ length: 1 + ri(4) }, () => { const st = 600 + ri(720); return { i: sid++, c: ri(3), st, en: st + 100 + ri(60) }; })) })); P.win = P.win.map(() => ({ e: 600, l: 1440 })); P.budget = 400 + ri(1500); }
    const a = old(P), b = solve(P); tot++; if (strip(a) === strip(b)) same++; if (a.status === 'timeout') tmo++; }
  Date.now = real; ok(same === tot && tmo > 0, `solver: byte-identical to the solver before rankings ${same}/${tot} (${tmo} timed out)`); }
// 2) brute force: same films as without penalties, the least travel + waiting + penalty, and reported costs without the penalty
{ let agree = 0, tot = 0, diff = 0, films = 0, honest = 0, pens = 0;
  for (let it = 0; it < 500; it++) { const P = randP(2 + ri(2), 3, 3 + ri(3), true, it % 2), r = solve(P), [bk, bc] = brute(P), sh = showsOf(P), r0 = solve(noP(P)); tot++;
    const pen = r.days.reduce((a, d) => a + d.seq.reduce((b, i) => b + (sh.get(i).p || 0), 0), 0), cost = r.days.reduce((a, d) => a + d.cost, 0);
    if (keyOf(P, r) === bk && (bk <= 0 || Math.abs(cost + pen - bc) < 1e-6) && r.status === 'optimal') agree++; else console.log('mismatch', it, P.mode, bk, keyOf(P, r), bc, cost + pen, r.status);
    if (keyOf(P, r) === keyOf(P, r0) && r.chosen.join() === r0.chosen.join()) films++;
    if (r.days.every(d => Math.abs(walk(P, d.d, d.seq.map(i => sh.get(i))) - d.cost) < 1e-9)) honest++;
    if (pen) pens++; if (r.days.map(d => d.seq.join()).join('|') !== r0.days.map(d => d.seq.join()).join('|')) diff++; }
  ok(agree === tot, `solver: ranked plans match brute force ${agree}/${tot}`);
  ok(films === tot, `solver: a ranking never changes which films are seen ${films}/${tot}`);
  ok(honest === tot && pens > tot / 2, `solver: each day's reported cost is its real travel + waiting, penalty left out ${honest}/${tot} (${pens} with a penalty)`);
  ok(diff > tot / 5, `solver: penalties really change the plans (${diff}/${tot} differ)`); }
// 2b) more films and days, where the search's own bound has to do the work
{ let agree = 0, tot = 0; seed = 29;
  for (let it = 0; it < 400; it++) { const nd = 3 + ri(2), n = 6 + ri(4), P = randP(nd, 4, n, true, it % 2); for (const f of P.films) for (let d = 0; d < nd; d++) if (f.days[d].length < 2) { const st = 600 + ri(600), c = ri(4), p = PEN[ri(6)]; f.days[d].push({ i: sid++, c, st, en: st + 90 + ri(60), ...(p ? { p } : {}) }); }
    P.win = P.win.map(() => ({ e: 600, l: 1380 })); P.budget = 8000; const r = solve(P), [bk, bc] = exact(P), sh = showsOf(P), v = r.days.reduce((a, d) => a + d.cost + d.seq.reduce((b, i) => b + (sh.get(i).p || 0), 0), 0); tot++;
    if (keyOf(P, r) === bk && Math.abs(v - bc) < 1e-6 && r.status === 'optimal') agree++; else console.log('mismatch', it, P.mode, bk, keyOf(P, r), bc, v, r.status); }
  ok(agree === tot, `solver: larger ranked plans match the exact split ${agree}/${tot}`); }
// 3) what it means: the favourite wins unless the other saves more than the gap, however the search gets there
{ const plan = (km, gap) => { const T = mkT(2, (a, b) => a === 2 || b === 2 ? ((a === 2 ? b : a) === 0 ? 10 : km) : 15), F = film(1, { 0: [{ i: 1, c: 0, st: 720, en: 840, p: gap }, { i: 2, c: 1, st: 720, en: 840 }] });
    return solve({ films: [F], win: [{ e: 600, l: 1380 }], T, nc: 2, buf: 10, late: 0, maxDay: 0, mode: 'most', budget: 2000 }); };
  ok(plan(40, 90).days[0].seq[0] === 2 && plan(40, 90).days[0].cost === 80, 'meaning: favourite 30 min further each way: 60 < 90, still the favourite, cost is travel only');
  ok(plan(60, 90).days[0].seq[0] === 1, 'meaning: favourite 50 min further each way: 100 > 90, the nearer cinema wins');
  ok(plan(25, 0).days[0].seq[0] === 1, 'meaning: no gap: the nearest, as today');
  // Two films only fit at the less-liked cinema: the ranking gives way
  const T = mkT(2, () => 20), a = film(1, { 0: [{ i: 1, c: 0, st: 720, en: 840 }, { i: 2, c: 1, st: 660, en: 780, p: 90 }] }), b = film(1, { 0: [{ i: 3, c: 1, st: 800, en: 920, p: 90 }] }), base = { win: [{ e: 600, l: 1380 }], T, nc: 2, buf: 10, late: 0, maxDay: 0, budget: 2000 };
  for (const mode of ['most', 'strict']) { const r = solve({ ...base, films: [a, b], mode }); ok(r.chosen.length === 2 && r.days[0].seq.join() === '2,3', `meaning: ${mode}: a film is never lost to the ranking`); } }
// 4) a search that runs out of time never ends up with fewer films because of the ranking (a fake clock makes both runs repeatable)
{ const raw = new Function(FILES.SOLVER.replace('return val(r0)>val(r)?r0:r', 'return r') + '\nreturn SOLVER;')()().solve, once = new Function(FILES.SOLVER.replace('return {solve}}', 'return {solve:run}}') + '\nreturn SOLVER;')()().solve, real = Date.now; let clock = 0; Date.now = () => ++clock; seed = 3;
  const val = (R, n, strict) => { let m = 0; for (const f of R.chosen) m += 2 ** (n - 1 - f); return strict ? m : R.chosen.length * 2 ** n + m; };
  let worse = 0, safe = 0, tmo = 0, tot = 0, one = 0;
  for (let it = 0; it < 40; it++) { const nc = 6, nd = 4, n = 24 + ri(6), N = nc + 1, T = new Int32Array(N * N), strict = it % 4 === 3; for (let a = 0; a < N; a++) for (let b = 0; b < N; b++) T[a * N + b] = a === b ? 0 : 5 + ((a * 7 + b * 3) % 5) * 6;
    const films = Array.from({ length: n }, () => ({ days: Array.from({ length: nd }, () => Array.from({ length: 1 + ri(4) }, () => { const st = 600 + ri(720), c = ri(nc); return { i: sid++, c, st, en: st + 100 + ri(60), p: [0, 30, 60, 90][c % 4] }; })) }));
    const P = { films, win: Array.from({ length: nd }, () => ({ e: 600, l: 1440 })), T, nc, buf: 10, late: 0, maxDay: 0, mode: strict ? 'strict' : 'most', budget: 400 + ri(1500) };
    const a = raw(P), b = solve(P), c = solve(noP(P)); tot++; if (a.status === 'timeout') tmo++; if (val(a, n, strict) < val(c, n, strict)) worse++; if (val(b, n, strict) >= val(c, n, strict)) safe++;
    const c0 = clock, u = solve(noP(P)), c1 = clock, v = once(noP(P)), c2 = clock; if (u.status === 'timeout' && strip(u) === strip(v) && c1 - c0 === c2 - c1) one++; }
  Date.now = real;
  ok(one === tot, `timeout: without a ranking there's no second search ${one}/${tot}`);
  ok(safe === tot, `timeout: never fewer films than the same search without a ranking ${safe}/${tot}`);
  ok(worse > 0 && tmo > 0, `timeout: the check has teeth, ${worse} of ${tmo} timed-out ranked searches alone would have lost out`); }

// ---------- planner.js on a stub page ----------
class El { constructor(id) { this.id = id; this.dataset = {}; this.style = {}; this.innerHTML = ''; this.textContent = ''; this.value = ''; this.children = []; this.parentNode = {}; this.L = {}; this.parentNode.classList = this.classList = { add() {}, remove() {}, toggle() {} }; }
  querySelector() { return null; } querySelectorAll() { return this.id === 'ptm' ? RAD : []; } addEventListener(t, f) { (this.L[t] ||= []).push(f); } setAttribute() {} insertAdjacentHTML() {} before() {} scrollIntoView() {} }
const RAD = ['T', 'B', 'D'].map(v => { const e = new El('r' + v); e.value = v; return e; });
const dstr = d => d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0');
const D1 = dstr(new Date(Date.now() + 864e5));
const CINS = [['Odeon Near', 'Odeon', 53.48], ['Vue Mid', 'Vue', 53.55], ['HOME', 'Independent', 53.60], ['Reel Far', 'Reel', 53.70]].map(([name, chain, lat]) => ({ name, chain, lat, lng: -2.24 }));
const sh = (c, h, s) => ({ k: 'a', c, t: `${D1}T${h}:00:00`, e: `${D1}T${h + 2}:00:00`, d: 110, x: [], ...(s ? { s } : {}) });
const SHOWS = [sh(0, 12, 'Screen 1'), sh(1, 12, 'Screen 10'), sh(1, 12, 'Screen 2'), sh(2, 12, 'Cinema 1'), sh(2, 12, 'Cinema 2'), sh(2, 12, 'Cinema 3'), sh(3, 12)];
const data = () => ({ from: D1, to: D1, films: { a: 'Film A' }, cinemas: CINS, shows: SHOWS });
const LIB = { screenType: () => 'Standard', isScreen: () => false, stCmp: () => 0, pickAllows: (p, x, c, day, cs) => !cs || cs.has(c), esc: String, tagBadges: () => '', staleNote: () => '' };
const HOME = { pc: 'M1 1AE', lat: 53.47, lng: -2.24 };
const wrapper = 'const SOLVER0=SOLVER;SOLVER=function(){const s=SOLVER0();return {solve(P){hook(P);return s.solve(P)}}};';
function page({ set = {}, picks = [{ k: 'a' }], raw = null } = {}) {
  const LS = new Map([['fazon-cinema-planner', raw ?? JSON.stringify({ picks, set: { pc: HOME.pc, geo: HOME, ...set } })]]);
  const localStorage = { getItem: k => LS.has(k) ? LS.get(k) : null, setItem: (k, v) => void LS.set(k, String(v)), removeItem: k => void LS.delete(k) };
  const t = { LS, P: null, urls: [] }, els = new Map(), document = { getElementById: id => els.get(id) || els.set(id, new El(id)).get(id), createElement: () => new El('') };
  document.getElementById('mode').children = [];
  new Function('document', 'localStorage', 'window', 'fetch', 'location', 'history', 'Worker', 'hook', [FILES.DATES, FILES.SOLVER, wrapper, FILES.ROUTES, FILES.PLANNER, 'return CINEMA_PLANNER;'].join('\n'))(document, localStorage, { matchMedia: () => ({ matches: false }) }, u => { t.urls.push(u); throw new Error('no network here'); }, { hash: '', search: '', pathname: '/' }, { replaceState() {} }, undefined, P => { t.P = P; })(data(), LIB);
  t.$ = document.getElementById; t.plan = async () => { await t.$('pgo').onclick(); return t.$('pout').innerHTML; };
  t.saved = () => JSON.parse(LS.get('fazon-cinema-planner')); t.pens = () => Object.fromEntries(t.P.films[0].days.flat().map(s => [s.i, s.p || 0]));
  // the list's own handlers, as the browser would call them
  t.act = (a, i) => t.$('prklist').L.click[0]({ target: { closest: q => q === '[data-act]' ? { dataset: { act: a } } : { dataset: { i } } } });
  t.type = (v, i) => { const tg = { dataset: { act: 'rk' }, value: v, closest: () => ({ dataset: { i } }) }; for (const f of t.$('prklist').L.change) f({ target: tg }); return String(tg.value); };
  t.find = q => { t.$('prkq').value = q; t.$('prkq').oninput(); return t.$('prksugg').innerHTML; };
  t.add = q => { const h = t.find(q), m = /data-i="(\d+)"/.exec(h); t.$('prksugg').onmousedown({ preventDefault() {}, target: { closest: () => ({ dataset: { i: m[1] } }) } }); };
  t.order = () => [...t.$('prklist').innerHTML.matchAll(/<b title="([^"]*)"/g)].map(m => m[1]);
  return t; }
const pjson = P => JSON.stringify({ ...P, T: Array.from(P.T), budget: 0 });
const nh = s => s.replace(/solved in [\d.]+ s/, '');

// no ranking in any form: the same solver input and result, byte for byte
{ const a = page({ raw: JSON.stringify({ picks: [{ k: 'a' }], set: { pc: HOME.pc, geo: HOME } }) }), h = await a.plan();
  ok(!pjson(a.P).includes('"p":') && !/ranked/.test(h), 'none: an old save sends no penalties and says nothing');
  for (const [n, v] of [['empty', []], ['null', null], ['a string', 'x'], ['a number', 5], ['an object', {}], ['junk', [null, 3, 'Odeon', {}, { c: 7 }, { s: 'Cinema 1' }, { ch: 2 }]], ['only gone names', [{ c: 'Closed Cinema' }, { ch: 'Gone Chain' }, { c: 'HOME', s: 'Cinema 9' }, { c: 'Reel Far', s: 'Screen 1' }]]]) {
    const t = page({ set: { rank: v } }), ht = await t.plan(); ok(pjson(t.P) === pjson(a.P) && nh(ht) === nh(h), `none: rank = ${n}: input and result identical`); } }
// saved lists: junk and repeats go, gone names stay, in order, through a change
{ const rank = [{ c: 'Closed Cinema' }, { ch: 'Vue' }, 7, { ch: 'Vue' }, { c: 'HOME', s: 'Cinema 2', z: 1 }, { c: 'HOME', s: 'Cinema 9' }];
  const t = page({ set: { rank } }); ok(t.order().join() === 'Vue,HOME · Cinema 2', 'load: gone names aren’t shown');
  t.$('pobj').onchange(); ok(JSON.stringify(t.saved().set.rank) === JSON.stringify([{ c: 'Closed Cinema' }, { ch: 'Vue' }, { c: 'HOME', s: 'Cinema 2' }, { c: 'HOME', s: 'Cinema 9' }]), 'load: gone names are kept, junk and repeats dropped ' + JSON.stringify(t.saved().set.rank)); }
// penalties: places among the names that count, evenly up to 90, unlisted 90, the most specific entry wins
{ const t = page({ set: { rank: [{ c: 'Closed Cinema' }, { ch: 'Vue' }, { c: 'Gone', s: 'Screen 1' }, { ch: 'Odeon' }] } }); await t.plan(); const a = t.pens();
  ok(a[1] === 0 && a[2] === 0 && a[0] === 45 && a[3] === 90 && a[6] === 90, 'pen: gone names take no place; chain entries; unlisted last ' + JSON.stringify(a));
  const u = page({ set: { rank: [{ ch: 'Independent' }, { c: 'HOME', s: 'Cinema 1' }, { c: 'Vue Mid' }, { c: 'HOME' }] } }); await u.plan(); const b = u.pens();
  ok(b[3] === 23 && b[4] === 68 && b[5] === 68 && b[1] === 45 && b[0] === 90, 'pen: a screen beats its cinema beats its type, wherever they sit ' + JSON.stringify(b));
  const v = page({ set: { rank: [{ c: 'HOME', s: 'Cinema 1' }, { c: 'HOME', s: 'Cinema 2' }] } }); await v.plan(); const c = v.pens();
  ok(c[3] === 0 && c[4] === 45 && c[5] === 90 && !('p' in v.P.films[0].days[0][3]), 'pen: HOME Cinema 1, else Cinema 2, the rest unlisted; a first choice sends no p ' + JSON.stringify(c)); }
// the plan itself: travel shown is real, the note is brief, an unranked plan keeps its words
{ const plain = page(), hp = await plain.plan(); ok(/Odeon Near/.test(hp), 'plan: unranked goes to the nearest');
  const r = page({ set: { rank: [{ ch: 'Independent' }] } }), hr = await r.plan(), only = page({ set: { cin: ['HOME'] } }), ho = await only.plan();
  const tt = h => /([\d h]+ ?(?:min)?) total travel/.exec(h)[1];
  ok(/<span class="ftm">[^]*HOME/.test(hr) && !/Odeon Near/.test(hr), 'plan: the ranked type beats a cinema 34 minutes nearer each way');
  ok(tt(hr) === tt(ho) && tt(hr) !== tt(hp), `plan: total travel is the journey alone, ${tt(hr)}, as if HOME were simply chosen`);
  ok(/1 of 1 at your ranked choices/.test(hr) && !/ranked/.test(ho), 'plan: says so briefly');
  ok(/better balances your ranking/.test(hr) && /needs less travel and waiting/.test(ho), 'plan: the status tooltip follows');
  const z = page({ set: { rank: [{ c: 'Reel Far' }] } }), hz = await z.plan(); ok(/Odeon Near/.test(hz) && /0 of 1 at your ranked choices/.test(hz), 'plan: a far-off favourite loses to a much shorter journey, and the note is honest');
  ok(!plain.urls.length && !r.urls.length, 'plan: no network'); }
// adding: chains and cinemas up front, screens by name and only where the listings have them
{ const t = page(), h0 = t.find('');
  ok(/Odeon/.test(h0) && /Reel Far/.test(h0) && !/ · /.test(h0.replace(/<span class="meta">.*?<\/span>/g, '')), 'add: no typing: types and cinemas, no screens');
  ok(/HOME · Cinema 1/.test(t.find('home')) && /HOME · Cinema 3/.test(t.find('home 3')) && !/HOME · Cinema 1/.test(t.find('home 3')), 'add: a cinema’s name brings its screens, words in any order');
  ok(!/ · /.test(t.find('reel')) && /Reel Far/.test(t.find('reel')), 'add: no screens where the listings have none');
  ok(t.find('vue').indexOf('Screen 2') < t.find('vue').indexOf('Screen 10'), 'add: screens in number order');
  ok(/Nothing to add/.test(t.find('zzz')), 'add: nothing found');
  t.add('home 1'); t.add('home 2'); t.add('odeon'); ok(t.order().join() === 'HOME · Cinema 1,HOME · Cinema 2,Odeon', 'add: in the order added ' + t.order());
  ok(!/HOME · Cinema 1/.test(t.find('home')), 'add: an entry already listed isn’t offered');
  ok(JSON.stringify(t.saved().set.rank[0]) === '{"c":"HOME","s":"Cinema 1"}', 'add: saved'); ok(t.$('prksum').textContent === '3 ranked' && !t.$('prkclr').hidden, 'add: summary');
  const s = page({ set: { rank: [{ ch: 'Vue' }] } }); ok(s.$('prksum').textContent === 'Vue', 'add: one or two are named'); ok(page().$('prksum').textContent === 'No preference' && page().$('prkclr').hidden, 'add: empty list'); }
// reordering among what's shown: typed numbers like the picks list, ↑ ↓, and hidden names stay put
{ const names = ['Odeon', 'Vue', 'Reel'], gone = { c: 'Closed' }, mk = () => page({ set: { rank: [{ ch: 'Odeon' }, gone, { ch: 'Vue' }, { ch: 'Reel' }] } }), ord = t => t.saved().set.rank.map(x => x.ch || x.c).join();
  let t = mk(); ok(t.order().join() === names.join(), 'move: start'); t.act('dn', 0); ok(t.order().join() === 'Vue,Odeon,Reel' && ord(t).includes('Closed'), 'move: down');
  t = mk(); t.act('up', 2); ok(t.order().join() === 'Odeon,Reel,Vue', 'move: up'); t.act('up', 0); ok(t.order().join() === 'Odeon,Reel,Vue', 'move: top can’t go up');
  t = mk(); ok(t.type('3', 0) === '3' && t.order().join() === 'Vue,Reel,Odeon', 'move: typed down'); t = mk(); t.type('1', 2); ok(t.order().join() === 'Reel,Odeon,Vue', 'move: typed up');
  t = mk(); ok(t.type('99', 0) === '3' && t.order().join() === 'Vue,Reel,Odeon', 'move: too big goes last');
  for (const v of ['0', 'x', '2a', '']) { t = mk(); ok(t.type(v, 1) === '2' && t.order().join() === names.join(), `move: "${v}" puts the number back`); }
  t = mk(); t.act('rm', 1); ok(t.order().join() === 'Odeon,Reel' && ord(t) === 'Odeon,Closed,Reel', 'move: remove takes the right one, not a hidden one ' + ord(t));
  t.$('prkclr').onclick(); ok(!t.order().length && !t.saved().set.rank.length, 'move: clearing clears hidden names too, like “Any cinema”');
  t = mk(); await t.plan(); t.act('dn', 0); ok(t.saved().set.rank[0].ch !== 'Odeon', 'move: saved at once'); }
console.log(fails ? fails + ' FAILED' : 'ALL PASSED');
