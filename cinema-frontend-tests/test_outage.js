// Cinema Planner unit tests for the outage message (static/cinema/outage.js).
// Run from anywhere:  deno run --allow-read cinema-frontend-tests/test_outage.js  (or: node …)
const fs = globalThis.Deno ? null : await import('node:fs');
const read = f => { const u = new URL('../static/cinema/' + f, import.meta.url); return fs ? fs.readFileSync(u, 'utf8') : Deno.readTextFileSync(u); };
const { CINEMA_OUTAGE: O, CINEMA_OUTAGE_TEXT: T } = new Function(read('outage.js') + '\nreturn {CINEMA_OUTAGE,CINEMA_OUTAGE_TEXT};')();
let fails = 0;
const ok = (c, m) => { if (!c) { fails++; console.log('FAIL', m); } else console.log('ok  ', m); };
const cin = (chain, stale, updated = '2026-11-17T12:00:00Z', error = null) => ({ chain, stale, updated, error });
const many = (chain, n, stale, upd) => Array.from({ length: n }, () => cin(chain, stale, upd));

const fresh = [...many('Odeon', 5, false), ...many('Vue', 4, false), ...many('Independent', 10, false)];
ok(O({ cinemas: fresh }) === null, 'all fresh: no outage');
ok(O({ cinemas: [] }) === null && O(null) === null && O({}) === null, 'empty / bad data: no outage');

const one = [...fresh]; one[0] = cin('Odeon', true);
ok(O({ cinemas: one }) === null, 'one stale Odeon of 5: no outage');

const chains = [...many('Odeon', 5, true, '2026-11-16T18:34:02Z'), ...many('Cineworld', 4, true, '2026-11-16T18:30:00Z'),
  ...many('Vue', 4, false), ...many('Independent', 30, false)];
const o = O({ cinemas: chains });
ok(o && o.chains.join() === 'Cineworld,Odeon' && !o.all, 'two whole chains stale: named, sorted');
ok(o && o.since.toISOString() === '2026-11-16T18:34:02.000Z', 'since = latest update among affected');
const t = T(o);
ok(t.startsWith('Cineworld and Odeon times haven\'t updated since '), 'text lists chains: ' + t);

const three = [...many('A', 3, true), ...many('B', 3, true), ...many('C', 3, true), ...many('Independent', 40, false)];
ok(T(O({ cinemas: three })).startsWith('A, B and C times'), 'three chains joined with commas and "and"');

ok(O({ cinemas: [...many('Savoy', 2, true), ...many('Independent', 20, false)] }) === null, 'chain with <3 cinemas ignored');

const indie = [...many('Independent', 6, true), ...many('Independent', 14, false)];
const oi = O({ cinemas: indie });
ok(oi && oi.all && T(oi).startsWith('Many cinemas haven\'t updated'), '30% of all cinemas stale: general outage');

const unf = [...Array.from({ length: 20 }, () => cin('Odeon', true, '', 'not fetched yet')), ...many('Vue', 4, false)];
ok(O({ cinemas: unf }) === null, '"not fetched yet" regions are not an outage');

ok(T(O({ cinemas: many('Vue', 4, true, '') })).includes(' recently'), 'no update time: says "recently"');

const big = [...many('Odeon', 30, true, '2026-11-16T18:34:02Z'), ...many('Vue', 20, true), ...many('Independent', 30, false)];
const ob = O({ cinemas: big });
ok(ob && !ob.all && T(ob).startsWith('Odeon and Vue times'), 'chains that are most cinemas: still named, not "many"');
const both = [...many('Odeon', 4, true), ...many('Independent', 6, true), ...many('Independent', 10, false)];
ok(T(O({ cinemas: both })).startsWith('Times from Odeon and many other cinemas'), 'chain + many others');

if (fails) { console.log(fails + ' FAILED'); (globalThis.Deno ? Deno.exit(1) : process.exit(1)); } else console.log('all passed');
