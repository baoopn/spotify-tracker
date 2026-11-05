# Design direction: "liner notes"

Shared visual language for the public page (`web/public`) and the admin
(`web/admin`). Derived with the ui-ux-pro-max skill (Editorial Grid /
Magazine + Nature Distilled warmth, pastel spot colours) and the baoopn
brand kit. Goal: **pastel, warm and hand-made, like a record sleeve or a
risograph zine, not a generic AI/SaaS dashboard.**

## Metaphor

Paper and ink. The page is a sheet of soft pastel paper; text and rules are
a soft aubergine ink; colour comes from a few **flat pastel spot inks**, as
in risograph printing. Album art is the hero, printed
with a thin ink border. Numbers are set like liner-note credits: tabular,
monospaced, small caps labels.

## Colour: truly pastel, no gradients

**No gradients anywhere** — no linear/radial/conic gradients, no blurred
album-art washes, no gradient text, no gradient skeleton shimmer. Every
surface is a single flat colour; depth comes from rules and print offsets.

The theme is **not** the logo colours. Navy `#0b0537` / cream `#fff5eb` are
reserved for the logo, favicon and the small brand badge/chip only. The UI
palette is genuinely pastel in both modes:

| Token | Light ("pastel day") | Dark ("pastel night") | Use |
|---|---|---|---|
| `--paper` | `#fbf7f2` soft milk | `#24212e` muted plum-grey | page background |
| `--paper-2` | `#f4eefa` lilac milk | `#2d2939` | raised surface / table header |
| `--paper-3` | `#eef6f1` mint milk | `#353043` | hover / selected row |
| `--ink` | `#3b3552` soft aubergine (not navy) | `#f2ecf9` lavender white | body text, rules, focus, primary button fill |
| `--ink-soft` | `#6b6585` | `#bdb5cf` | secondary text (≥ 4.5:1 on paper) |
| `--rule` | ink @ 14% | ink @ 18% | hairlines |
| `--spot-peach` | `#ffd6c4` | `#f0b8a3` | now playing, warm highlight |
| `--spot-mint` | `#c8ecd9` | `#a8d8bf` | success, "enriched" |
| `--spot-lilac` | `#e3d7fb` | `#c6b4ef` | playlists, selection, accent |
| `--spot-butter` | `#fbedb3` | `#e8d68e` | warnings, pending |
| `--spot-sky` | `#cfe5fb` | `#a9c9ea` | info, link hover wash |
| `--spot-rose` | `#f9cfdc` | `#eaa9bd` | errors (+ icon/text, never colour alone) |

Rules:
- Spots are **fills only**; text on a spot is always the *light-mode* ink
  `#3b3552` (dark text on pastel, in both modes) — check 4.5:1.
- Dark mode is a soft, low-contrast-background night palette (plum-grey, not
  navy or black); pastels stay pastel (light, desaturated), never neon.
- Primary buttons: `--ink` fill with `--paper` text, or a spot fill with ink
  text; print-offset shadow in `--ink` (light) / a spot (dark).
- The alternate palettes (Lavender, Mint, Peach, Sky, Rose) only change which
  spot is dominant (accent, selection, now-playing) and tint `--paper-2`.
- Brand badge (`--brand`/`--brand-fg`): navy/cream, small, logo-adjacent only.

## Typography

- Display: **Outfit** (brand) — large, tight (`letter-spacing: -0.02em`),
  used sparingly: track title, page titles, big numbers.
- Body: **Inter**, 16px base, 1.5 line-height.
- Credits/labels/numbers: **JetBrains Mono**, `font-variant-numeric:
  tabular-nums`, small caps or uppercase with `letter-spacing: 0.08em` at
  11–12px for labels ("NOW PLAYING", "PLAYS", "LAST PLAYED").
- Hierarchy through size and weight, not colour.

## Shape, lines and depth

- Hairline rules (`1px solid` ink at 15–20% opacity) separate sections, like
  a printed booklet. Prefer rules and whitespace over boxed cards.
- Radius: `--baoopn-radius` 14px for large containers and art, 8px for
  controls, full pill only for tags/chips.
- **No blurred drop shadows.** Depth is a *print offset*: a solid
  `2px 2px 0 var(--ink)` (or a spot colour) offset shadow on primary buttons,
  the now-playing card and focused/hovered cards. Press state removes the
  offset (`translate(2px,2px)`).
- Subtle paper grain overlay on the page background (SVG noise, ~4% opacity,
  `pointer-events:none`, disabled under `prefers-reduced-transparency` /
  high-contrast).

## Layout

- Asymmetric editorial grid. Public page: large album art left, credits
  block right (title, artists, album · year, progress as a thin ink line
  with a dot), then "Recently played" as a **numbered tracklist** (01, 02 …
  in mono) rather than cards, and playlists as a row of square "sleeves".
- Admin: a calm left rail; pages open with a big Outfit title + one-line
  mono summary; KPI numbers are large type on paper separated by rules
  (no icon-in-a-circle tiles); tables are ruled tracklists with mono numbers,
  generous row height (≥ 44px), sticky header, no zebra cards.
- Status = small flat chip with a dot + mono label, filled with the matching
  spot ink.

## Motion

Few and purposeful: 150–220ms ease-out for hover/press, the brand
now-playing bars, a gentle progress line. No bouncy springs, no shimmer
gradients on everything (skeletons use a flat spot fill with an opacity pulse — no
shimmer gradient). Respect `prefers-reduced-motion`.

## Avoid (reads as "AI-generated")

Any gradient (incl. blurred album-art backgrounds), purple→blue gradients, glassmorphism/frosted panels, glowing borders, neon
accents, emoji icons, sparkles, every element in an identical rounded card
with a soft shadow, icon-in-colored-circle KPI tiles, "Welcome back 👋"
greetings, centered hero with gradient text, generic stock illustrations,
lorem-style marketing copy. Copy should be short, specific and personal
("What Bao is listening to", "Played 3 times this week").

## Checklist (from ui-ux-pro-max priorities)

Contrast ≥ 4.5:1 for text (spots are never text colour), visible focus
(2px ink outline + 2px offset), touch targets ≥ 44×44, SVG icons only,
`cursor-pointer` on interactive elements, hover 150–300ms, reduced motion
respected, no horizontal scroll at 320/375/768/1024/1440px, CLS-safe images
(fixed aspect ratios).
