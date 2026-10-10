# Token law

`tokens.css` declares the vocabulary the rest of the interface is written in.
The stylesheet itself carries no commentary, so the constraints behind its
values live here. Each entry states what holds now; where a gate enforces it,
the gate is named.

## Registered properties

- `--rail-w` / `--side-w` are registered as `<length>` so collapsing a column
  can tween. An unregistered custom property only jumps, and
  `grid-template-columns` cannot interpolate a track containing `minmax()`.
  `--rail-open` / `--side-open` are the expanded widths, written by the drag
  onto `.app`'s inline style.
- `--win-ar` is registered so a bad write falls back to 16:10 instead of making
  `aspect-ratio` invalid, which collapses every box using it to zero height.
- `interpolate-size: allow-keywords` lets `height: auto` take part in a
  transition; engines without it jump, which is the prior behaviour.

## Theme packs

- A pack's values are the declarations of one generated rule,
  `:root:root[data-pack]`, in a `<style id="pack-theme">` appended to `<head>`
  when a pack is first applied and left where it is.
- Nothing is written to the root's inline style: a stylesheet of equal
  specificity added later wins without `!important`, and one already present
  loses the tie.
- The selector ties the contrast tiers (`:root[data-theme][data-contrast]`) and
  beats them by order. With no pack there is no element: the tiers alone carry
  the inks, and the reader's contrast is theirs.
- A pack's own inks take the reader's contrast as a relative-colour lightness
  offset inside that rule; the built-in inks need none.
- `--ok --warn --err --net --deleg --add --del --focus` are not in the pack
  vocabulary and are never written.
- Decoration that is not a status has its own tokens, which a pack may set
  (see Decorative roles). A pack that names `net` or `deleg` has them dropped
  with a warning by the kernel; the frontend has no entry that could write them.

## Decorative roles

`net` and `deleg` say what the agent is doing: running, connected, delegated
out. A hyperlink, the product accent and a hover halo are not that, so they read
their own tokens. Each default is the status colour it replaces, in both schemes
and at every contrast step, so nothing paints differently until a pack sets one.

| Pack key | Variable | Default | Job |
| --- | --- | --- | --- |
| `link` | `--link` | `--net` | Hyperlinks and URLs: `.md a`, `.fbk a`, `.lk`, `.hit-row .u` |
| `brand` | `--brand` | `--net` | The product's own tint: wallet and toolbar icons, info notes, feedback status chips, skill chips, the selected option and the open workspace's bar |
| `halo` | `--halo` | `--net` | Hover and drag-over emphasis: card hover, screenshot hover, the reference-drop ring |
| `labelAgent` | `--label-agent` | `--deleg` | An agent's name: `.who` labels and sub-agent chips |
| derived | `--brand-wash` | `--net-wash` | Tint behind `brand`; a pack that sets `brand` derives it at 12% over its ground, as `--accent-wash` follows `accent` |
| derived | `--label-agent-wash` | `--deleg-wash` | Tint behind `labelAgent`, derived the same way |

- `--halo` is not `--glow`: `--glow` is the running compose ring's own local
  variable, set to `--net` while a turn runs, and stays a status.
- The running compose ring, call lines, pips, carets and the delegated nest keep
  `net` and `deleg`.

## Faces

The interface ships its own faces. On Windows it otherwise resolved to Segoe UI
and Microsoft YaHei, neither of which carries a 500 or a 600: CSS then rounds
`--w-reg` (500) down to 400 and `--w-med` (600) up to 700, collapsing three
weights into two and thinning the body text. Both shipped files are variable, so
every step exists on every platform. Licences sit beside them (both OFL 1.1).

The Chinese face is subset to GB2312 plus the interface's own characters —
1.6MB instead of 14MB. A character outside the subset falls through the stack
rather than disappearing.

The shipped mono face has no geometric shapes (U+25xx) and no `✗`; those fall to
the installed Noto Sans SC, so the shape is the same cross-platform but the
advance is not a monospace cell. A mark that has to stand in the same column as
`✓` must be `×`, which is in the face and shares its width.

`--mono` names the shipped Chinese face because terminal output and diffs carry
Chinese and none of the monospaced faces has a Han glyph — the character fell
through to whatever the OS picked, a serif on Windows. Alignment is unchanged: a
Han glyph is 1em against a 0.6em Latin cell either way, so it was never a 2:1
grid to break.

## Surfaces

- `--float` / `--float-hi` exist apart from `--raised`. In dark, `--raised` and
  what it covers are nearly the same lightness, and a black shadow on a
  near-black ground paints nothing, so a menu read as transparent. A floating
  layer is one step lighter, carries an inner highlight, and throws a deeper,
  wider shadow; all three together are what separate it.
