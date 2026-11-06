# HCI literature behind the checks

Each entry says what the source claims, how `run.ts` turns it into a check, and where a machine can't judge it. Keys in **[brackets]** are the ones `checks.ts` and the report cite.

## Heuristics and principles

- **[Nielsen1994]** <a id="nielsen1994"></a> Nielsen, J. (1994). Enhancing the explanatory power of usability heuristics. *Proc. CHI '94*, 152–158. ACM. Also Nielsen, J. (1994). Heuristic evaluation. In J. Nielsen & R. L. Mack (Eds.), *Usability Inspection Methods*, 25–62. Wiley.
  The ten heuristics, derived by factor-analysing 249 usability problems. Their ancestor is Molich & Nielsen (1990), *Improving a human-computer dialogue*, CACM 33(3), and Nielsen & Molich (1990), *Heuristic evaluation of user interfaces*, Proc. CHI '90. The suite maps them like this:
  - #1 visibility of system status: `where-am-i`, `layout-status`
  - #3 user control and freedom: `colour-scheme`
  - #4 consistency and standards: `consistent-nav`, `colour-scheme`
  - #7 flexibility and efficiency, i.e. accelerators: `char-key-shortcuts`
  - #9 help users recover from errors: `error-recovery`
  - #2 match with the real world, #5 error prevention, #6 recognition over recall, #8 minimalist design, #10 help: **not automatable**. A heuristic evaluation needs 3–5 evaluators, since one finds about 35% of problems (Nielsen & Landauer 1993, *A mathematical model of the finding of usability problems*, Proc. INTERCHI '93).

- **[Shneiderman2016]** <a id="shneiderman2016"></a> Shneiderman, B., Plaisant, C., Cohen, M., Jacobs, S., Elmqvist, N., & Diakopoulos, N. (2016). *Designing the User Interface: Strategies for Effective Human-Computer Interaction* (6th ed.). Pearson. The Eight Golden Rules date from the 1st ed., 1987.
  - Rule 1, strive for consistency: `consistent-nav`
  - Rule 2, enable shortcuts for frequent users: `char-key-shortcuts`, which supports Wide's `FazonWide` keys
  - Rule 5, prevent errors / simple error handling: `error-recovery`
  - Rule 7, keep users in control: `keyboard-reachable`

- **[Norman2013]** <a id="norman2013"></a> Norman, D. A. (2013). *The Design of Everyday Things* (revised ed.). Basic Books. 1st ed. 1988 as *The Psychology of Everyday Things*.
  The book covers signifiers, feedback and mapping, and the gulfs of execution and evaluation. `layout-status` checks feedback: does the pressed switch match the state? `where-am-i` checks the signifier of current location.

## Motor control: target size

- **[Fitts1954]** <a id="fitts1954"></a> Fitts, P. M. (1954). The information capacity of the human motor system in controlling the amplitude of movement. *Journal of Experimental Psychology*, 47(6), 381–391.
- **[MacKenzie1992]** <a id="mackenzie1992"></a> MacKenzie, I. S. (1992). Fitts' law as a research and design tool in human-computer interaction. *Human-Computer Interaction*, 7(1), 91–139.
  Movement time is MT = a + b·log₂(D/W + 1), the Shannon formulation from MacKenzie. Smaller targets cost time and misses. `target-size` uses the WCAG 2.5.8 floor (24 px) as the pass line. Wide's 28 px controls sit above it. Fitts also argues for screen-edge targets (effectively infinite W), which favours Wide's sidebar and sticky bar. The suite doesn't score that.

## Reading on screen

- **[DysonHaselgrove2001]** <a id="dysonhaselgrove2001"></a> Dyson, M. C., & Haselgrove, M. (2001). The influence of reading speed and line length on the effectiveness of reading from screen. *International Journal of Human-Computer Studies*, 54(4), 585–612.
  Lines of about 55 cpl gave the best comprehension and about 100 cpl the fastest reading. Lines past 100 weren't studied, so `line-length` fails above 100 and warns above 80.
