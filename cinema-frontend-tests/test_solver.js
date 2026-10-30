// Cinema Planner unit tests (solver, screen types, Google Routes client) against the files the site serves.
// Run from anywhere:  node cinema-frontend-tests/test_solver.js  (or: deno run --allow-read cinema-frontend-tests/test_solver.js)
// (Lives outside static/ so Hugo never publishes it.)
const fs = globalThis.Deno ? null : await import('node:fs');
const read = f => { const u = new URL('../static/cinema/' + f, import.meta.url); return fs ? fs.readFileSync(u, 'utf8') : Deno.readTextFileSync(u); };
const FILES = { SOLVER: read('solver.js'), ST: read('screentypes.js'), ROUTES: read('routes.js'), DATES: read('dates.js'), PLANNER: read('planner.js') };
const src = FILES.SOLVER;
const SOLVER = new Function(src + '\nreturn SOLVER;')();
// the Web Worker is built from SOLVER.toString(): make sure that stringified source works on its own too
{ const W = new Function('const S=(' + SOLVER.toString() + ')();return S;')();
  if (typeof W.solve !== 'function') throw new Error('SOLVER.toString() is not self-contained'); }
const { solve } = SOLVER();
let fails = 0;
const ok = (c, m) => { if (!c) { fails++; console.log('FAIL', m); } else console.log('ok  ', m); };
// helper: nc cinemas, travel fn (a,b)->mins or -1; home index = nc
function mkT(nc, f) { const N = nc + 1, T = new Int32Array(N * N); for (let a = 0; a < N; a++) for (let b = 0; b < N; b++) T[a * N + b] = a === b ? 0 : f(a, b); return T; }
const hh = s => { const [h, m] = s.split(':').map(Number); return h * 60 + m; };
let sid = 0; const SH = [];
const show = (c, st, len) => { const s = { i: sid++, c, st: hh(st), en: hh(st) + len }; SH.push(s); return s; };
const film = (nd, byDay) => ({ days: Array.from({ length: nd }, (_, d) => byDay[d] || []) });
const W = (e, l) => ({ e: hh(e), l: hh(l) });
const base = { buf: 10, late: 0, maxDay: 0, budget: 2000 };

