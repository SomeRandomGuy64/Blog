// Cinema Planner unit tests for same-film corrections (static/cinema/same.js): the visitor's corrections over the server's
// matches, and saved picks following their film.  Run from anywhere:  deno run --allow-read cinema-frontend-tests/test_same.js  (or: node …)
const fs = globalThis.Deno ? null : await import('node:fs');
const read = f => { const u = new URL('../static/cinema/' + f, import.meta.url); return fs ? fs.readFileSync(u, 'utf8') : Deno.readTextFileSync(u); };
const S = new Function(read('same.js') + '\nreturn CINEMA_SAME;')()();
let fails = 0;
const ok = (c, m) => { if (!c) { fails++; console.log('FAIL', m); } else console.log('ok  ', m); };
const eq = (a, b) => JSON.stringify(a) === JSON.stringify(b);
const data = () => ({
  films: { rocky: 'Rocky', kidsrocky: 'Kids Club: Rocky', dune: 'Dune' },
  aliases: { throwbackrocky: 'rocky' },
  notes: { rocky: { n: { rocky: 'Rocky', throwbackrocky: 'Throwback: Rocky' }, s: ['kidsrocky'], u: true }, kidsrocky: { s: ['rocky'], u: true } },
  shows: [{ k: 'rocky', c: 0 }, { k: 'rocky', r: 'throwbackrocky', c: 1 }, { k: 'kidsrocky', c: 2 }, { k: 'dune', c: 3 }],
});
const L = o => S.clean(o);

ok(eq(S.clean(null), { m: {}, no: [], ok: [] }), 'clean: nothing saved');
ok(eq(S.clean({ m: { a: 1, b: 'c' }, no: 'x', ok: ['d', 2] }), { m: { b: 'c' }, no: [], ok: ['d'] }), 'clean: junk dropped');

// no corrections: unchanged
let D = S.apply(data(), L(null));
ok(eq(D.shows.map(s => s.k), ['rocky', 'rocky', 'kidsrocky', 'dune']) && D.notes.rocky.u, 'no corrections: untouched');

// merge one film into another
D = S.apply(data(), L({ m: { kidsrocky: 'rocky' } }));
ok(D.shows[2].k === 'rocky' && D.shows[2].r === 'kidsrocky', 'merge: showings move');
ok(!D.films.kidsrocky && D.films.rocky === 'Rocky', 'merge: the empty film goes');
ok(S.keyOf(D, 'kidsrocky') === 'rocky' && D.notes.rocky.n.kidsrocky === 'Kids Club: Rocky' && !D.notes.rocky.u, 'merge: alias, name, no badge');
ok(eq(D.notes.rocky.s, []), 'merge: no longer suggested');

// split a server merge
D = S.apply(data(), L({ m: { throwbackrocky: 'throwbackrocky' } }));
ok(D.shows[1].k === 'throwbackrocky' && D.films.throwbackrocky === 'Throwback: Rocky' && D.shows[1].r === undefined, 'split: own film again');
ok(D.shows[0].k === 'rocky' && !D.notes.rocky.n && !D.aliases.throwbackrocky, 'split: the rest stays, alias gone');

ok(S.keyOf(D, 'rocky') === 'rocky', 'split: a pick stays on the main film');

// the visitor beats the server, and the server's listing can be moved on its own
D = S.apply(data(), L({ m: { throwbackrocky: 'dune' } }));
ok(D.shows[1].k === 'dune' && D.shows[0].k === 'rocky', 'merge a single listing elsewhere');

// bad corrections are harmless
D = S.apply(data(), L({ m: { rocky: 'gone' } }));
ok(D.shows[0].k === 'rocky', 'target missing from today: nothing moves');
D = S.apply(data(), L({ m: { rocky: 'dune', dune: 'rocky' } }));
ok(D.shows[0].k === 'rocky' && D.shows[3].k === 'dune', 'a loop moves nothing');
ok(S.keyOf({ aliases: { a: 'b', b: 'a' } }, 'a').length === 1, 'keyOf: a loop ends');

// dismissals
D = S.apply(data(), L({ no: ['kidsrocky|rocky'] }));
ok(eq(D.notes.kidsrocky.s, []) && !D.notes.kidsrocky.u, 'different: suggestion and badge go');
ok(D.notes.rocky.u, 'different: an unconfirmed merge keeps its badge');
D = S.apply(data(), L({ ok: ['rocky'] }));
ok(!D.notes.rocky.u, 'looks right: badge goes');

// saved picks
const pick = (k, o) => Object.assign({ k, t: k, cin: [], st: [], req: [], exc: [], dd: null }, o);
let P = S.remap([pick('dune'), pick('throwbackrocky')], data());
ok(eq(P.map(p => p.k), ['dune', 'rocky']), 'remap: old key follows');
P = S.remap([pick('rocky', { cin: ['A'], req: ['3D'], dd: ['2026-11-01', '2026-11-02'] }), pick('dune'), pick('throwbackrocky', { cin: ['B'], req: ['3D', 'IMAX'], dd: ['2026-11-02'] })], data());
ok(eq(P.map(p => p.k), ['rocky', 'dune']), 'remap: two become one, in the higher place');
ok(eq(P[0].cin, ['A', 'B']) && eq(P[0].req, ['3D']) && eq(P[0].dd, ['2026-11-02']), 'remap: allows what either allowed');
P = S.remap([pick('rocky', { cin: ['A'] }), pick('throwbackrocky')], data());
ok(eq(P[0].cin, []) && P[0].dd === null, 'remap: "any" wins');
ok(eq(S.remap([pick('nowhere')], data()).map(p => p.k), ['nowhere']), 'remap: unknown keys are kept');

console.log(fails ? `${fails} FAILED` : 'all passed');
if (fails) { globalThis.Deno ? Deno.exit(1) : process.exit(1); }
