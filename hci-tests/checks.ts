// What each check measures and where its rule comes from. Keys in `sources` are the
// citation keys in LITERATURE.md; run.ts prints them next to every result.
export type Check = { title: string; rule: string; sources: string[] };

export const CHECKS: Record<string, Check> = {
  "axe-wcag": {
    title: "WCAG 2.2 A/AA automated audit (axe-core), dark and light themes",
    rule: "No axe violations tagged wcag2a/wcag2aa/wcag21*/wcag22aa: contrast ≥ 4.5:1 for text, names on controls, labels, lang, alt text…",
    sources: ["WCAG22", "Nielsen1994"],
  },
  "axe-best-practice": {
    title: "axe-core best-practice rules",
    rule: "Landmarks, one main, heading order, page-has-heading-one. Not WCAG failures; reported as warnings.",
    sources: ["WCAG22", "Nielsen1994"],
  },
  "reflow-320": {
    title: "Reflow at 320 CSS px (400% zoom of a 1280px window)",
    rule: "Standard layout at 320×640 never scrolls the page sideways; wide tables or charts may scroll inside their own box.",
    sources: ["WCAG22-1.4.10"],
  },
  "no-sideways-scroll": {
    title: "No sideways page scroll at any tested size",
    rule: "documentElement.scrollWidth ≤ clientWidth at every profile, wide included.",
    sources: ["WCAG22-1.4.10", "Nielsen1994"],
  },
  "target-size": {
    title: "Pointer targets ≥ 24×24 CSS px (or spaced apart)",
    rule: "WCAG 2.5.8 minimum incl. its spacing and inline-link exceptions. Fitts's law: time grows with log2(D/W + 1), so small targets cost time and errors.",
    sources: ["WCAG22-2.5.8", "Fitts1954", "MacKenzie1992"],
  },
  "body-text-size": {
    title: "Body text size",
    rule: "Paragraph text ≥ 16px (warn below 16, fail below 14). Reading speed drops sharply under the critical print size.",
    sources: ["LeggeBigelow2011"],
  },
  "line-height": {
    title: "Line spacing",
    rule: "Paragraph line-height ≥ 1.4 (WCAG 1.4.8 AAA asks 1.5).",
    sources: ["WCAG22-1.4.8", "Tinker1963"],
  },
  "line-length": {
    title: "Line length (characters per line)",
    rule: "35–80 cpl passes; 80–100 warns (WCAG 1.4.8 AAA caps at 80); over 100 fails (beyond the range studied); under 35 on desktop warns.",
    sources: ["DysonHaselgrove2001", "Dyson2004", "Bringhurst1992", "WCAG22-1.4.8"],
  },
  "text-spacing": {
    title: "Text-spacing override doesn't clip text",
    rule: "With line-height 1.5, letter-spacing 0.12em, word-spacing 0.16em, paragraph spacing 2em, no text gets newly cut off.",
    sources: ["WCAG22-1.4.12"],
  },
  "focus-visible": {
    title: "Keyboard focus is visible",
    rule: "Every element reached by Tab changes visibly (outline, shadow, colour…) while focused.",
    sources: ["WCAG22-2.4.7", "Nielsen1994"],
  },
  "focus-not-obscured": {
    title: "Focused element not hidden behind sticky/fixed bars",
    rule: "When focused, an element is on screen and not fully covered by a fixed or sticky box (the wide page bar, the header).",
    sources: ["WCAG22-2.4.11"],
  },
  "keyboard-reachable": {
    title: "Everything clickable is keyboard-reachable",
    rule: "Heuristic: an element with cursor:pointer that isn't a link, button, form control or tabindex is mouse-only.",
    sources: ["WCAG22-2.1.1", "Shneiderman2016"],
  },
  "char-key-shortcuts": {
    title: "Single-key shortcuts can be turned off or remapped",
    rule: "WCAG 2.1.4 (A): global one-character shortcuts need an off switch, a remap, or to work only when a control has focus. Accelerators are good (Nielsen #7, Shneiderman #2) and a ? sheet aids learning them.",
    sources: ["WCAG22-2.1.4", "Nielsen1994", "Shneiderman2016", "Grossman2007"],
  },
  "consistent-nav": {
    title: "Navigation is the same on every page",
    rule: "The site nav lists the same links in the same order everywhere (per layout).",
    sources: ["WCAG22-3.2.3", "Nielsen1994", "Shneiderman2016"],
  },
  "where-am-i": {
    title: "Current location shown in navigation",
    rule: "On any page under a nav section, that nav link carries aria-current=page (and so its visible style).",
    sources: ["Nielsen1994", "Norman2013"],
  },
  "page-titles": {
    title: "Distinct, descriptive page titles",
    rule: "Every page has a <title> that is unique across the site and says more than the site name.",
    sources: ["WCAG22-2.4.2", "PirolliCard1999"],
  },
  "link-purpose": {
    title: "Link text carries scent",
    rule: "No 'click here' / 'read more' links; the same link text shouldn't lead to different places on one page (warn).",
    sources: ["WCAG22-2.4.4", "PirolliCard1999"],
  },
  "headings": {
    title: "Heading structure",
    rule: "One visible h1 per page; no skipped heading levels.",
    sources: ["WCAG22-1.3.1", "WCAG22-2.4.6"],
  },
  "reduced-motion": {
    title: "Honours prefers-reduced-motion",
    rule: "With reduce emulated: no running infinite CSS animations and no continuous requestAnimationFrame loop after load.",
    sources: ["WCAG22-2.3.3"],
  },
  "colour-scheme": {
    title: "First visit follows the OS light/dark preference",
    rule: "With no saved theme, prefers-color-scheme: light gives the light theme (and dark gives dark). Platform consistency; light polarity reads faster for many readers.",
    sources: ["Nielsen1994", "Piepenbrock2013"],
  },
  "layout-status": {
    title: "Layout switch tells the truth",
    rule: "The Standard | Wide control's aria-pressed matches the layout actually showing, wherever the control is visible.",
    sources: ["Nielsen1994", "Norman2013"],
  },
  "layout-stability": {
    title: "Layout stability and load time",
    rule: "Cumulative Layout Shift ≤ 0.1 and Largest Contentful Paint ≤ 2.5s (≈ the 1s 'flow' / 10s 'attention' limits). Local runs show CLS truthfully but flatter LCP.",
    sources: ["Miller1968", "Nielsen1993", "CardMoranNewell1983", "WebVitals"],
  },
  "error-recovery": {
    title: "A helpful 404 page",
    rule: "An unknown URL returns 404 with the site's own page and a way back (home link / nav).",
    sources: ["Nielsen1994", "Shneiderman2016"],
  },
  "wide-reading-column": {
    title: "Reading pages keep a single reading column in wide",
    rule: "On a reading page in wide mode, paragraphs stay ≤ 80 cpl however wide the window.",
    sources: ["DysonHaselgrove2001", "Bringhurst1992"],
  },
};