// 1) one day, one cinema (travel home 20). A 12:00-14:00, B 14:05-16:00 (clash w/ buffer), B also 14:30-16:30, C 16:00-18:00 only
{ const T = mkT(1, () => 20);
  const A = film(1, { 0: [show(0, '12:00', 120)] }), B = film(1, { 0: [show(0, '14:05', 115), show(0, '14:30', 120)] }), C = film(1, { 0: [show(0, '16:00', 120)] });
  const r = solve({ ...base, films: [A, B, C], win: [W('10:00', '23:00')], T, nc: 1, mode: 'most' });
  ok(r.status === 'optimal' && r.chosen.length === 2, 'case1 most: 2 films (A+C or A+B@14:30) ' + JSON.stringify(r.chosen));
  ok(r.chosen.includes(0) && r.chosen.includes(1), 'case1 priority tie-break prefers A+B over A+C');
  ok(r.days[0].seq.length === 2 && SH[r.days[0].seq[1]].st === hh('14:30'), 'case1 picks the 14:30 B');
}
// 2) strict vs most differ: film0 only at 13:00-17:00; film1 at 12:00-14:00, film2 at 14:30-16:30, film3 16:40-18:40 (all same cinema, 1 day)
{ const T = mkT(1, () => 10);
  const f0 = film(1, { 0: [show(0, '13:00', 240)] }), f1 = film(1, { 0: [show(0, '12:00', 120)] }), f2 = film(1, { 0: [show(0, '14:30', 120)] }), f3 = film(1, { 0: [show(0, '17:15', 120)] });
  const P = { ...base, films: [f0, f1, f2, f3], win: [W('10:00', '23:30')], T, nc: 1 };
  const m = solve({ ...P, mode: 'most' }), s = solve({ ...P, mode: 'strict' });
  ok(m.chosen.join() === '1,2,3', 'case2 most-films takes 1,2,3 -> ' + m.chosen);
  ok(s.chosen[0] === 0 && s.chosen.length === 2 && s.chosen.includes(3), 'case2 strict keeps film0 (+film3) -> ' + s.chosen);
  ok(m.unfit[0] && m.unfit[0].higher === true, 'case2 most: film0 reason = fits with higher, dropped for count');
}
// 3) unreachable legs: cinema0 <-> cinema1 null; home->both 20. f0 only at c0 12:00-14:00, f1 only at c1 15:00-17:00 -> can't do both same day; 1 day
{ const T = mkT(2, (a, b) => (a < 2 && b < 2) ? -1 : 20);
  const f0 = film(1, { 0: [show(0, '12:00', 120)] }), f1 = film(1, { 0: [show(1, '15:00', 120)] });
  const r = solve({ ...base, films: [f0, f1], win: [W('10:00', '23:30')], T, nc: 2, mode: 'most' });
  ok(r.chosen.length === 1 && r.chosen[0] === 0, 'case3 unreachable leg blocks second film');
  const r2 = solve({ ...base, films: [f0, f1], win: [W('10:00', '23:30'), W('10:00', '23:30')], T, nc: 2, mode: 'most' });
  ok(r2.chosen.length === 1, 'case3b f1 has no showing day 2 -> still 1');
  const f1b = film(2, { 0: [show(1, '15:00', 120)], 1: [show(1, '15:00', 120)] }), f0b = film(2, { 0: [show(0, '12:00', 120)] });
  const r3 = solve({ ...base, films: [f0b, f1b], win: [W('10:00', '23:30'), W('10:00', '23:30')], T, nc: 2, mode: 'most' });
  ok(r3.chosen.length === 2 && r3.days.length === 2, 'case3c splits across 2 days');
}
// 4) day windows: home by 22:00 with 30 min home travel: film ending 21:40 fails (21:40+10+30 > 22:00), ending 21:15 ok
{ const T = mkT(1, () => 30);
  const f0 = film(1, { 0: [show(0, '19:00', 160)] }), f1 = film(1, { 0: [show(0, '19:00', 135)] });
  const r = solve({ ...base, films: [f0, f1], win: [W('10:00', '22:00')], T, nc: 1, mode: 'most' });
  ok(r.chosen.join() === '1', 'case4 latest-home window enforced (+buffer exit)');
  const f2 = film(1, { 0: [show(0, '10:15', 100)] });
  const r2 = solve({ ...base, films: [f2], win: [W('10:00', '22:00')], T, nc: 1, mode: 'most' });
  ok(r2.chosen.length === 0, 'case4b earliest-leave window enforced (10:00+30 > 10:15)');
  const r3 = solve({ ...base, late: 15, films: [f2], win: [W('10:00', '22:00')], T, nc: 1, mode: 'most' });
  ok(r3.chosen.length === 1, 'case4c trailer allowance lets you arrive 15 min late');
  const r4 = solve({ ...base, films: [f2], win: [null], T, nc: 1, mode: 'most' });
  ok(r4.chosen.length === 0, 'case4d unavailable day');
}
// 5) travel tie-break: same film at near cinema (5 min) and far cinema (60 min) same time -> choose near
{ const T = mkT(2, (a, b) => (a === 1 || b === 1) && (a === 2 || b === 2) ? 60 : (a === 0 || b === 0) && (a === 2 || b === 2) ? 5 : 50);
  const f0 = film(1, { 0: [show(1, '14:00', 120), show(0, '14:00', 120)] });
  const r = solve({ ...base, films: [f0], win: [W('10:00', '23:30')], T, nc: 2, mode: 'most' });
  ok(SH[r.days[0].seq[0]].c === 0 && r.days[0].cost === 10, 'case5 least travel showing chosen (cost 10)');
}
// 6) order within day can be non-priority (later-ranked film first) + insertion ordering
{ const T = mkT(1, () => 15);
  const f0 = film(1, { 0: [show(0, '18:00', 120)] }), f1 = film(1, { 0: [show(0, '13:00', 120)] }), f2 = film(1, { 0: [show(0, '15:20', 120)] });
  const r = solve({ ...base, films: [f0, f1, f2], win: [W('10:00', '23:30')], T, nc: 1, mode: 'most' });
  ok(r.chosen.length === 3 && r.days[0].seq.map(i => SH[i].st).join() === [hh('13:00'), hh('15:20'), hh('18:00')].join(), 'case6 sequence ordered by time');
}
// 7) maxDay=1 forces spread over days
{ const T = mkT(1, () => 15);
  const mk = () => film(2, { 0: [show(0, '12:00', 100)], 1: [show(0, '12:00', 100)] });
  const r = solve({ ...base, maxDay: 1, films: [mk(), mk(), mk()], win: [W('10:00', '23:30'), W('10:00', '23:30')], T, nc: 1, mode: 'most' });
  ok(r.chosen.length === 2 && r.days.every(d => d.seq.length === 1), 'case7 max films/day respected');
}
// 8) brute-force cross-check on random small instances (count + priority key)
function brute(P) { // enumerate all assignments film->(skip | day,show) with permutations checked via solver-free feasibility
  const n = P.films.length, N = P.nc + 1, tr = (a, b) => a === b ? 0 : P.T[a * N + b];
  let best = -1, bestC = Infinity;
  const feasible = (d, list) => { // list of shows; try all orders
    const w = P.win[d]; if (!w) return Infinity; if (list.length > (P.maxDay || 99)) return Infinity;
    const perms = a => a.length < 2 ? [a] : a.flatMap((x, i) => perms([...a.slice(0, i), ...a.slice(i + 1)]).map(p => [x, ...p]));
    let mc = Infinity; perms(list).forEach(o => { let loc = P.nc, t = w.e, first = true, c = 0;
      for (const s of o) { const tt = tr(loc, s.c); if (tt < 0) return;
        const arr = first ? t + tt : t + P.buf + tt; if (arr > s.st + P.late) return;
        c += tt + (first ? 0 : Math.max(0, s.st - arr)); t = s.en; loc = s.c; first = false; }
      const th = tr(loc, P.nc); if (th >= 0 && t + P.buf + th <= w.l) mc = Math.min(mc, c + th); });
    return mc;
  };
  const opts = P.films.map(f => { const o = [null]; f.days.forEach((l, d) => l.forEach(s => o.push([d, s]))); return o; });
  const pick = new Array(n);
  (function rec(i) { if (i === n) { const byD = new Map(); for (const x of pick) if (x) (byD.get(x[0]) || byD.set(x[0], []).get(x[0])).push(x[1]);
      let cost = 0; for (const [d, l] of byD) { const c = feasible(d, l); if (c === Infinity) return; cost += c; }
      let cnt = 0, mv = 0; pick.forEach((x, f) => { if (x) { cnt++; mv += 2 ** (n - 1 - f); } });
      const k = P.mode === 'strict' ? mv : cnt * 2 ** 28 + mv; if (k > best || (k === best && cost < bestC)) { best = k; bestC = cost; } return; }
    for (const o of opts[i]) { pick[i] = o; rec(i + 1); } })(0);
  return [best, bestC];
}
function closure(T, N) { for (let k = 0; k < N; k++) for (let a = 0; a < N; a++) { const ak = T[a * N + k]; if (ak < 0) continue;
  for (let b = 0; b < N; b++) { const kb = T[k * N + b]; if (kb < 0) continue; const v = ak + kb, ab = T[a * N + b]; if (ab < 0 || v < ab) T[a * N + b] = v; } } }
let seed = 7; const rnd = () => (seed = (seed * 16807) % 2147483647) / 2147483647;
let agree = 0, tot = 0, costEq = 0; const costWorse = [];
for (let it = 0; it < 400; it++) {
  const nc = 3, nd = 2, n = 3 + Math.floor(rnd() * 3);
  const M = Array.from({ length: nc + 1 }, () => Array.from({ length: nc + 1 }, () => rnd() < 0.1 ? -1 : 5 + Math.floor(rnd() * 50)));
  const T = mkT(nc, (a, b) => M[a][b]); closure(T, nc + 1);
  const films = Array.from({ length: n }, () => film(nd, Object.fromEntries(Array.from({ length: nd }, (_, d) => [d, Array.from({ length: Math.floor(rnd() * 3) }, () => { const st = 600 + Math.floor(rnd() * 600); const s = { i: sid++, c: Math.floor(rnd() * nc), st, en: st + 90 + Math.floor(rnd() * 60) }; return s; })]))));
  const mode = rnd() < 0.5 ? 'most' : 'strict', late = rnd() < 0.3 ? 15 : 0;
  const P = { films, win: [W('10:00', '23:30'), rnd() < 0.2 ? null : W('11:00', '22:30')], T, nc, buf: 10, late, maxDay: rnd() < 0.2 ? 2 : 0, mode, budget: 2000 };
  const r = solve(P); let k = 0; r.chosen.forEach(f => k += 2 ** (n - 1 - f)); if (mode !== 'strict') k += r.chosen.length * 2 ** 28;
  const [b, bc] = brute(P); tot++; const rc = r.days.reduce((a, d) => a + d.cost, 0); if (b === k) { if (Math.abs(rc - bc) < 1e-6) costEq++; else costWorse.push(rc - bc); } if (b === k) agree++; else { console.log("mismatch", it, mode, b, k, r.chosen); if (it === 143) console.log(JSON.stringify({P: {...P, T: Array.from(T)}, r}, null, 0)); }
}
console.log('cost-optimal', costEq, '/', tot, 'worse by', costWorse);
ok(agree === tot, `case8 random brute-force cross-check ${agree}/${tot}`);