- **[Dyson2004]** <a id="dyson2004"></a> Dyson, M. C. (2004). How physical text layout affects reading from screen. *Behaviour & Information Technology*, 23(6), 377–393. A review: line length, columns, window size and scrolling versus paging.
- **[Bringhurst1992]** <a id="bringhurst1992"></a> Bringhurst, R. (1992). *The Elements of Typographic Style*. Hartley & Marks. Says 45–75 characters, with 66 ideal, for a single column. This is the source of the < 35 cpl warning on desktop.
- **[Tinker1963]** <a id="tinker1963"></a> Tinker, M. A. (1963). *Legibility of Print*. Iowa State University Press. The classic studies of how size, line length and leading interact. It's the basis for `line-height`.
- **[LeggeBigelow2011]** <a id="leggebigelow2011"></a> Legge, G. E., & Bigelow, C. A. (2011). Does print size matter for reading? A review of findings from vision science and typography. *Journal of Vision*, 11(5):8, 1–22.
  Reading speed holds steady above a "critical print size" and collapses below it. The common 16 px body text sits comfortably above it at desktop viewing distances. That's the basis for `body-text-size`.
- **Mansfield, Legge & Bane (1996).** Psychophysics of reading XV: Font effects in normal and low vision. *Investigative Ophthalmology & Visual Science*, 37(8), 1492–1501. Fixed-pitch Courier read as fast as proportional Times at ordinary sizes, and better near the acuity limit. So JetBrains Mono for prose isn't a usability problem in itself. A monospace font does take more width per character, which is why line length is measured in characters rather than pixels. *(Context only; no check.)*
- **[Piepenbrock2013]** <a id="piepenbrock2013"></a> Piepenbrock, C., Mayr, S., Mund, I., & Buchner, A. (2013). Positive display polarity is advantageous for both younger and older adults. *Ergonomics*, 56(7), 1116–1124. Also Buchner, A., & Baumgartner, N. (2007). Text–background polarity affects performance irrespective of ambient illumination and colour contrast. *Ergonomics*, 50(7), 1036–1063.
  Dark text on light reads better in proofreading-style tasks. That's no reason to drop dark mode. It is a reason the first visit should follow the OS preference rather than always open dark: `colour-scheme`.

## Navigation and wayfinding

- **[PirolliCard1999]** <a id="pirollicard1999"></a> Pirolli, P., & Card, S. (1999). Information foraging. *Psychological Review*, 106(4), 643–675.
  People follow "information scent", the cues in link text and labels. Vague link text ("read more") and identical titles give no scent: `link-purpose`, `page-titles`.

## Response time

- **[Miller1968]** <a id="miller1968"></a> Miller, R. B. (1968). Response time in man-computer conversational transactions. *Proc. AFIPS Fall Joint Computer Conference*, 33, 267–277.
- **[CardMoranNewell1983]** <a id="cardmorannewell1983"></a> Card, S. K., Moran, T. P., & Newell, A. (1983). *The Psychology of Human-Computer Interaction*. Lawrence Erlbaum. Model Human Processor timings.
- **[Nielsen1993]** <a id="nielsen1993"></a> Nielsen, J. (1993). *Usability Engineering*, ch. 5. Academic Press. Gives the 0.1 s / 1 s / 10 s limits: feels instant, keeps the flow of thought, keeps attention.
- **[WebVitals]** <a id="webvitals"></a> Google (2020–). Web Vitals: LCP ≤ 2.5 s, CLS ≤ 0.1, INP ≤ 200 ms. Industry thresholds, not peer-reviewed. They are the practical, measurable stand-ins for the limits above: `layout-stability`. Run with `--base https://fazon.nexus` for real network timings.

## Accessibility standard