- `--face-sunk` and `--face-code` are two faces because machine output and a set
  listing are two things. One mix served both and stepped down from `--raised`
  while the column actually lays `--surface`: measured, the step came to 2.7 L
  in dark, just past the line where a layer is visible.

## Hues

- A hue used as a surface and as text on a tint of itself needs two values. The
  `*-ink` token is the reading value on a tint of the hue; the base stays the
  fill. In dark the two coincide, because there the tint is darker than the hue.
  The light `--accent` base was once dark enough to carry white (#A76200, white
  on it at 4.4977 — not 4.5, with nothing left over); a fill that dark is brown,
  and it is the largest saturated area the light scheme has. It is the hue
  itself now, with dark ink on it. `--err` carries its own ink for the same
  reason: a danger button borrowed `--accent-fg`, which held only while the two
  hues happened to share a lightness.
- Text takes the ink, never the base.
  - Light `--warn` reads 4.58:1 on white and 3.86:1 on the floating layer, under
    the 4.5 line.
  - A label, count or note coloured `--warn`, `--ok` or `--err` uses
    `--warn-ink`, `--ok-ink` or `--err-ink`.
  - Borders, washes and glyph marks (status symbols, check ticks, hover icons)
    stay on the base: a graphic needs 3:1, which the base clears.
  - `ink-text.test.ts` fails a bare hue as `color` outside its glyph list.
- An ink is read on every tint it lands on, not only the lightest one. The
  light `--accent-ink` cleared the popover's own surface at 6.4:1 and the
  selected row inside it — that same surface with 7% text mixed in — at 4.40,
  under the line the rest of the scheme holds. The darkest surface an ink lands
  on is the one that sets its value.
- One colour, one job. `accent` = needs you · `net` = running on its own ·
  `deleg` = delegated out · `ok` = succeeded · `warn` = allowed, with a cost ·
  `err` = broken or destructive · `ghost` = a neutral fact. `focus` is its own
  token: a focus ring must not share a colour's meaning with "running".
- `accent` and `warn` were the same hex, so "needs you" and "risky but allowed"
  were indistinguishable on screen. `warn` sits at H45 — amber is "your turn",
  orange is "this step has a cost". Measured against `accent`'s tier: dark
  8.14:1 and light ink 5.83:1, beside `accent`'s 8.35 and 5.77.
- `--cat-1` … `--cat-5` are categorical, not status.
  - Borrowing deleg/net/accent/ok/faint spends five status positions on
    something that is not a status.
  - That borrowed set failed four of the six checks a categorical palette must
    pass. Worst: "tool output" (grey) against "model reply" (green) at ΔE 12.0
    for normal vision, under the floor of 15, while those are the second and
    fourth largest areas in the window.
  - The current set was chosen against all six and verified in both schemes:
    lightness band, chroma floor, adjacent-pair colour-vision separation
    (ΔE ≥ 8), normal-vision floor (≥ 15), and contrast with the ground.
  - In light, four segments fall below 3:1 and rely on the legend's text labels.
  - Chart series take `--cat-1` … `--cat-5` by index. A sixth series onward
    repeats the ramp with a second channel (dash for lines, hatch for bars,
    lower opacity for slices) because five distinguishable hues is the ceiling,
    and every chart carries a legend or a data table so colour is never the only
    way to read a series.
- `--lights-w` / `--lights-h` are the window corner macOS draws its controls
  over, and they are zero everywhere the shell does not hand that corner to the
  page. Whichever element occupies the corner reserves them, so a layout change
  that moves what sits there does not have to rediscover the measurement.
- `--call-sym` / `--call-gap` are the transcript gutter: the symbol cell and the
  space after it. Anything that has to line up with a card reads them instead of
  restating the sum, and `.nest-bd` redefines them rather than re-specifying the
  grid.
- `--io-up` / `--io-down` alias the categorical set, not the status one: sent
  and received are two kinds of one quantity, not two healths, so `net` or `ok`
  there would read as "running" or "succeeded". `--io-down` is `--cat-5`, which
  already is this figure — the context bar's output segment.

## Shape

`--chev` masks one stroked polyline wherever a corner is drawn, with the colour
still coming from `currentColor`. Tree corners were stroked polylines while
disclosure arrows were the font's solid triangles: one meaning, two shapes.

Radii were 1/2/3/4 — near square, and every container in the window is stroked,
so square read as table cells rather than operable surfaces. The whole tier was
raised with its ratios kept.

## Motion

- Three durations for three weights: `--t-line` a line, `--t-card` a card,
  `--t-panel` a panel. `--m-*` bundles the duration with its easing, because a
  weight decides both; `--m-step` is choreography — how far apart a batch of
  elements is staggered.
- `--t-live` (.6s) is not a weight. A meter or a fill reports work that is
  running, and reports it by growing, so this tier drives a fill's own geometry
  and never answers a click — at .6s it would answer late.
