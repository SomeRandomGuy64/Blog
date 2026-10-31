// Cinema Planner unit tests for the date helpers (static/cinema/dates.js): start-date choices, month groups and the far-off note.
// Run from anywhere:  deno run --allow-read cinema-frontend-tests/test_dates.js  (or: node …)
const fs = globalThis.Deno ? null : await import('node:fs');
const read = f => { const u = new URL('../static/cinema/' + f, import.meta.url); return fs ? fs.readFileSync(u, 'utf8') : Deno.readTextFileSync(u); };
const T = new Function(read('dates.js') + '\nreturn CINEMA_DATES;')()();
let fails = 0;
const ok = (c, m) => { if (!c) { fails++; console.log('FAIL', m); } else console.log('ok  ', m); };
const eq = (a, b) => JSON.stringify(a) === JSON.stringify(b);
const sh = (c, d) => ({ c, t: d + 'T19:00' });

ok(T.add('2026-10-30', 3) === '2026-11-02', 'add: crosses a month');
ok(T.add('2026-10-20', 7) === '2026-10-27', 'add: clocks going back changes nothing');

const shows = [sh(0, '2026-12-20'), sh(0, '2026-10-30'), sh(1, '2026-10-30'), sh(1, '2026-11-20'), sh(0, '2026-10-29'), sh(0, '2027-01-01')];
ok(eq(T.startDays(shows, '2026-10-30', '2026-12-24'), ['2026-10-30', '2026-11-20', '2026-12-20']), 'startDays: only dates with listings, in the window, sorted, unique');
ok(eq(T.startDays([], '2026-10-30', '2026-11-14'), ['2026-10-30']), 'startDays: no listings still offers the first day');
ok(eq(T.startDays([], '2026-11-15', '2026-11-14'), []), 'startDays: closed window offers nothing');

const opts = ['2026-10-30', '2026-11-20', '2026-12-20'];
ok(T.nearest(opts, '2026-11-20') === '2026-11-20', 'nearest: a valid saved start stays');
ok(T.nearest(opts, '2026-11-01') === '2026-11-20', 'nearest: a date with no listings moves to the next one');
ok(T.nearest(opts, '2027-01-01') === '2026-10-30' && T.nearest(opts, '') === '2026-10-30', 'nearest: past the end or unset -> first');
ok(T.nearest([], '2026-11-01') === undefined, 'nearest: no choices');

const m = T.months(opts);
ok(eq(m.map(x => x[0]), ['October 2026', 'November 2026', 'December 2026']) && eq(m[1][1], ['2026-11-20']), 'months: grouped in date order');

const D = { cinemas: [{ chain: 'Odeon' }, { chain: 'Everyman' }, { chain: 'Empty' }], shows: [sh(0, '2026-11-14'), sh(1, '2026-12-20')] };
ok(eq(T.stopsBefore(D, '2026-11-14'), ['Empty']), 'stopsBefore: a chain listing up to that day still counts');
ok(eq(T.stopsBefore(D, '2026-11-20'), ['Odeon', 'Empty']), 'stopsBefore: names chains whose listings end earlier');
ok(T.farNote(D, '2026-11-05', '2026-10-30') === '', 'farNote: nothing for a plan in the next week');
ok(/Odeon, Empty/.test(T.farNote(D, '2026-11-20', '2026-10-30')) && !/four weeks/.test(T.farNote(D, '2026-11-20', '2026-10-30')), 'farNote: names the chains that stop early');
ok(/four weeks/.test(T.farNote(D, '2026-12-20', '2026-10-30')), 'farNote: warns past four weeks');
ok(/fewer listings/.test(T.farNote({ cinemas: [{ chain: 'A' }], shows: [sh(0, '2026-11-20')] }, '2026-11-10', '2026-10-30')), 'farNote: generic note when every chain reaches the date');

ok(/for Odeon, Empty,/.test(T.farNote(D, '2026-12-23', '2026-10-30')) && !/Everyman/.test(T.farNote(D, '2026-12-23', '2026-10-30')), 'farNote: a plan past every listing still only names the chains that stop early');

if (fails) { console.log(fails + ' FAILED'); (globalThis.Deno ? Deno.exit : process.exit)(1); }
console.log('all passed');
