// Cinema Planner unit tests (solver, screen types, Google Routes client) against the files the site serves.
// Run from anywhere:  node cinema-frontend-tests/test_solver.js  (or: deno run --allow-read cinema-frontend-tests/test_solver.js)
// (Lives outside static/ so Hugo never publishes it.)
const fs = globalThis.Deno ? null : await import('node:fs');
const read = f => { const u = new URL('../static/cinema/' + f, import.meta.url); return fs ? fs.readFileSync(u, 'utf8') : Deno.readTextFileSync(u); };
const FILES = { SOLVER: read('solver.js'), ST: read('screentypes.js'), ROUTES: read('routes.js') };
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
console.log(fails ? fails + ' FAILED' : 'ALL PASSED');