- `--t-press` (.06s) is contact itself; anything slower reads as the interface
  thinking it over. What a press looks like is a token, so reduced motion
  answers for every control by changing one value.
- `--ease-spring` is the overshoot tier; the other three are monotonic, so
  everything lands the same way and nothing has weight. Use it only where an
  action has just settled.
- Motion describes a state transition, never an element's lifetime. Mount and
  remount are not causes. A live fact may enter once; replay and virtualization
  do not replay motion. Navigation moves the containing view, not every child
  mounted beneath it. Held by `src/ui/motion.test.tsx`, including against this
  stylesheet, because the tempting rule and the correct one differ by a
  selector.
- Under reduced motion a press is still answered, but not by displacement. The
  wash stays: colour is not motion.

## Space

- `--y-line` / `--y-card` / `--y-panel` are the same three weights in space.
  With only the durations tiered, a one-file read and a call carrying twenty
  lines of output weighed the same, and a long turn had no readable sections.
- `--y-say` sets the reply's own margins. Machine activity sits 22px apart, a
  reply against machine activity 32px, two utterances 42px — the ladder's
  direction was right and its steps were too small to read as a change of
  speaker.
- `--y-close` / `--close-h` are the close: what a card's last line sits above
  the action that ends it, and how tall that action is. The close is one step
  below `--y-line`, because an action belongs to the sentence it acts on rather
  than standing apart from it; a card that ends must read as finished, not as
  waiting for something that was never laid out.
- That action is present for the whole life of the card. A control that appears
  only under a pointer is one nobody finds, and on touch it does not appear at
  all; it rests at `--muted` and comes forward on hover.

## Type

- The scale carried 24 sizes, 442 uses of them inside the four steps between 10
  and 11.5px. Half a pixel is not a level; it reads as nothing quite matching.
  Six steps now, widening as they go. The floor is 11px: dense Han strokes lose
  their counters there first, and AA's large-text exemption starts at 18.66px.
  Above 24px is off the scale — a brand moment is one size for one thing.
- `--read` is the conversation body's own size, separate from interface zoom, so
  "I cannot read this" adjusts one thing. The value here is the first-frame
  fallback; the real default is `look.ts`'s `readDefault()`.
- `--column` is the reading column's width. The composer is laid at it in
  every measure; the transcript takes it through `--doc`.
- `--doc` is the column the transcript is laid in: `--column`, or the whole
  pane in the full measure. Cards, prose and the composer share its edges.
- The column's floor is its narrowest block: 80 columns of IBM Plex Mono at
  `--fs-ui` is 624px, and the transcript's indent and padding want 82px more.
- Line length moves with `--read`: about 80 Latin characters at 15px, about 36
  Han characters at 16px. Column width and body size are one reading.
- `--srail-w` is the locator rail's own column. It is a `pointer-events: auto`
  strip laid over the transcript's right edge, so content that clears it must
  reference the same number.
- Tracking varies with size rather than staying constant: large sizes need the
  air between letters pulled back, small caps need it opened or the word packs
  together. The Chinese tier is zeroed by `:lang(zh)` at the top of `app.css` —
  negative tracking squeezes a Han character's em box, not the air between
  letters.

## Weight and contrast

- Stroke weight is an axis beside contrast, not part of it: however bright the
  colour, 400 Han at 11px is thin. Han strokes are dense and blur at a size
  Latin still holds, so the Chinese interface starts at medium.
- The `heavy` tier steps further for Chinese, whose default is already the Latin
  bold tier, and stops at 700: above it a Han character's closed strokes fill in.
- Text contrast has three tiers; backgrounds and semantic colours do not
  participate. The floor is AA body text: `--ghost` must reach 4.5:1 on the
  darkest ground it actually lands on, because it appears at 10–11px and the
  large-text exemption starts at 18.66px. That ground is `--page` — the rail,
  the side column and the chrome all lay it, where `--ghost` appears two orders
  of magnitude more often than on `--raised`. The default tier starts at that
  line and the other two step up; the body end does not move, because near-white
  on a dark ground haloes and narrowing the span is what "softer" means.
  Enforced by `perf/contrast.mjs`.
- `--ghost` is `--faint`'s value in every tier: a lighter one fails the floor.
- `perf/tiers.mjs` holds every tier: each ink is at least as legible as the
  default palette stepped once (3.0/10.1, 3.5/7.5 and 3.5/7.0 lightness points
  for text, muted and faint) and as the floors recorded there, on both grounds.
- `prefers-contrast: more` is followed when the user has not chosen a tier.
  Light and dark are judged separately: a manual dark choice under a light
  system scheme would otherwise paint the light theme's dark body text onto a
  dark ground.
