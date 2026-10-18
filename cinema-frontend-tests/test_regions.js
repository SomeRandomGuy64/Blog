// Cinema Planner unit tests for the region helpers (static/cinema/regions.js): saved-choice cleaning, the listings URL,
// summaries and totals. Run from anywhere:  deno run --allow-read cinema-frontend-tests/test_regions.js  (or: node …)
const fs = globalThis.Deno ? null : await import('node:fs');
const read = f => { const u = new URL('../static/cinema/' + f, import.meta.url); return fs ? fs.readFileSync(u, 'utf8') : Deno.readTextFileSync(u); };
const R = new Function(read('regions.js') + '\nreturn CINEMA_REGIONS;')()();
let fails = 0;
const ok = (c, m) => { if (!c) { fails++; console.log('FAIL', m); } else console.log('ok  ', m); };
const eq = (a, b) => JSON.stringify(a) === JSON.stringify(b);

// clean(): strings that look like keys, de-duplicated, sorted
ok(eq(R.clean(['yorkshire', 'north-west', 'yorkshire']), ['north-west', 'yorkshire']), 'clean: dedup + sort');
ok(eq(R.clean(['North West', 7, null, '', 'x'.repeat(41), 'wales', '<b>']), ['wales']), 'clean: drops non-keys');
ok(eq(R.clean('north-west'), []) && eq(R.clean(null), []), 'clean: non-array -> []');

// url(): ?regions=a,b (sorted, readable commas); none -> no param; other params kept
const API = 'https://cinema-api.fazon.nexus/v1/listings.json';
ok(R.url(API, ['yorkshire', 'north-west']) === API + '?regions=north-west,yorkshire', 'url: sorted keys joined by , ' + R.url(API, ['yorkshire', 'north-west']));
ok(R.url(API, []) === API, 'url: no keys -> unchanged');
ok(R.url('http://127.0.0.1:8800/v1/listings.json?x=1&regions=wales', ['london']) === 'http://127.0.0.1:8800/v1/listings.json?x=1&regions=london', 'url: replaces regions, keeps other params');
ok(R.url('http://127.0.0.1:8800/v1/listings.json?regions=wales', []) === 'http://127.0.0.1:8800/v1/listings.json', 'url: no keys drops a stale regions param');
ok(new URL(R.url(API, ['east', 'london'])).searchParams.get('regions') === 'east,london', 'url: parses back to the same list');

// list() / covered(): older API without regions -> null / derived from cinemas
const regions = [{ key: 'scotland', name: 'Scotland', cinemas: 40 }, { key: 'north-west', name: 'North West', cinemas: 123 },
  { key: 'yorkshire', name: 'Yorkshire and the Humber', cinemas: 90 }, { key: 'wales', name: 'Wales', cinemas: 0 }];
ok(R.list({ cinemas: [] }) === null && R.list({ regions: [] }) === null, 'list: older API -> null');
ok(eq(R.list({ regions: [{ key: 'a', name: 'A', cinemas: '5' }, { name: 'no key' }, null, { key: 'b' }] }), [{ key: 'a', name: 'A', cinemas: 5 }, { key: 'b', name: 'b', cinemas: 0 }]), 'list: normalises entries');
ok(eq(R.covered({ region: ['yorkshire', 'north-west'], cinemas: [] }), ['north-west', 'yorkshire']), 'covered: D.region');
ok(eq(R.covered({ cinemas: [{ region: 'wales' }, { region: 'scotland' }, { region: 'wales' }, {}] }), ['scotland', 'wales']), 'covered: from cinemas when D.region is missing');

// summary() (display order) / total()
ok(R.summary(['north-west'], regions) === 'North West', 'summary: one');
ok(R.summary(['yorkshire', 'north-west'], regions) === 'North West + Yorkshire and the Humber', 'summary: two, display order');
ok(R.summary(['yorkshire', 'north-west', 'scotland'], regions) === 'Scotland + 2 more', 'summary: three -> + 2 more');
ok(R.summary(regions.map(r => r.key), regions) === 'All of the UK', 'summary: all');
ok(R.summary([], regions) === 'No region' && R.summary(['gone'], regions) === '1 region', 'summary: empty / unknown');
ok(R.total(['north-west', 'yorkshire', 'gone'], regions) === 213 && R.total([], regions) === 0, 'total: sums known regions');
ok(R.WARN === 200 && R.KEY === 'fazon-cinema-regions', 'constants');

console.log(fails ? fails + ' FAILED' : 'ALL PASSED');
if (fails) globalThis.Deno ? Deno.exit(1) : process.exit(1);