// 8b) up to 40 films: film sets no longer fit in 32-bit ints. Checks use films with indices >= 31.
const OLD = (() => { const u = new URL('./solver_32bit_ref.js', import.meta.url);
  return new Function((fs ? fs.readFileSync(u, 'utf8') : Deno.readTextFileSync(u)) + '\nreturn SOLVER;')()().solve; })();
const strip = r => JSON.stringify({ ...r, ms: 0 });
const nd = (n, d) => Array.from({ length: n }, (_, i) => d);
// (1) n <= 28: identical output (schedule, unfit reasons, status, node count) to the old 32-bit solver
function randInst(n, ndays, nc, perDay, mode) {
  const M = Array.from({ length: nc + 1 }, () => Array.from({ length: nc + 1 }, () => rnd() < 0.08 ? -1 : 5 + Math.floor(rnd() * 40)));
  const T = mkT(nc, (a, b) => M[a][b]); closure(T, nc + 1);
  const films = Array.from({ length: n }, () => film(ndays, Object.fromEntries(Array.from({ length: ndays }, (_, d) => [d, Array.from({ length: Math.floor(rnd() * perDay) }, () => {
    const st = 600 + Math.floor(rnd() * 660); return { i: sid++, c: Math.floor(rnd() * nc), st, en: st + 85 + Math.floor(rnd() * 70) }; })]))));
  const win = Array.from({ length: ndays }, () => rnd() < 0.15 ? null : W(rnd() < 0.5 ? '10:00' : '13:00', rnd() < 0.5 ? '23:30' : '21:00'));
  return { films, win, T, nc, buf: 10, late: rnd() < 0.3 ? 15 : 0, maxDay: rnd() < 0.2 ? 2 : 0, mode, budget: 20000 };
}
{ seed = 4242; let same = 0, cmp = 0, maxN = 0;
  for (let it = 0; it < 60; it++) {
    const n = it < 40 ? 4 + Math.floor(rnd() * 8) : 20 + Math.floor(rnd() * 9), P = n > 12 ? randInst(n, 7, 4, 1.25, rnd() < 0.5 ? 'most' : 'strict') : randInst(n, 3, 3, 2.6, rnd() < 0.5 ? 'most' : 'strict');
    const a = OLD({ ...P }), b = solve({ ...P });
    if (a.status !== 'optimal' || b.status !== 'optimal') continue;   // timing-dependent otherwise
    cmp++; maxN = Math.max(maxN, n); if (strip(a) === strip(b)) same++; else console.log('differs', it, n, a.chosen, b.chosen, a.nodes, b.nodes);
  }
  ok(cmp >= 50 && same === cmp, `case8b n<=28 identical to the old solver on ${same}/${cmp} seeded instances (largest n=${maxN})`);
}
// (2a) 40 films x 10 days, one showing each, 4 per day at 10:00/13:00/16:00/19:00 (2 h, same cinema): everything fits
const grid = (f, d, slot, len = 120) => ({ i: 10000 + f, c: 0, st: 600 + slot * 180, en: 600 + slot * 180 + len, d });
function gridInst(mode, tweak) {
  const sh = Array.from({ length: 40 }, (_, f) => grid(f, Math.floor(f / 4), f % 4)); if (tweak) tweak(sh);
  return { films: sh.map(s => film(10, { [s.d]: [{ i: s.i, c: s.c, st: s.st, en: s.en }] })), win: nd(10, W('09:00', '23:59')), T: mkT(1, () => 15), nc: 1, buf: 10, late: 0, maxDay: 0, mode, budget: 2500 };
}
for (const mode of ['most', 'strict']) {
  const r = solve(gridInst(mode));
  ok(r.status === 'optimal' && r.chosen.length === 40 && r.chosen.join() === Array.from({ length: 40 }, (_, i) => i).join() && r.days.length === 10 && r.days.every(d => d.seq.length === 4),
    `case8c ${mode}: 40 films x 10 days all scheduled (${r.chosen.length}, ${r.status}, ${r.ms} ms)`);
  ok(r.days.find(d => d.d === 9).seq.join() === '10036,10037,10038,10039' && r.days.every(d => d.cost === 180), `case8c ${mode}: films 36-39 on the last day, cost 180 per day (15+15 travel + 3x50 wait)`);
}
// (2b) film 32 moves to day 9 as one long 10:00-17:30 showing, clashing with films 36, 37, 38 (all ranked lower)
//      most: skip 32 (-1) rather than 36-38 (-3) -> 39 films; strict: 32 beats 36-38 -> 32 + 39 on day 9 -> 37 films
{ const tw = sh => { sh[32] = { ...grid(32, 9, 0, 450) }; };
  const m = solve(gridInst('most', tw)), s = solve(gridInst('strict', tw));
  ok(m.status === 'optimal' && m.chosen.length === 39 && !m.chosen.includes(32) && m.chosen.includes(36) && m.chosen.includes(39) && m.unfit[32] && m.unfit[32].higher === true,
    'case8d most: drops film 32 for 36-38 ' + JSON.stringify(m.unfit));
  ok(s.status === 'optimal' && s.chosen.length === 37 && s.chosen.includes(32) && s.chosen.includes(39) && ![36, 37, 38].some(f => s.chosen.includes(f)) && s.unfit[36].higher === false,
    'case8d strict: keeps film 32, drops 36-38 ' + JSON.stringify(s.unfit));
  ok(s.days.find(d => d.d === 9).seq.join() === '10032,10039' && s.days.find(d => d.d === 8).seq.join() === '10033,10034,10035', 'case8d strict day plans');
  // two high-ranked showings at the same time on day 8: 33 (higher) wins in both modes
  const tw2 = sh => { sh[38] = { ...grid(38, 8, 1) }; };
  for (const mode of ['most', 'strict']) { const r = solve(gridInst(mode, tw2));
    ok(r.status === 'optimal' && r.chosen.length === 39 && r.chosen.includes(33) && !r.chosen.includes(38), `case8e ${mode}: same-time clash 33 vs 38 -> 33`); }
}
// (2c) random small instances padded to 36-40 films with 31+ films that have no usable showing first, so the real films
//      get indices >= 31; their result must match brute force (and the unpadded solve) after the index shift
{ seed = 99; let agree2 = 0, tot2 = 0;
  for (let it = 0; it < 60; it++) {
    const nc = 3, n = 3 + Math.floor(rnd() * 3), pad = 31 + Math.floor(rnd() * (40 - n - 30));
    const M = Array.from({ length: nc + 1 }, () => Array.from({ length: nc + 1 }, () => rnd() < 0.1 ? -1 : 5 + Math.floor(rnd() * 50)));
    const T = mkT(nc, (a, b) => M[a][b]); closure(T, nc + 1);
    const films = Array.from({ length: n }, () => film(2, Object.fromEntries([0, 1].map(d => [d, Array.from({ length: Math.floor(rnd() * 3) }, () => { const st = 600 + Math.floor(rnd() * 600); return { i: sid++, c: Math.floor(rnd() * nc), st, en: st + 90 + Math.floor(rnd() * 60) }; })]))));
    const mode = rnd() < 0.5 ? 'most' : 'strict', P = { films, win: [W('10:00', '23:30'), W('11:00', '22:30')], T, nc, buf: 10, late: 0, maxDay: 0, mode, budget: 2000 };
    // padding: no showings at all, or only on an unavailable 3rd day
    const dead = Array.from({ length: pad }, (_, k) => k % 2 ? film(3, {}) : film(3, { 2: [{ i: sid++, c: 0, st: 700, en: 800 }] }));
    const r = solve({ ...P, films: [...dead, ...films.map(f => ({ days: [...f.days, []] }))], win: [...P.win, null] });
    const [b, bc] = brute(P), got = r.chosen.map(f => f - pad);
    let k = 0; got.forEach(f => k += 2 ** (n - 1 - f)); if (mode !== 'strict') k += got.length * 2 ** 28;
    const rc = r.days.reduce((a, d) => a + d.cost, 0); tot2++;
    if (k === b && Math.abs(rc - bc) < 1e-6 && r.chosen.every(f => f >= 31) && r.status === 'optimal') agree2++; else console.log('padded mismatch', it, mode, b, k, bc, rc, r.chosen);
  }
  ok(agree2 === tot2, `case8f films at indices 31-39 match brute force (count, priority, travel) ${agree2}/${tot2}`);
}
// (2d) two independent halves: days 0-4 only show films 0-19, days 5-9 only films 20-39. Both objectives decompose,
//      so the 40-film optimum = the two 20-film optima side by side (solved separately with the old 32-bit solver)
{ seed = 2024; let good = 0, runs = 0;
  for (let it = 0; it < 6; it++) {
    const mode = it % 2 ? 'strict' : 'most', A = randInst(20, 5, 4, 1.2, mode), Bp = randInst(20, 5, 4, 1.2, mode);
    const Tm = A.T;   // share A's travel matrix
    const P = { ...A, budget: 20000, films: [...A.films.map(f => ({ days: [...f.days, ...nd(5, [])] })), ...Bp.films.map(f => ({ days: [...nd(5, []), ...f.days] }))], win: [...A.win, ...Bp.win], T: Tm };
    const ra = OLD({ ...A, budget: 20000 }), rb = OLD({ ...Bp, T: Tm, buf: A.buf, late: A.late, maxDay: A.maxDay, budget: 20000 }), r = solve(P);
    if (ra.status !== 'optimal' || rb.status !== 'optimal') continue; runs++;
    const want = [...ra.chosen, ...rb.chosen.map(f => f + 20)].join(), wc = ra.days.reduce((a, d) => a + d.cost, 0) + rb.days.reduce((a, d) => a + d.cost, 0), rc = r.days.reduce((a, d) => a + d.cost, 0);
    if (r.chosen.join() === want && Math.abs(rc - wc) < 1e-6 && r.status === 'optimal') good++; else console.log('halves mismatch', it, mode, r.status, want, r.chosen.join(), wc, rc);
  }
  ok(runs >= 4 && good === runs, `case8g 40 films = two independent 20-film halves ${good}/${runs}`);
}
// (3) performance: realistic 40 films x 10 days (12 cinemas, several showings per film per day) stays within the budget,
//     and a 28-film run is about as fast as with the old solver
function realInst(n, ndays, mode, budget) { seed = 777;
  const nc = 12, xy = Array.from({ length: nc + 1 }, () => [rnd() * 30, rnd() * 30]);
  const T = mkT(nc, (a, b) => Math.round(10 + 2.2 * Math.hypot(xy[a][0] - xy[b][0], xy[a][1] - xy[b][1])));
  const films = Array.from({ length: n }, (_, f) => { const pop = f < 10 ? 6 : f < 25 ? 3 : 1.5;
    return film(ndays, Object.fromEntries(Array.from({ length: ndays }, (_, d) => [d, Array.from({ length: Math.floor(rnd() * pop * 2) }, () => {
      const st = 600 + Math.floor(rnd() * 44) * 15; return { i: sid++, c: Math.floor(rnd() * nc), st, en: st + 95 + Math.floor(rnd() * 80) }; })]))); });
  return { films, win: Array.from({ length: ndays }, (_, d) => d % 7 < 5 ? W('17:00', '23:59') : W('10:00', '23:59')), T, nc, buf: 10, late: 10, maxDay: 0, mode, budget };
}
for (const mode of ['most', 'strict']) {
  const P = realInst(40, 10, mode, 2500), t = Date.now(), r = solve(P), ms = Date.now() - t;
  const valid = r.days.every(d => d.seq.length && d.seq.length <= r.cap[d.d]) && new Set(r.chosen).size === r.chosen.length && r.days.reduce((a, d) => a + d.seq.length, 0) === r.chosen.length;
  console.log(`   perf 40x10 ${mode}: ${r.status}, ${r.chosen.length}/40 films (max index ${Math.max(...r.chosen)}), ${ms} ms, ${r.nodes} nodes, memo ${r.memo}`);
  ok(ms <= 2500 + 800 && ['optimal', 'films', 'timeout'].includes(r.status) && r.chosen.length >= 20 && r.chosen.some(f => f >= 31) && valid, `case8h 40x10 ${mode} within budget (${ms} ms, ${r.status})`);
}
{ const P = realInst(28, 7, 'most', 2500); let to = 0, tn = 0; const ro = [], rn = [];
  for (let k = 0; k < 2; k++) { let t = Date.now(); ro.push(OLD({ ...P })); to += Date.now() - t; t = Date.now(); rn.push(solve({ ...P })); tn += Date.now() - t; }
  console.log(`   perf 28x7: old ${ro.map(r => r.status + ' ' + r.chosen.length + ' films ' + r.nodes + ' nodes ' + r.ms + ' ms').join(' | ')}`);
  console.log(`   perf 28x7: new ${rn.map(r => r.status + ' ' + r.chosen.length + ' films ' + r.nodes + ' nodes ' + r.ms + ' ms').join(' | ')}`);
  ok(rn.every(r => r.status === ro[0].status && r.chosen.length === ro[0].chosen.length), 'case8i 28x7: same status and film count as the old solver');
  // same-work speed check: a smaller instance both solvers finish (fixed node count), compare wall time
  const Q = realInst(24, 6, 'most', 60000); let qo = 0, qn = 0, a, b;
  for (let k = 0; k < 3; k++) { let t = Date.now(); a = OLD({ ...Q }); qo += Date.now() - t; t = Date.now(); b = solve({ ...Q }); qn += Date.now() - t; }
  console.log(`   perf same-work (n=24, 6 days, ${a.nodes} nodes, ${a.status}): old ${qo / 3 | 0} ms, new ${qn / 3 | 0} ms`);
  ok(strip(a) === strip(b) && qn <= qo * 1.5 + 50, `case8j same work: identical result, new ${qn / 3 | 0} ms vs old ${qo / 3 | 0} ms`);
}
// too many films is a clear error, not a wrong answer
{ let e = null; try { solve({ ...gridInst('most'), films: Array.from({ length: 48 }, () => film(10, {})) }); } catch (x) { e = x; }
  ok(e && /at most 47 films/.test(e.message), 'case8k >47 films throws'); }