- **[WCAG22]** <a id="wcag22"></a> W3C (2023). *Web Content Accessibility Guidelines (WCAG) 2.2*. W3C Recommendation, 5 October 2023. https://www.w3.org/TR/WCAG22/. Automated through axe-core (Deque) for everything axe can decide. The success criteria the suite checks directly:
  - **[WCAG22-1.3.1]** <a id="wcag22-1.3.1"></a> Info and Relationships (A), and **[WCAG22-2.4.6]** <a id="wcag22-2.4.6"></a> Headings and Labels (AA): `headings`
  - **[WCAG22-1.4.8]** <a id="wcag22-1.4.8"></a> Visual Presentation (AAA): ≤ 80 characters, line spacing ≥ 1.5. Used as a *warning* line only.
  - **[WCAG22-1.4.10]** <a id="wcag22-1.4.10"></a> Reflow (AA): 320 CSS px with no 2-D scrolling: `reflow-320`. This is the only thing the standards demand of "mobile". Nothing in the literature requires mobile-*first* authoring. A desktop-first Wide alongside a Standard that reflows is fine.
  - **[WCAG22-1.4.12]** <a id="wcag22-1.4.12"></a> Text Spacing (AA): `text-spacing`
  - **[WCAG22-2.1.1]** <a id="wcag22-2.1.1"></a> Keyboard (A): `keyboard-reachable`
  - **[WCAG22-2.1.4]** <a id="wcag22-2.1.4"></a> Character Key Shortcuts (A): `char-key-shortcuts`
  - **[WCAG22-2.3.3]** <a id="wcag22-2.3.3"></a> Animation from Interactions (AAA), honouring `prefers-reduced-motion`: `reduced-motion`
  - **[WCAG22-2.4.2]** <a id="wcag22-2.4.2"></a> Page Titled (A): `page-titles`
  - **[WCAG22-2.4.4]** <a id="wcag22-2.4.4"></a> Link Purpose (A): `link-purpose`
  - **[WCAG22-2.4.7]** <a id="wcag22-2.4.7"></a> Focus Visible (AA): `focus-visible`
  - **[WCAG22-2.4.11]** <a id="wcag22-2.4.11"></a> Focus Not Obscured (Minimum) (AA, new in 2.2): `focus-not-obscured`. This one matters for Wide's sticky page bar.
  - **[WCAG22-2.5.8]** <a id="wcag22-2.5.8"></a> Target Size (Minimum) (AA, new in 2.2): `target-size`
  - **[WCAG22-3.2.3]** <a id="wcag22-3.2.3"></a> Consistent Navigation (AA): `consistent-nav`

## Relevant, but not checkable by a script

These need a person, either you running a heuristic walkthrough or a few Dojo friends doing think-aloud sessions.

- **Hick, W. E. (1952).** On the rate of gain of information. *QJEP*, 4(1), 11–26. **Hyman, R. (1953).** *JEP*, 45(3), 188–196. Choice time grows with log₂(n + 1). This bears on how many items the sidebar and Track toolbars show. Note that it applies to unfamiliar, unordered choices; experts scanning a known layout are faster.
- **Cowan, N. (2001).** The magical number 4 in short-term memory. *Behavioral and Brain Sciences*, 24(1), 87–114. It updates **Miller, G. A. (1956)**, *Psychological Review*, 63(2), 81–97. Miller's 7±2 is often misapplied to menu lengths; it's about recall, so recognition over recall (Nielsen #6) is the real lesson.
- **Lane, D. M., Napier, H. A., Peres, S. C., & Sándor, A. (2005).** Hidden costs of graphical user interfaces: Failure to make the transition from menus and icon toolbars to keyboard shortcuts. *IJHCI*, 18(2), 133–144. **[Grossman2007]** <a id="grossman2007"></a> Grossman, T., Dragicevic, P., & Balakrishnan, R. (2007). Strategies for accelerating on-line learning of hotkeys. *Proc. CHI '07*, 1591–1600. Users rarely adopt shortcuts unless the UI teaches them. Wide's `data-tip` tooltips and `?` sheet are the right move; showing the key inside the tooltip would go further.
- **Kurosu, M., & Kashimura, K. (1995).** Apparent usability vs. inherent usability. *CHI '95 Conference Companion*, 292–293. **Tractinsky, N., Katz, A. S., & Ikar, D. (2000).** What is beautiful is usable. *Interacting with Computers*, 13(2), 127–145. **Lindgaard, G., Fernandes, G., Dudek, C., & Brown, J. (2006).** Attention web designers: You have 50 milliseconds to make a good first impression! *BIT*, 25(2), 115–126. Aesthetics colours perceived usability. This is a reason to measure with tasks, not just ask whether people like the site.
- **Wertheimer (1923)** and **Palmer & Rock (1994)**, Rethinking perceptual organization: The role of uniform connectedness, *Psychonomic Bulletin & Review*, 1(1), 29–55. Gestalt grouping explains why Wide's hairlines-not-cards approach works: proximity and alignment do the grouping. Judge it by eye.
- **Nielsen, J. (2006).** F-shaped pattern for reading web content. Nielsen Norman Group (grey literature). Readers scan more than they read. Front-load headings and first sentences.
