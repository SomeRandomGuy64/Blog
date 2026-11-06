# HCI checks

These are automated checks of fazon.nexus against usability and accessibility literature. Each check names the sources it comes from; [LITERATURE.md](LITERATURE.md) has the full citations, what each one claims, and what a script can't judge.

```
deno run -A hci-tests/run.ts                  # hugo build + full sweep (~38 pages × 11 profiles)
deno run -A hci-tests/run.ts --quick          # desktop + mobile only
deno run -A hci-tests/run.ts --pages articles,about
deno run -A hci-tests/run.ts --base https://fazon.nexus   # the live site, real network timings
deno run -A hci-tests/run.ts --jobs 8         # parallel tabs (default 4)
```

The report goes to `hci-tests/out/report.md`, with the raw findings in `report.json`. The exit code is 1 when anything **fails**. Warnings are things the literature leans against but that aren't hard failures (AAA criteria, best-practice rules, judgement calls).

On the first run, astral downloads a headless Chrome for Testing to `~/.cache/astral`.

## What it does

- Builds the site with `hugo` into a temp dir and serves it on a free localhost port. The 404 handling mimics GitHub Pages.
- Every page except `/cinema/` is loaded in headless Chrome at these profiles:
  - Standard: 1366×768 in dark and light, plus a pass with reduced motion on
  - Mobile: 375×812
  - Reflow: 320×640
  - Wide: 1920×1080, 2560×1440, 2560×1080 and 3440×1440, plus a light pass at 1920
- `api.fazon.nexus` is blocked, so every page is seen logged out and nothing is written to the backend.
- Media `play()`, `AudioContext.resume` and speech are stubbed, and Chrome runs with `--mute-audio`. Kyōtsu stays silent.
- Each tab gets its own in-memory `localStorage`, so parallel passes can't change each other's theme or layout.

## Files

- `run.ts`: harness, passes, and report
- `probe.js`: the in-page measurements (target boxes, overflow, characters per line, focus state…)
- `checks.ts`: one entry per check: title, pass rule, citation keys
- `LITERATURE.md`: annotated bibliography

## Limits

These are automated checks: they catch regressions and measurable rule breaks. They don't replace a heuristic evaluation (Nielsen 1994 recommends 3–5 evaluators) or watching someone use the site. `keyboard-reachable` is a heuristic (pointer cursor without focusability). axe-core only flags what it can decide, which is roughly a third of WCAG.