// 9) screen types: one combined label per showing, canonical order, 3D last; pick filtering by p.st
{ const xs = s => FILES[s];
  const { screenType, otherTags, stCmp, pickAllows, isScreen } = new Function(xs('ST') + '\nreturn SCREENTYPES;')()(new Set(['35mm','3D','4DX','4K','70mm','D-BOX','Dolby Atmos','Dolby Cinema','EPIC','IMAX','IMAX 1.43:1','Infinity Vision','Luxe','ScreenX','Superscreen','Ultra Lux','XPlus','iSense']));
  ok(screenType(['3D', '4DX', 'Classic']) === '4DX 3D', 'st: 3D+4DX+Classic -> "4DX 3D" ' + screenType(['3D', '4DX', 'Classic']));
  ok(screenType(['3D', 'IMAX']) === 'IMAX 3D' && screenType(['IMAX']) === 'IMAX', 'st: IMAX 3D / IMAX');
  ok(screenType(['70mm', 'IMAX', 'IMAX 1.43:1']) === 'IMAX 1.43:1 70mm', 'st: IMAX 1.43:1 absorbs IMAX -> ' + screenType(['70mm', 'IMAX', 'IMAX 1.43:1']));
  ok(screenType(['Dolby Atmos', 'EPIC']) === 'EPIC Dolby Atmos' && screenType(['Infinity Vision', 'XPlus']) === 'XPlus Infinity Vision', 'st: EPIC Dolby Atmos, XPlus Infinity Vision');
  ok(screenType([]) === 'Standard' && screenType(['Subtitled', 'Hindi']) === 'Standard', 'st: no screen tags -> Standard');
  ok(otherTags(['Subtitled', '3D', '4DX', 'Classic']).join() === 'Classic,Subtitled' && isScreen('3D') && !isScreen('Subtitled'), 'st: other tags separate');
  ok(['4DX 3D', 'Standard', 'IMAX 3D', 'IMAX', 'Luxe'].sort(stCmp).join('|') === 'Standard|IMAX|IMAX 3D|4DX 3D|Luxe', 'st: canonical sort ' + ['4DX 3D', 'Standard', 'IMAX 3D', 'IMAX', 'Luxe'].sort(stCmp).join('|'));
  const shows = [[], ['IMAX'], ['IMAX', '3D'], ['4DX', '3D', 'Classic'], ['Luxe', 'Subtitled'], ['Subtitled']];
  const m = p => shows.filter(x => pickAllows({ st: [], req: [], exc: [], dd: null, ...p }, x, 0, '2026-10-08', null)).length;
  ok(m({}) === 6, 'pick: no filter = all');
  ok(m({ st: ['IMAX 3D'] }) === 1 && m({ st: ['IMAX', 'IMAX 3D'] }) === 2 && m({ st: ['Standard'] }) === 2, 'pick: st exact match (IMAX 3D=1, IMAX+IMAX 3D=2, Standard=2)');
  ok(m({ req: ['3D'] }) === 2 && m({ exc: ['3D'] }) === 4, 'pick: "any 3D" via req/exc tag still works');
  ok(m({ st: ['Standard'], exc: ['Subtitled'] }) === 1 && m({ st: ['Luxe'], req: ['Subtitled'] }) === 1, 'pick: st combined with other-tag chips');
  ok(m({ req: ['IMAX'] }) === 2, 'pick: old saved req IMAX keeps working (raw tags)');
  ok(m({ dd: ['2026-10-08'] }) === 0 && m({ dd: ['2026-10-09', '2026-10-17'] }) === 6 && pickAllows({ st: [], req: [], exc: [], dd: null }, [], 2, '2026-10-08', new Set([1])) === false, 'pick: excluded-date + cinema filters');
  ok(pickAllows({ st: [], req: [], exc: [], wd: [1] }, [], 0, '2026-10-08', null), 'pick: old saved weekday list (wd) is ignored');
  ok(pickAllows({ cin: [] }, ['IMAX'], 0, 0, null), 'pick: old pick object without st/req/exc fields');
}
// 10) Google Routes client against a fake Routes API: headers, body, chunking, ROUTE_NOT_FOUND, streamed body, errors
{ const xs = s => FILES[s];
  const G = new Function(xs('ROUTES') + '\nreturn GROUTES;')()();
  const pts = Array.from({ length: 151 }, (_, i) => ({ lat: 53.4 + i * 0.01, lng: -2.2 }));
  const need = []; for (let j = 1; j < 151; j++) need.push({ i: 0, j, t: true, w: G.near(pts[0], pts[j]) });
  need.push({ i: 5, j: 0, t: true, w: false });
  const calls = [];
  const fake = async (url, o) => { const b = JSON.parse(o.body); calls.push({ url, o, b });
    const els = b.destinations.map((d, k) => { const km = Math.abs(d.waypoint.location.latLng.latitude - b.origins[0].waypoint.location.latLng.latitude) * 111;
      const e = { originIndex: 0, destinationIndex: k, condition: 'ROUTE_EXISTS', duration: Math.round((b.travelMode === 'WALK' ? km * 12 : 20 + km * 2) * 60) + 's' };
      if (b.travelMode === 'TRANSIT' && k === 3) return { originIndex: 0, destinationIndex: k, condition: 'ROUTE_NOT_FOUND', status: {} };
      if (k === 0) delete e.destinationIndex; delete e.originIndex; return e; }).reverse();
    const text = calls.length === 2 ? els.map(e => JSON.stringify(e)).join('\n') : JSON.stringify(els);   // 2nd call: streamed-style body
    return { ok: true, status: 200, text: async () => text }; };
  const got = new Map(), prog = [];
  const res = await G.run({ key: 'TESTKEY', pts, need, fetch: fake, onProgress: (d, n) => prog.push(d + '/' + n), onResult: (m, i, j, v) => got.set(m + i + '>' + j, v), depart: '2026-10-08T16:00:00Z', sleep: async () => {} });
  const tr = calls.filter(c => c.b.travelMode === 'TRANSIT'), wk = calls.filter(c => c.b.travelMode === 'WALK');
  ok(res.error === null && res.requests === calls.length, 'routes: no error, ' + calls.length + ' requests');
  ok(tr.length === 3 && tr.every(c => c.b.origins.length * c.b.destinations.length <= 100) && tr.map(c => c.b.destinations.length).sort((a, b) => a - b).join() === '1,50,100', 'routes: TRANSIT chunked to <=100 elements per request ' + tr.map(c => c.b.destinations.length));
  ok(wk.length === 1 && wk[0].b.destinations.length === need.filter(n => n.w).length && !wk[0].b.transitPreferences, 'routes: one WALK request for <3 km pairs (' + wk[0].b.destinations.length + ')');
  ok(calls.every(c => c.url === 'https://routes.googleapis.com/distanceMatrix/v2:computeRouteMatrix' && c.o.method === 'POST' && c.o.headers['X-Goog-Api-Key'] === 'TESTKEY' && c.o.headers['X-Goog-FieldMask'] === 'originIndex,destinationIndex,duration,condition,status' && c.o.headers['Content-Type'] === 'application/json'), 'routes: POST + headers');
  ok(calls.every(c => !c.url.includes('TESTKEY') && !c.url.includes('?') && !c.url.includes('53.')), 'routes: key/coords never in URL');
  ok(tr.every(c => c.b.departureTime === '2026-10-08T16:00:00Z' && JSON.stringify(c.b.transitPreferences.allowedTravelModes) === '["BUS","LIGHT_RAIL","RAIL"]' && c.b.origins[0].waypoint.location.latLng.latitude), 'routes: TRANSIT body (departureTime, allowedTravelModes, latLng waypoints)');
  ok(got.get('T0>1') === Math.ceil(Math.round((20 + 0.01 * 111 * 2) * 60) / 60) && got.get('T0>4') === null && got.get('T0>5') > 0, 'routes: parsed minutes; ROUTE_NOT_FOUND -> null ' + got.get('T0>1') + ' ' + got.get('T0>4'));
  ok(got.get('T5>0') > 0 && [...got.keys()].filter(k => k[0] === 'T').length === 151, 'routes: every TRANSIT pair answered incl. missing origin/destinationIndex = 0');
  ok(got.get('W0>1') > 0 && got.get('W0>1') < got.get('T0>1'), 'routes: WALK minutes parsed');
  ok(prog[prog.length - 1] === calls.length + '/' + calls.length, 'routes: progress reaches ' + prog[prog.length - 1]);
  // parse helpers
  ok(G.elements('[{"duration":"60s","condition":"ROUTE_EXISTS"}]').length === 1 && G.elements('{"a":1}\n{"b":"}{"}').length === 2 && G.elements('').length === 0, 'routes: elements() handles array / streamed objects / empty');
  ok(/^\d{4}-\d\d-\d\dT\d\d:00:00Z$/.test(G.departure()) && new Date(G.departure()).getHours() === 17, 'routes: departure = tomorrow 17:00 local, RFC3339 UTC ' + G.departure());
  // errors: 403 stops immediately and reports; 429 retries then succeeds; network error -> message
  let n403 = 0; const r403 = await G.run({ key: 'BAD', pts, need: need.slice(0, 120), fetch: async () => { n403++; return { ok: false, status: 403, text: async () => '{"error":{"code":403,"message":"Routes API has not been used in project 1 before or it is disabled.","status":"PERMISSION_DENIED"}}' }; }, onResult: () => { throw new Error('no results expected'); }, sleep: async () => {}, concurrency: 1 });
  ok(/HTTP 403: Routes API has not been used/.test(r403.error) && n403 === 1, 'routes: 403 -> clear error, stops after 1 request: ' + r403.error.slice(0, 60));
  let n429 = 0; const got2 = []; const r429 = await G.run({ key: 'K', pts, need: [{ i: 0, j: 1, t: true, w: false }], fetch: async () => (++n429 < 3 ? { ok: false, status: 429, text: async () => '' } : { ok: true, status: 200, text: async () => '[{"condition":"ROUTE_EXISTS","duration":"125s"}]' }), onResult: (m, i, j, v) => got2.push(v), sleep: async () => {} });
  ok(r429.error === null && n429 === 3 && got2[0] === 3, 'routes: 429 retried, then 125s -> 3 min');
  const rnet = await G.run({ key: 'K', pts, need: [{ i: 0, j: 1, t: true, w: false }], fetch: async () => { throw new TypeError('Failed to fetch'); }, sleep: async () => {} });
  ok(/Couldn’t reach/.test(rnet.error), 'routes: network/CORS failure reported');
  const r400 = await G.run({ key: 'K', pts, need: [{ i: 0, j: 1, t: true, w: false }], fetch: async () => ({ ok: false, status: 400, text: async () => '[{"error":{"code":400,"message":"API key not valid. Please pass a valid API key.","status":"INVALID_ARGUMENT"}}]' }), sleep: async () => {} });
  ok(/HTTP 400: API key not valid/.test(r400.error), 'routes: 400 bad key message');
}
// 11) travel choices: request bodies per mode and avoidance, element caps, no walking for bike or car
{ const xs = s => FILES[s];
  const G = new Function(xs('ROUTES') + '\nreturn GROUTES;')()();
  const pts = Array.from({ length: 701 }, (_, i) => ({ lat: 53.4 + i * 0.0001, lng: -2.2 }));
  const need = []; for (let j = 1; j < 701; j++) need.push({ i: 0, j, t: true, w: true });
  const dep = '2026-10-08T16:00:00Z', ask = p => G.requests(need, pts, dep, p), allow = p => ask(p).find(q => q.body.travelMode === 'TRANSIT').body.transitPreferences.allowedTravelModes.join();
  ok(G.prof('T') === 'T' && G.prof('T', false, false) === 'T', 'travel: plain transit keeps the old "T" cache prefix');
  ok(new Set([G.prof('T', 1, 0), G.prof('T', 0, 1), G.prof('T', 1, 1), G.prof('B'), G.prof('D'), 'T', 'W']).size === 7, 'travel: every choice has its own cache prefix');
  ok(G.prof('B', true, true) === 'B' && G.prof('D', true, false) === 'D', 'travel: avoidance is ignored for cycling and driving');
  ok(allow('T') === 'BUS,LIGHT_RAIL,RAIL' && JSON.stringify(ask('T')) === JSON.stringify(G.requests(need, pts, dep)), 'travel: no avoidance = the original request');
  ok(allow(G.prof('T', true, false)) === 'BUS,SUBWAY,TRAIN', 'travel: avoid trams = BUS,SUBWAY,TRAIN (no LIGHT_RAIL, no RAIL)');
  ok(allow(G.prof('T', false, true)) === 'BUS,SUBWAY,LIGHT_RAIL', 'travel: avoid trains = BUS,SUBWAY,LIGHT_RAIL (no TRAIN, no RAIL)');
  ok(allow(G.prof('T', true, true)) === 'BUS,SUBWAY', 'travel: avoid both = BUS,SUBWAY');
  ok(ask('Ttr').filter(q => q.body.travelMode === 'WALK').length > 0 && ask('Ttr').filter(q => q.mode === 'Ttr').every(q => q.body.departureTime === dep && q.dests.length <= 100), 'travel: avoidance keeps WALK, departureTime and the 100-element cap');
  for (const [p, tm] of [['B', 'BICYCLE'], ['D', 'DRIVE']]) { const q = ask(p);
    ok(q.length === 2 && q.every(r => r.mode === p && r.body.travelMode === tm) && q.map(r => r.dests.length).join() === '625,75', `travel: ${tm} only, chunked to 625 elements per request ${q.map(r => r.dests.length)}`);
    ok(q.every(r => !('transitPreferences' in r.body) && !('departureTime' in r.body) && !('routingPreference' in r.body)), `travel: ${tm} sends no transit fields, departureTime or routingPreference`); }
  const seen = [], modes = []; const r = await G.run({ key: 'TESTKEY', pts, need: need.slice(0, 2), prof: 'B', depart: dep, sleep: async () => {},
    fetch: async (u, o) => { modes.push(JSON.parse(o.body).travelMode); return { ok: true, status: 200, text: async () => '[{"condition":"ROUTE_EXISTS","duration":"125s"},{"destinationIndex":1,"condition":"ROUTE_EXISTS","duration":"60s"}]' }; },
    onResult: (m, i, j, v) => seen.push(m + j + '=' + v) });
  ok(r.error === null && modes.join() === 'BICYCLE' && seen.join() === 'B1=3,B2=1', 'travel: run() reports under the profile, ignores w for bike/car ' + seen);
}
// 12) planner.js with a stub page and fake Routes API: cache keys, fetching only what's missing, wording (no real key or API)
{ class El { constructor(id) { this.id = id; this.dataset = {}; this.style = {}; this.innerHTML = ''; this.textContent = ''; this.value = ''; this.children = []; this.parentNode = {}; this.parentNode.classList = this.classList = { add() {}, remove() {}, toggle() {} }; }
    querySelector() { return null; } querySelectorAll() { return this.id === 'ptm' ? RAD : []; } addEventListener() {} setAttribute() {} insertAdjacentHTML() {} before() {} scrollIntoView() {} }
  const RAD = ['T', 'B', 'D'].map(v => { const e = new El('r' + v); e.value = v; return e; });
  const iso = (d, h) => d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0') + 'T' + h + ':00';
  const day = new Date(Date.now() + 864e5), today = iso(day, '00:00').slice(0, 10);
  const D = () => ({ from: today, to: today, films: { a: 'Film A', b: 'Film B' }, cinemas: [{ name: 'One', chain: 'X', lat: 53.48, lng: -2.24 }, { name: 'Two', chain: 'X', lat: 53.49, lng: -2.24 }],
    shows: [{ k: 'a', c: 0, t: iso(day, '12:00'), e: iso(day, '14:00'), d: 110, x: [] }, { k: 'b', c: 1, t: iso(day, '18:00'), e: iso(day, '20:00'), d: 110, x: [] }] });
  const L = { screenType: () => 'Standard', isScreen: () => false, stCmp: () => 0, pickAllows: () => true, esc: String, tagBadges: () => '', staleNote: () => '' };
  const LS = new Map([['fazon-cinema-gkey', 'TESTKEY'], ['fazon-cinema-planner', JSON.stringify({ picks: [{ k: 'a' }, { k: 'b' }], set: { pc: 'M1 1AE', geo: { pc: 'M1 1AE', lat: 53.47, lng: -2.24 } } })]]);
  const localStorage = { getItem: k => LS.has(k) ? LS.get(k) : null, setItem: (k, v) => void LS.set(k, String(v)), removeItem: k => void LS.delete(k) };
  const MINS = { TRANSIT: 20, WALK: 99, BICYCLE: 11, DRIVE: 7 }; let calls = [], noBike = false;
  const fetch = async (u, o) => { if (!u.startsWith('https://routes.googleapis.com/')) throw new Error('unexpected fetch ' + u); const b = JSON.parse(o.body); calls.push(b);
    const v = b.destinations.map((_, k) => noBike && b.travelMode === 'BICYCLE' ? { destinationIndex: k, condition: 'ROUTE_NOT_FOUND' } : { destinationIndex: k, condition: 'ROUTE_EXISTS', duration: MINS[b.travelMode] * 60 + 's' });
    return { ok: true, status: 200, text: async () => JSON.stringify(v) }; };
  const boot = () => { const els = new Map(), document = { getElementById: id => els.get(id) || els.set(id, new El(id)).get(id), createElement: () => new El('') };
    const window = { matchMedia: () => ({ matches: false }) }, location = { hash: '', search: '', pathname: '/' }, history = { replaceState() {} };
    document.getElementById('mode').children = [];
    new Function('document', 'localStorage', 'window', 'fetch', 'location', 'history', 'Worker', [FILES.DATES, FILES.SOLVER, FILES.ROUTES, FILES.PLANNER, 'return CINEMA_PLANNER;'].join('\n'))(document, localStorage, window, fetch, location, history, undefined)(D(), L);
    return document.getElementById; };
  let $ = boot();
  const plan = async () => { calls = []; await $('pgo').onclick(); return calls.map(b => b.travelMode); };
  const mode = v => { RAD.forEach(r => r.checked = r.value === v); $('ptm').onchange({ target: { value: v } }); };
  const box = (id, v) => $(id).onchange({ target: { checked: v } });
  const keys = () => Object.keys(JSON.parse(LS.get('fazon-cinema-gtimes') || '{}')), prefixes = () => [...new Set(keys().map(k => k.split('|')[0]))].sort().join();
  const pre = await plan(), legs = pre.filter(m => m === 'TRANSIT').length;
  ok(legs === 3 && pre.includes('WALK') && prefixes() === 'T,W', 'plan: public transport fetched under the old T| and W| keys ' + prefixes());
  ok(/Using Google transit times for every journey/.test($('pbanner').innerHTML) && /travelmode=transit/.test($('pout').innerHTML) && !/ cycle| drive/.test($('pout').innerHTML), 'plan: transit wording and links');
  $ = boot();
  ok((await plan()).length === 0 && /20 min to Two/.test($('pout').innerHTML), 'plan: a fresh page reuses cached transit times, nothing re-billed');
  box('pnotram', true);
  ok(/will be fetched/.test($('pbanner').innerHTML) && /avoiding trams where possible/.test($('pbanner').innerHTML) && /preference/.test($('ptmst').textContent), 'plan: avoid trams wording says where possible');
  let m = await plan();
  ok(m.length === legs && m.every(x => x === 'TRANSIT') && calls.every(b => b.transitPreferences.allowedTravelModes.join() === 'BUS,SUBWAY,TRAIN'), 'plan: avoid trams fetches its own transit times, walking stays cached');
  box('pnotrain', true); await plan();
  ok(calls.length === legs && calls.every(b => b.transitPreferences.allowedTravelModes.join() === 'BUS,SUBWAY'), 'plan: avoid both fetches again');
  box('pnotram', false); box('pnotrain', false);
  ok((await plan()).length === 0, 'plan: unticking goes back to the cached times with no requests');
  mode('B'); m = await plan();
  ok(m.length === legs && m.every(x => x === 'BICYCLE') && !calls.some(b => b.transitPreferences), 'plan: cycling asks for BICYCLE only, no WALK');
  ok(/11 min cycle to Two/.test($('pout').innerHTML) && /11 min cycle home/.test($('pout').innerHTML) && /travelmode=bicycling/.test($('pout').innerHTML) && !/travelmode=transit/.test($('pout').innerHTML), 'plan: cycling legs labelled, Maps links match');
  ok(/Using Google cycling times for every journey/.test($('pbanner').innerHTML), 'plan: cycling banner');
  mode('D'); m = await plan();
  ok(m.length === legs && m.every(x => x === 'DRIVE') && /7 min drive to Two/.test($('pout').innerHTML) && /travelmode=driving/.test($('pout').innerHTML) && /traffic or parking/.test($('ptmst').textContent), 'plan: driving requests, labels, links, note');
  mode('T');
  ok((await plan()).length === 0 && /20 min to Two/.test($('pout').innerHTML), 'plan: back to public transport never reuses bike or car times');
  ok(prefixes() === 'B,D,T,Ttr,Tt,W'.split(',').sort().join(), 'plan: cache holds one set per choice ' + prefixes());
  // no route by bike: the fallback is marked and named for what it is
  const c0 = JSON.parse(LS.get('fazon-cinema-gtimes')); for (const k in c0) if (k.startsWith('B|')) delete c0[k]; LS.set('fazon-cinema-gtimes', JSON.stringify(c0));
  $ = boot(); noBike = true; mode('B'); await plan(); noBike = false;
  ok(/~\d+( h)?( \d+)?( min)? \(public transport estimate\) to Two/.test($('pout').innerHTML) && !/ cycle/.test($('pout').innerHTML) && /straight-line public transport estimates/.test($('pbanner').innerHTML), 'plan: bike fallback says public transport estimate, not cycle');
  // forgetting the key: public transport estimates only, bike and car off, saved choice kept for later
  $('pkey').value = ''; $('pkey').onchange();
  ok(RAD.find(r => r.value === 'T').checked && RAD.filter(r => r.value !== 'T').every(r => r.disabled && !r.checked) && $('pnotram').disabled && $('pnotrain').disabled, 'plan: without a key bike/car/avoid are disabled');
  ok(/need a Google Maps key/.test($('ptmst').textContent) && /public transport estimates/.test($('pbanner').innerHTML) && (await plan()).length === 0, 'plan: without a key there is a note and no requests');
  ok(JSON.parse(LS.get('fazon-cinema-planner')).set.travel === 'B', 'plan: saved choice survives for when a key returns');
  $ = boot();
  ok(!$('ptav').hidden && RAD.find(r => r.value === 'T').checked && /public transport and walking/.test($('pout').innerHTML), 'plan: a fresh keyless page opens on public transport');
}
console.log(fails ? fails + ' FAILED' : 'ALL PASSED');
