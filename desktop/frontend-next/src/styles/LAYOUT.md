# Layout law

`app.css` carries no commentary. The decisions behind its rules live here, in
the order the stylesheet makes them. Each entry states what holds now; where a
gate or a component enforces it, that is named.

`TOKENS.md` holds the vocabulary — colour, type, motion and space — that these
rules are written in.

## Reset and the window

- Scrollbars are styled once, on `*`. Declaring them per container means the one
  that was missed (the settings sheet was) drops to the system default. Both the
  standard properties and the `::-webkit-*` pair are written: new WebView2 reads
  the first and ignores the second, older shells only read the second. A
  container that wants no scrollbar hides it itself.
- The transcript card resets both standard properties to `auto`: while either
  is set Chromium ignores `::-webkit-*`, and the system scrollbar's buttons and
  track run through the card's rounded corners.
- `body` divides `100vh` back out by the zoom. Zooming the interface leaves `vh`
  measured against the unzoomed viewport, so `100vh` overflows by exactly the
  zoom factor — which is how the composer was pushed off the bottom of the
  screen.
- `-webkit-font-smoothing` stays behind the `[data-platform="darwin"]` guard. On
  WebView2 it disables ClearType, and Han characters at small sizes go soft.
- `font-synthesis-weight: none`. Synthesising a bold strokes the outline wider,
  and Han strokes are dense enough that wider is blurred. Faces with a real
  Medium (PingFang, Noto) use it; faces without one (YaHei) fall back honestly.

## Chinese typography

- Small-caps labels are Latin typography. The mono stack has no Han glyphs,
  tracking pulls a word apart, `uppercase` means nothing, and 10px is under the
  readable floor. The English rules stay as they are; `:lang(zh)` catches the
  Chinese case, because one class carrying two scripts is two typographies.
- Section headings are split out for the same reason. Small caps do not exist in
  Chinese, and the two remaining signals — smaller, lighter — both say
  "secondary". The Chinese tier keeps the size and darkens the ink, leaving
  hierarchy to weight alone. `.side .lbl` is listed there for specificity: the
  right column's same-named rule comes later in the file.

## Wallpaper and sky

- `body::before` is the theme's picture, when a pack ships one. It sits behind
  everything under a scrim of the page colour, so text keeps its ground.
- `.sky` is a live background a pack draws, not a picture it placed. The layer is
  clipped by an ellipse at the top right, takes no hit testing and does not
  scroll: it is depth behind the window, not content in it. Reduced motion stops
  its animation and draws the clouds one frame (see `Sky.tsx`).
- With a picture or a sky, only the empty page lets it through. Cards, panels and
  the composer stay opaque: a background is worth a window's margins, not its
  content.
- The rail, the side column and the tab strip had no ground of their own — they
  borrowed `body`'s — so making `.app` transparent let the picture run under
  their text. A surface owns its ground.
- Those three cover the window, so nothing of the picture is left. With one
  loaded, each surface steps back to frosted instead: the empty page shows the
  picture, content takes its own ground, and the picture keeps the columns and
  the margins. A thick scrim and a wide blur are two ways of losing the picture,
  so both are kept small — the text stands on its own shadow, which costs a few
  pixels around each glyph rather than the whole column.
- A transcript row is not a card but an unframed line, so the picture would run
  under the prose. `.flow:not([data-empty])` takes its own ground the moment
  there is content. The run header and the composer are the middle column's two
  opaque ends; both follow `.flow`, or the picture survives only in the band the
  prose sits on.
- `.hero` is the one text left standing on the picture, so it gets a ground that
  fades out from its centre. A box would delete the emptiness itself, which is
  what the empty state is for.
- The grain is laid over everything rather than under it: every surface that
  colours itself — both columns, the middle, the cards — would cover a layer
  underneath, leaving texture only in the corners. 2–3% does not hurt body text.

## Title bar and chrome

- `-webkit-app-region` is Chromium's drag property. Inside the shell this row
  *is* the title bar, so it has to be the drag handle, and every control on it
  opts back out. Membership is what the markup says is interactive, not a list of the
  controls that happen to be on the bar today.
- macOS floats its window controls over the window's corner, not over this row.
  Whichever element owns that corner reserves them — `.chrome` collapsed, the
  rail head open — and both read `--lights-w` / `--lights-h`. The swap inside
  that width is opacity alone, so nothing after it moves.
- The shell pins the lights with `trafficLightPosition`: AppKit's own placement
  sits them above the line this row centres its controls on. The inset is
  measured from their top edge, so it is that line minus half a control.
- Windows puts its controls at the other end, hard against the corner, and they
  are the only controls here that are square.
- The wordmark is masked rather than an `<img>`: the file ships one hard-coded
  blue that reads as a status colour here and goes muddy on a dark ground.
- `.crumb` needs `flex: 1` with `min-width: 0` together, or a long target pushes
  the whole bar wide instead of ellipsing. Clipping is separate: `min-width: 0`
  lets the breadcrumb narrow, but children default to `overflow: visible`, so a
  long name still paints outside the box and crosses the bar over the controls
  on the right.
- The separator between crumbs is a written character, not the 1px hairline
  `.sep`. Sharing that name puts the glyph in a 1px-wide box with a background,
  and the strokes spill out of it.
- Only identity is left on the top bar — which project, which session. The goal
  and the measurements moved to the run header.
- A card the bar opens is a `[role="dialog"]`, and a dialog opts out whole:
  the drag rule claims every non-control descendant of the bar, so without the
  exemption a card's text and its QR code would be title bar, and a press on
  them would move the window.
- The phone card's dot is the door being open, not a notification: a live
  listener is state the person at the window should see without opening it.
- The bar's `backdrop-filter` makes it a stacking context, so a card's own
  z-index only ranks it inside the bar, and the panes after it in the DOM take
  the hits it paints over. While a card is open the bar itself is lifted; a
  press on the card otherwise lands on the pane and closes it.

## Pickers and menus

- A menu drawn outside its anchor's box cannot be dismissed by the pointer
  leaving that box: the offset between them belongs to neither, and crossing it
  is how the menu is reached, so it closes exactly when it is being aimed at.
  `useDismiss` — pressing away, or Escape — is the one dismissal.
- An offset that must stay hoverable, as under a reading that opens on hover, is
  spanned by a pseudo-element on the anchor the size of the offset.
- A picker is as long as the user's config: `[[providers]]` has no ceiling, so
  one gateway account can list a hundred models. The filter field is sticky
  because the rows are the scrolling part — a field that scrolls away takes the
  query out of sight while it is still filtering — and it stays typable when the
  menu locks selection.
- One transition in and out rather than keyframes for entry and nothing for
  exit; `display` rides `allow-discrete`.
- A Picker's menu renders into the body, so `left`/`top` are measured and written
  inline and nothing above it in the tree can clip it. `.modemenu` is the
  composer's policy menu, which is not a Picker and stays in place. Its
  `anchor()` fallback is load-bearing: `anchor-name` hangs on
  `[aria-expanded="true"]`, which flips the instant the menu is asked for.
- Every menu answers one question — which one is it right now — so it gets the
  rail's answer: a fill and a marker. `[data-lead]` is what Enter takes while the
  field still holds focus, drawn at hover's weight deliberately: it is a standing
  offer, not a selection.

## Slash and file completion

- The completion completes the line being typed, so it sits against the
  composer's top edge and takes its width. On a wide window filling the composer
  throws the category label hundreds of pixels away, and the eye has to cross the
  whole row to read one entry.
- Only the list scrolls. A key hint inside the scroller covers the last row while
  `scrollIntoView` still counts that row as visible, so the first step down does
  not scroll.
- A directory can be walked into and a file cannot: that difference is worth a
  colour. The matched letters are marked so fuzzy matching does not look
  arbitrary. On the selected row `data-on` is the caret, not a stored value, and
  it has to be readable in peripheral vision; its marker is the same bar the rail
  draws on a workspace with a pane open in it. The description steps up one tier
  with the row, which also brings it back over the AA line.
- Hover means "you could take this"; `data-on` is what Enter takes. A pointer
  parked over the list made the two indistinguishable, so while `data-kb` is set
  hover yields. Held arrows outrun a .13s fade and light three rows at once, so
  the transition drops under `data-kb`.

## Skill chips in the composer

- A chip is text the textarea holds (`/name`) drawn by `.chipmirror`, a copy of
  the line laid exactly over it. While a chip is present the textarea's glyphs
  are transparent, so caret, selection, IME and undo stay the native control's.
- The mirror copies the textarea's computed metrics rather than restating them:
  the composer's font, zoom and line height are set in several places, and one
  missed rule shifts every chip.
- The chip's wash is a `box-shadow` spread, not padding: padding changes the
  width of the run and the caret lands a few pixels beside the letter it is on.
  Weight is left alone for the same reason.
- Scrolling follows the textarea's scroll timeline (`--draft`), with `--travel`
  set to its scroll range. A scroll listener would be a user-input registration
  the action census has to name, and scrolling a draft is not an action.
- Skills take `--net`, subagents `--deleg`: the same split the rest of the
  product uses between work done here and work handed off.

## Composer shelf

- A mode is this turn's capability, not a preference, which is why it lives on
  the composer and not in settings. A pill is its label's width or it is nothing:
  shrinking one squeezed the shelf's own controls, and Chinese wraps.
- The pill's value is what it says, not a second plate stuck onto it — weight and
  colour separate it from the name. A press travels to the bottom and springs
  back, or the row feels like glass on a trackpad. While its menu is open the
  button stays pressed, or opening a menu leaves its source looking unclicked.
- `.mode.tog` off is the state every turn starts in, so it is drawn as text
  rather than as another chip on the shelf. Turning it on strokes the outline
  along its own path, with `pathLength` normalised so the four segments draw
  independently. The wash arrives from the left, the same direction as "think
  first", on its own layer so it does not fight `background` with hover.
- `.mode.danger` breathes: gates fully open is a standing state, not an action.
- One hairline separates "who does this" (the model) from "how this turn runs"
  (the three to its right).
- A segmented control is only for a choice between tiers with no better and
  worse — the execution setting is one.
- `.pip` is for a request sent and not yet answered by the kernel: it must
  neither pretend the switch happened nor show nothing at all.

## Run state

- The `R` mark is the one flourish in the whole design. Three strokes drawn in
  order, a beat, then erased and rewritten: reasoning proceeds a step at a time,
  so its indicator draws a stroke at a time rather than spinning. Halted, it
  freezes where it stopped — reasoning stopped half way is exactly what that
  means. Done is persisted state, not proof that completion happened on this
  mount.
- The rail's session rows answer to the same distinction. A run's state is a
  2px, 14px-tall tick at the row's left edge, square-cut and set 3px in: drawn
  rather than an inset shadow, because a shadow in a rounded row can only
  follow that radius and a short mark cannot. It is one mark in three colours —
  accent, warn, ok — so the row says which state it is in without the row
  itself becoming the signal.
- Finishing is the one rail state that is an event rather than a state, so it
  alone is animated, and only for the run this window watched cross into
  `done`. A session that was already done when the rail mounted, or that comes
  back into scope, draws its tick and nothing else. `data-just-done` carries
  that event and `data-run="done"` carries the state; keeping them apart is
  what stops history from replaying as news.
- Unread is a fact the kernel owns, drawn beside the run tick and never merged with it: an 8px dot at the row end, so titles keep their column and it cannot be read as the project's status dot, a semibold title, and a spoken "未读" on the dot.
- In dark an unread row also takes a 4% tint, below hover and selection. In light it would sit one step from the selected row, so light has none. A remote row keeps its dot left of the delete button, and its turn count shifts left to make room.
- A folded project row sums the unread sessions under it into an outlined count among its action buttons, unlike the filled feedback badge, and drops it when the project opens. A pane tab carries a 6px dot; the tab strip is currently hidden by the Studio sheet. Nothing here animates.

## The queue

- These are lines said and not yet read. What the strip has to answer is "did
  that land", so the state is a chip on the row. The count and the byte total
  each refuse alone: a header showing one of them is wrong about the other
  exactly when it matters.
- The number of entries is capped (64); their height is not. The head stays put
  and the rows grow; total height comes from `.composeaux`, and the rule here
  only says that the rows scroll rather than the head scrolling away with them.
- `steer_consumed` means this turn has already read the line into its context:
  it cannot be taken back, and `AckDequeue` removes it when the turn ends. It
  stops being a card and becomes a record — still visible, because a crash before
  the write has to leave it on screen, but no longer a card's height. Its border
  goes transparent rather than away, or the column loses its alignment.
- Its actions are quiet but present: a row of controls that exists only under the
  pointer reads as a row that does nothing.
- Everything stacked above the input shares one ceiling, `--aux-budget`, so a
  full queue scrolls inside it instead of taking the transcript.
- The studio composer's chrome is taller than the one the budget was set
  against, so studio lowers it (28vh, 19vh when short): with 64 queued lines the
  transcript keeps 40% of the pane, which `perf/budget.mjs` holds.
- The queued line is the only thing on screen that has not happened yet, so its
  cancel sits beside "queued" and stays visible. A cancel revealed on hover is no
  cancel.

## Columns and the seam

- Column widths change on `.app`, so the tween belongs there and `.cols` only
  reads the result into a track. The tween must be cut during a drag: a .34s
  tween makes the column chase the pointer.
- The side track is closed unless a panel mounts in it. It was written open and
  closed by `[data-side="off"]`, and the attribute stayed wired to the built-in
  browser after that browser moved onto the workbench. Nothing rendered there
  and the shell still reserved 308px. `shellcolumns.test.ts` holds it.
- Pane chrome has one left edge: the view switcher, the workbench's tab strip
  and the browser's toolbar all start at `--pane-gutter`. The switcher took the
  reading column's inset, right under a conversation and wrong under a
  full-bleed canvas — it floated 278px in from the strip below it.
- A row nobody bounded is why the file tree had no scrollbar. `overflow: auto`
  was already on the list; the workbench body's implicit row is `auto`, sized to
  its content, so the column grew past the panel and was clipped instead.
  `grid-template-rows: minmax(0, 1fr)` is the half that makes the overflow real.
- The workbench is hidden, not unmounted. It holds the open files and a browser
  per tab, each with its own history, and a glance at the conversation must not
  cost them. `.scroll[data-pane="browser"][hidden]` was already written for it.
- The workbench's divider shares the browser's grid cell rather than being inset
  into the pane: that cell already starts under the nav and ends at the window.
- It is in flow there, not absolute, so `inset: auto` is load-bearing:
  `.gutter-r`'s `right: -6px` becomes a relative offset and cancels the negative
  margin that straddles the seam.
- Both seams are drawn. The studio layer had hidden `.gutter-l` and pinned
  `--rail-open` with `!important`, so the rail had no handle and a dragged width
  was read, stored, written inline and then overridden every time.
- `.gutter-l` outranks the rail because the rail is `position: fixed` at 12: a
  seam painted under the panel it divides cannot be grabbed where it matters.
- The rail's floor is 232px (`RAIL.min`); a narrower saved width is raised on
  read.
- Under 232px the session filter row cannot hold four labels in either
  language.
- The rail head is a size container, and its threshold is a content-box width,
  not a rail width.
- The threshold is 220px: the content box of the default 264px rail, which has
  22px side padding. The default and anything wider show counts and shortcut
  hints.
- At 219px or less counts and hints are dropped, the filter cells may shrink
  and labels ellipsize.
- Inside a window of 860px or less the padding is 16px, so the cutoff falls at
  a 252px rail.
- The rail lists every mounted workspace, each folding over its own sessions.
  The studio layer had hidden all but the focused one and relabelled its
  children 「最近」, so a count of six sat above a list of one. The switcher above
  still names where a *new* session lands; the tree is how an existing one is
  reached.
- Only 「这台机器」 is hidden, by `[data-here]` rather than by `.machrow`: the
  section label above already carries that name, its count and its add button,
  while a remote host's row is the only thing naming that machine.
- A collapsed column is inert, not only narrow: at zero width its controls are
  still in the tab order, and focusing one scrolls a column nobody can see.
- The rail and the workbench are both `inert` while collapsed, and the workbench
  stops painting once the collapse has finished. The shortcut and the gutter
  that reopen a column sit outside it, so they stay reachable.
- Collapsing reflows the text inside a column, which is what makes it feel
  wrong. The contents are locked to the open width so they are clipped rather
  than squeezed, and they shrink faster than the container: what is seen is
  something being taken away, not something struggling.
- The divider already exists as the panel's own border; the gutter only adds a
  hit area wide enough not to aim at. It shows on rest and .14s late — passing
  the pointer over a line should not light it. Neutral, because `accent` means
  "needs you" and a draggable boundary is not that.
- `.gutter-l` is at -6, not -5: the panel's 1px border sits outside this box, so
  the seam's centre is half a pixel further out. While dragging, the handle
  tracks the pointer and `transform` must not tween; releasing drops `data-on`
  and hands the transform back. Reaching a limit by keyboard nudges the line, so
  a focused separator has to be visible to show it.
- The grip is one switch, one shape, three weights, and it lives on the divider
  where the hand already is. Three dots at a weight that survives a glance: the
  previous resting state was a single chevron at 45%, invisible. Against the
  window edge it moves in two pixels or the frame clips a corner off it. Shut,
  there is no seam left to drag and only one thing left to say — and it is the
  only way back, so it stops being a hairline and becomes a tab. The side
  column's horizontal tier lays the same handle on its side.

## The flow

- `.scroll` needs `flex: 1`: with an empty flow it would not stretch, and the
  composer would be pushed to the top.
- `scroll-behavior: smooth` is not used. Resetting `scrollTop` every frame during
  a stream restarts the smooth animation continuously, which pins the main thread
  and defers every `setTimeout`. Scrolling is written directly from JS.
- `.ovf` is the overflow bubble a truncated line gets. The native `title`'s
  delay, position, size and scrolling all belong to the browser, and a long
  preview becomes a floating block that cannot be kept inside its bounds; this
  one has a ceiling, scrolls itself, and its text can be selected. Its position
  is measured by `place.ts` and written inline. `[data-cut]` takes the help
  cursor: a mark only where there is something to read.
- `.jump` hangs off the composer's top edge rather than a written `bottom`,
  because the textarea grows to 96px.
- `.flow-edge` fades content cut off under the tab strip. It rides a scroll
  timeline, not a scroll listener; without one it stays at opacity 0, which is
  today's hard edge. The `@supports` guard is load-bearing — a zero-duration
  animation would otherwise pin it visible.
- The reading column is centred rather than right-aligned. Before the column had
  a ceiling the two were the same thing; once it narrowed they parted, and at
  1920 a right-aligned column leaves four hundred pixels on the left while the
  composer stays left. `margin-inline: auto` splits the reserved space to both
  sides, so two rail widths are reserved, not one: half of one lands on the rail
  side and the prose reaches under a strip that takes every pointer event, where
  a click is a jump rather than a selection.
- The reading measure is one column shared by the transcript and the composer.
  `.flow` is laid at `--doc`; the composer, its aux shelf and its notes at
  `--column`. A card's edges are the composer's.
- Rows inside the flow carry no ceiling of their own. A narrower cap on replies
  or user turns puts the steps card and the bubble short of the composer's edge.
- The full measure moves `--doc` alone: the transcript widens and the composer
  keeps the reading width, where the line being typed stays one glance.
- Below 680px both columns take the phone rule instead (see *Phones*).
- Docking the agent's browser puts the pane on a grid, and the browser's column
  spans the body's row and the composer's. Inside the conversation column `100%`
  is that column, so the composer's own centring keeps it off the divider, and
  the panel reaches the pane's bottom rather than the composer's top edge.

## Virtualisation

- Layout cost grows linearly with the number of cards, and the several hundred
  off screen are read by nobody. Settled cards mount in blocks and an unseen
  block leaves the DOM entirely, holding its place with the height last measured.
  Inside a block a second layer of `content-visibility` skips layout and paint
  for the part of it that is off screen. The last card is excluded: the one being
  written changes height every frame, and a structural pseudo-class cannot pick
  it without invalidating its siblings.
- `auto` remembers a card once it has been laid out; the fallback height is what
  a card the reader has never reached is assumed to be. `display: contents` on
  the frame between block and card keeps the card the block's child everywhere
  layout is concerned.
- `content-visibility` contains paint, so nothing a card draws may reach past
  its own box. The reply's action row sits 6px left of the text so its icons
  line up with it; the reply's box is widened by the same 6px (padding with a
  matching negative margin) so the hovered button is not cut in half.
- `.flow-end` answers "are we at the bottom" by being observed, which costs
  nothing per scroll, rather than measuring the whole record.

## The locator rail

- `--srail-w` is declared on `.pane` because three rules read it: how wide the
  rail is, how far the flow clears it, how far the composer clears it. Written
  separately they were 54 and 18, and in the 36px between them the rail was
  taking pointer events while the content believed it was still clickable — the
  right-aligned controls, the queued line's cancel among them, were unreachable.
- The host has zero height so it cannot take part in layout, which also means a
  percentage height there is zero: the rail's height is written in at measure
  time by the code already reading the scroller's geometry.
- The rail replaces the scrollbar, so it is always present and usually very
  faint, and it inherits the scrollbar's feel: it can be dragged. The viewport
  box is the far edge; a mark's hit area is to its left and the two do not
  overlap. Overlapping, the mark's 54px box covered the whole viewport box and a
  press never reached it.
- A mark's hit area is far wider and taller than its line: the target is 2px and
  nobody should have to aim at it. At a 9px step adjacent boxes overlap and the
  later one covers the earlier; `--lit` decides who is on top, so the one under
  the pointer always is. `--lit` is how near this mark is to the focus — 1 is the
  focus itself, 0 is far enough — and both length and opacity are derived from
  it, so at rest every tick is the same length and equally faint.
- The transcript draws no native scrollbar: the viewport box and a scrollbar
  thumb say the same thing, and the one that can be marked is the one kept.
- A jump marks the call's identity only. Flashing a long output would draw the
  locator's hint as a selection.

## Cards

- A call is a grid. No ground is the normal state; the coloured categories
  (net/write/ask/deleg/mcp) each take their own below, so a ground is itself the
  signal that this step did more than read.
- The gutter line is 2px and coloured by category. At 1px a hairline is not on
  the screen at all, and the timeline's structure lived only in the code.
- Inside a sub-agent panel a step is a sub-step: the gutter does not widen with
  it, or two levels at one width cannot be told apart.
- The category colours are the semantic ones and each matches its sentence —
  `net` is the network, `deleg` is delegated out, `ask` is "your turn". Only
  write does not: writing a file needs nobody, it is "this step has a cost",
  which is the sentence `warn` was split out of `accent` to carry.
- `[data-k="me"]` opens a turn. Scrolling back, it is the only anchor, and it
  used to be as light as a tool call's header. The turn number is deliberately
  absent: the rewind entry is on the card, so there is no number to match by eye.
- User message controls sit below the bubble, ordered copy, edit, rewind.
  They stay icon-only with names, hints and keyboard focus. Inline-size
  containment keeps the row from widening the bubble; wider rows extend left.
- The row appears on message hover, keyboard focus, or an open rewind menu.
  Devices without hover keep the controls visible.
- `[data-k="host"]` is what the host did itself. A transcript has three authors —
  you, the model, the host — and `data-k` only knows tool names, so the host's
  cards landed on the default grey beside an uncategorised tool. A dashed line is
  the one dimension of this gutter still free: colour is category, length is
  duration, motion is state.

## Four weights

The turn's anchor, the final answer, the steps in between, and the things that
need a decision. All four shared one card weight, and a screenful could only be
told apart by the colour of a symbol.

- The answer is the turn's product, not another timeline entry. `--muted` is for
  process and the answer uses `--text`; its symbol steps back to a mark rather
  than a ringed button on a ground. Filled, and in the reading ink: a 1.5-stroke
  ring at `--ghost` is where "not a button" had gone all the way to "not there".
  Every other mark fills its 28px cell, so a mark that does not leaves the rail
  reading as if it ran behind them.
- The answer and machine output differed only in colour and line height, so a
  column of them weighed the same. One step of the type scale separates "this is
  the answer" from "this is process"; machine output stays at `--read`, so only
  one side moved.
- The steps in between are scanned, not read: one step, one line, with long
  arguments elided rather than wrapped. Wrapping makes cards of one kind differ
  in height across a screen, and the hierarchy then has to be worked out card by
  card. The anchor, the answer and the decisions may still wrap: those are read.
- A tool's headline names the call, which is the only way to know which one it
  is. Name and tag answer one question (what tool, in which shell); the argument
  answers another (what this call did). All three were 8px apart, which reads as
  three parallel things.
- A read's headline and a write's headline weighed the same. The consequential
  categories stay at `--w-med` and the rest step down, so scanning a column shows
  where the turn actually changed something.

## Entrances

- Weight decides the entrance: a line fades, a card carries a displacement, a
  panel opens from its top edge.
- Motion describes a state transition, never how long an element has been alive.
  Mounting is not a cause: virtualisation brings a block back and remounts every
  card in it, and replaying the entrance says an hour-old fact just happened. An
  entrance is therefore a one-shot debt, owed by the projection and spent by the
  first paint; `.enterbox` generates no box and only carries the mark.
- A sub-agent's own steps appear while it runs, which *is* a fact arriving — it
  just happens inside a card rather than beside one.

## Running

- `「正在读取…」→「读了 7 个文件」` is this step's one state transition; do not
  spend it on a 0ms swap.
- While running, the symbol is grey and takes its category colour on settling, so
  the colour is itself the completion signal. The symbol used to also carry an
  endless pulse, which with the light travelling down the line put two looping
  signals on one card saying the same thing. The travelling one is kept: it says
  something a pulse cannot — that this step is advancing rather than stuck. It
  uses the structure already there and adds no element, and its direction means
  something: down is "on to the next". It stops on settling, so history is
  always still.

## Trajectory

- The table holds nothing but times and made the reader subtract them. A bar
  scaled by duration draws "which step was slow" and "which ran together"
  directly; no column moves, the bar is an added one.
- A duration longer than the axis is possible — a sub-agent's clock starts at its
  own beginning — and the overflow is clipped rather than allowed to paint over
  the next column.
- An instant event has no length and is drawn as a tick: giving it a nominal
  width would misreport a duration. The round is the trunk: thicker, neutral,
  with the tools' coloured ticks laid on it.
- Only the record types that reveal an internal decision are coloured; a plain
  event stays neutral.
- The export bar sits above the table because exporting is an operation on the
  whole table, not on a row.

## Run graph and timeline

- Activity is what was said and the trajectory is the machine's record; neither
  answers "who did what, and then who". The timeline reads the same published run
  graph along the time axis; the graph reads it along the other one — who is
  waiting on whom.
- A step's spine runs from its own dot to the next one, with the first and last
  segments ending on their dots, or the line hangs off both ends.
- The running step is framed: on a page of collapsed rows, which one is moving
  should not have to be read.
- The chevron is drawn, not typed: the mono subset has no U+203A.
- Files changed hang under the step that changed them and stay visible collapsed,
  because "what did this step produce" is exactly the question a collapsed row
  should answer. The side column's global list covers the whole trip and cannot
  say who.
- The legend is about line style, not colour: solid means the upstream answer
  actually entered the downstream context, dashed means only an ordering was
  recorded. Those are two different edges in the kernel, and one line style would
  say they are the same thing. Colour follows the style so a reader who cannot
  separate them still reads the graph.
- The graph is routinely wider than the panel: it scrolls itself rather than
  widening the window or dragging the header along.
- A node's meta is one line: the status word never wraps, and the portrait and
  model after it are what get squeezed. "Waiting on" is the one question this
  view can answer that a list cannot, so it displaces the usual meta.
- Status is both a colour and the word on the card — colour never carries the
  meaning alone. Queued is not pending: it wants to run and the concurrency
  ceiling is holding it, so its pip is alive at a slower breath, its border stays
  dashed (it has not run), and the word takes the accent — the only place on the
  screen where the ceiling is visible. Skipped and pending are both "did not
  run": a dashed border separates them at a glance from a card that did.
- An interruption stands beside the graph rather than in it. The graph's
  vocabulary has no term for work nobody is running, so forcing it into a node
  state would invent one.

## Right-column instruments

- A block heading's 8px below it comes from the draft; 3px let a short heading
  touch the prose under it.
- Both columns reserve the scrollbar's gutter so the whole column does not jump
  sideways when one appears.
- `待确认` is the only row on this rail a reader can act on now, so it does not
  share the grammar of the counts beside it.
- The wallet is the account's number and the rows above are this trip's — one
  card, one rule between two kinds of quantity. The reading is itself the refresh
  control: an extra icon competes for attention in the same row. Its label,
  disabled badge and fetch time are three things, and without spacing they read
  as one word.
- A spark needs room or it has no shape; it is part of the reading in that row,
  not decoration, so it takes the elastic middle while the number stays
  right-aligned. Colour belongs to the mark, not to one of its two callers, so
  the rule is scoped under `.rate` and none of it reaches the cost panel.
- A bar is drawn only where there is a real denominator (see `kit.tsx`'s `Bar`).
- The names list follows the count as two tiers of one group: the number is the
  conclusion, the list is its detail.
- The prefix moving is the cache fact worth interrupting for, drawn as a line
  rather than a pill: a pill beside the readings reads as another reading.
- Hit rate is made of the two input segments only; output tokens are not in that
  bar, so they get no swatch. The two rows are separate accounts: the upper is
  priced per hit and miss, the lower has one price. The rate is live only while
  characters are actually arriving — while a tool runs it should read `—`.
- The plan cursor runs down the list rather than each row colouring itself: where
  you are now is one glance.
- The strike is drawn rather than simply present, which is the difference between
  "just completed" and "was already complete". It is painted on the inline `.ln`
  rather than the flex child, so `box-decoration-break` draws one per wrapped
  fragment; on the flex item a two-line step gets one line struck.
- An unreachable external service is one row: name left, destination right, and
  a red dot for the fact that it is broken.
  - The reason is not read here — it is beside `.srv` in settings, where it can
    be acted on.
  - A failure is not drawn in `.job`'s grey, which means "done".
  - A red dot plus the heading's count already say it twice; a third red bar
    would be a third saying, and no other block on this rail has one.
  - The name is the row's identity, so it is never the first thing clipped.
  - The fix link stays printed rather than appearing on hover: it is the only
    thing on the row that is not a status.
- `.sparkbox` is not named `.spark`: the component's own root already carries
  that, and a wrapper under the same name hands its layout to the chart. A
  `viewBox` with no width or height falls back to 300×150, a chart several times
  the height of the panel it sits in.
- A file row is a button when there is a diff behind it, and it drops everything
  a button brings of its own. git's own letter is coloured by what it means: a
  word says the same thing four times as wide in a column that has none to
  spare. The directory may be clipped and the filename may not — ellipsising
  `.p` as one unit took the filename first. A row flashes when the middle column
  has just landed a change, or two places move at once and the reader does not
  know which to watch.

## Reduced motion

- The three reload states already speak through colour and a line of text, so
  stopping the animation loses nothing.
- The down marker says "receiving" through its breath alone, so stopping it
  leaves two states identical; it takes a still form instead.
- The limit nudge is a displacement, and displacement is what motion sensitivity
  is about. A still dip in the line says the same thing.
- "Running" still has to be said when the travelling light stops: the line takes
  a solid, static form, and the composer's sweeping light becomes one still line
  that still says "this is taking effect".

## Folding the scene

- The order in which things yield is in `viewport.ts`; the rules here only say
  what is given up. The thresholds are not media queries, because a media query
  answers about the real viewport and zoom does not change it: at 1.3 the
  interface is a third larger while the layout still believes it has the whole
  screen.
- Yielding collapses a column, never removes it: removing it takes the grip on
  the divider with it, and the only entrance left is a shortcut nobody can see.
  - The right column is the point of this design, so at a narrow width it drops
    below the flow and stays scrollable rather than disappearing.
  - Laid out horizontally, a dragged width means nothing and there is nowhere to
    drag, so the handle is dropped only in that state — collapsed, the column is
    hidden entirely and the handle on the seam is the only way back.
  - Above the scene fold the left column is only narrowed to zero, so its
    column and seam are still there and hiding its grip would be the removal
    this section forbids.
  - Under it the rail is a drawer, not a column (see *Phones*). No seam is left
    to drag and the chrome's rail button is the entrance, so the grip goes; so
    does the workbench grip, whose entrance is the 「工作台」 tab.
- When both side columns have yielded, the transcript rail yields too; its ticks
  and viewport remain separately reachable.
- A short window divides its height proportionally, and what is left over is the
  transcript. What yields first is what is nearest the composer — the right
  column is already lying under the flow at that tier and is one of those
  shares. The transcript's share is a promise and the auxiliary budget is the
  variable.
- Scene folding is about the whole app; the composer should stack only when its
  own column is actually narrow, which is a container query rather than a scene
  tier.
- Two buttons side by side must be the same height: "do not ask again" is one
  size down from "refuse", and centred their top edges differ by 1px — which
  reads as one unaligned row and counts as two.

## Phones

The scene fold is where the window is a phone: a paired device's browser, or a
window dragged that narrow. What changes there is how things are reached, not
what exists.

- The rail is a drawer over the conversation. Its grid track is zero whatever
  `--rail-w` says, so opening it covers the pane instead of squeezing it to a
  strip. `.railveil` dims what it covers and closes it on a tap.
- Going somewhere from the drawer — another session, the settings — puts it
  away (`useDrawerCloses`): on a phone the drawer is a route, not a column kept
  beside the work.
- The docked workbench is the pane's whole width. Beside a 390px conversation a
  560px dock leaves neither usable, so while it is open the conversation and
  the composer step aside; the 「对话」 tab brings them back.
- Under 760px the workbench has one column, so the explorer and the canvas
  take turns in it: the files toggle (`data-files`) swaps one for the other,
  exactly as a docked workbench does. Hiding the explorer outright at that
  width leaves the toggle pressed over an empty body and the files unreachable.
- The settings sheet fills the screen; its inset and rounded frame are a
  desktop's margin, and here they cost a sixth of the width.
- The transcript never scrolls sideways; wide content scrolls inside its own
  block. The wallpaper's reading plate bleeds 64px past the column to fade in,
  and where there is no room for the bleed it is clipped, not allowed to widen
  the page.
- Heights are `dvh`: `vh` on a phone includes the browser's own bar, and the
  composer sat behind it.
- A coarse pointer gets a composer at no less than 16px. iOS zooms the page on
  focus into anything smaller, and nothing zooms it back.
- Without hover, controls revealed on hover stay visible. A phone turns the
  first tap into a hover, so a hidden control costs a blind tap to find.

## A paired device

- A paired device is told so on every screen: `.devicebar` sits above the
  chrome naming the machine it drives, the device's own number, and a way to
  leave. The page is otherwise the window's, and nothing else says the
  sessions on it live somewhere else.
- The bar is a fixed 30px so the rail, which is `position: fixed` from the
  top, can start under it; a bar that sized itself would leave the rail
  covering its text.
- On the window the phone button trades its dot for a count once a device is
  online, and a line under it says when one comes or goes. Online is an open
  event stream, not a recent request, so a phone whose page is shut reads as
  gone within one read.

## Onboarding

- The stage is the named inline-size container `onboarding`. The card folds at
  760px of that container, so interface zoom changes its columns and spacing.
- The fold follows available CSS width, not the unscaled viewport measured by
  a media query. Form controls must fit inside the shell's clipping boundary.
- `perf/onboarding.mjs` checks card bounds, the 760/761px boundary, saved zoom
  and resizing, alongside scrolling, focus clearance and completing setup.
- The connect card grows inside the opening scene rather than starting a second
  screen, so the introduction above it stays present and the palette follows the
  scene (a dark ground) rather than the app theme.
- The stage is exactly the window's height and is the scroll container, because
  the body clips.
- That height is `100dvh / var(--zoom)`, as on the body: `vh` does not scale
  with the zoom setting, so a bare `100dvh` stage would be taller than the
  zoomed window.
- The title bar is `position: sticky; top: 0` inside the stage: it holds the
  drag region and the window buttons, which must stay on screen however far
  the card is scrolled.
- A stage with only a `min-height` grows with its content and never overflows
  itself, so the connected state pushed the start button below a short window
  with nothing to scroll.
- The ambient glow is `position: fixed`: absolutely positioned inside a scroll
  container it counts as scrollable overflow, and at a wide window (38vw tall)
  it let the stage scroll far past the card.
- The shell is `flex: none`: a shrinkable shell is cut off instead of making
  the stage scroll.
- The stage's `scroll-padding-top` equals the title bar's 64px, so focus moving
  up the form scrolls the field below the sticky bar rather than under it.
- The title bar and footer are `flex: none` too; as shrinkable flex items they
  were squeezed to half height once the card overflowed.
- The shell clips with `overflow: clip`, so it is not a scroll container.
- Its labels are not uppercased: tracking pulls apart the Chinese particles in
  mixed text. The "get a key" entry sits at the label's right end with tracking
  zeroed, because the label's .06em is for Latin small labels.

## The settings sheet

- One title bar exists — `.chrome` — and it owns the window buttons and the drag
  region; this layer starts below it, so the window buttons still work while
  settings is open.
- The backdrop dims and softens but stays thin: the sheet over it is glass, and
  what shows through glass has to be light.
- A floating layer fades in rather than sliding: a full-bleed blurred backdrop
  shows its edge the moment it moves. The sheet is conditionally rendered, so it
  had an entrance and no way out — it simply stopped existing. A view transition
  gives it both ends. Only the named element takes part; without naming the root
  explicitly, every swap also cross-fades the whole window.
- The change preview uses the same pair — a dimmed, blurred backdrop and a
  raised surface starting below the title bar — for the same reason, including
  the same view transition. The diff inside it drops the top margin it carries
  for use inside a card, where it is one element among several; here it is the
  only content.
- A very faint gilt line under the header says this is the window's own thing,
  not content.
- The search entry sits beside the title. That position used to hold one
  explanation addressed to two of the three tiers; that sentence now lives on
  each settings block (`.apply`). Results are laid under the header without
  repainting settings itself: looking for something should not take away the
  page you are reading.
- What changing a setting costs is a fact, not a warning: one colour for all
  three tiers, only lighter than body text. What deserves colour is a present
  state such as "cannot be changed while a turn is running".
- A search landing flashes once: it is navigation feedback, not a changed
  setting.
- The nav column is 224px because of the Chinese names: "Tools and permissions"
  needs 138px at that width while a row leaves the name 99.6px. The extra 16px
  does not close the gap but decides how many characters of the value survive
  beside it. It is named `.navsec`, not `.navgrp`: the rail already owns that
  name for its columns of buttons.
- The nav doubles as a readout: the current value hangs at the right, so opening
  settings shows at a glance what this session runs under — which was the only
  reason the separate status bar existed.
- The value gives up its width first. Chinese names fit this column and English
  ones do not, and a row that truncates the name says nothing. The name's basis
  is its content width rather than 0: at 0 the row never "overflows", the name
  simply takes what is left and is clipped, and the value is never asked to
  yield. The value's shrink factor is 99, not 1: with both shrinkable the browser
  splits the overflow in proportion to their bases and takes a slice off the name
  too — 38px off "工具与权限" in the English build.
- A bar props the selected row up from the left. In a column of equal pills a
  fill alone gives out on one side or the other.
- `.nv` is real information, not decoration: `--ghost` at 10.5px measures 2:1 and
  cannot be read.
- The settings column hugs the nav rather than centring: with only a left rail,
  centring suspends the content as an island. 760px is a reading measure for
  prose; the extensions page is a table of five columns and is the one page
  widened, or its descriptions are clipped.
- Changing section is one navigation: what changes is the column, not each block
  arriving separately. Four blocks each flying in is four announcements of "here
  is something new" when nothing happened — you walked to another page. The
  section mark is the section's mark, not its state: it appears on every page, so
  tinting it amber says every page needs you.
- A state is said in the vocabulary of the separation it modifies: with no line
  to tint, hover lifts the fill and focus rings.
- The advanced fold hides three complete settings blocks, not a note, so its
  summary is drawn at a settings row's weight. Collapsed, `.ds` says whether
  anything inside has been changed: hiding a setting that is in force leaves the
  page disagreeing with the window.
- The page heading says where you are; block headings stay strong enough to scan
  the individual decisions. A command named in a note is set in the interface's
  own mono, because a bare `<code>` inherits the browser's default family.

## Settings rows

- One option per row, shared with the ask card as one control: two copies drift
  apart. `.prow`'s only differences are its own disabled and danger states.
- Risk is present before you click, not red after you select: with "allow
  everything" chosen, the whole segmented control takes a red border and red
  text rather than adding a hint afterwards.
- `.kv .v` breaks `anywhere` rather than `break-all`: a value that offers its own
  break points (a path carries a `<wbr>` after each separator) keeps them.
- On an external service row the name is the name, in the interface face, and the
  rest is machine fact in mono. Unreachable is the one state needing action and
  is said with a left bar, so an all-green block carries no visual weight at all.
  The name is the identity and must hold its width: squeezed to zero it wraps one
  character at a time over its neighbour, so `fold` and `meta` shrink instead.
- The service's own sentence sits where a skill row's description sits and is set
  the same way: different source, same reading. The badge beside it says "not
  connected now, this is the last record" — without it the row vouches for a
  service that is switched off. A tool row is three parts: the name (machine
  fact), its own description, and whether it will change your things.
- Actions sit at the end of the row with a gap before the status metadata:
  reading a state and acting on it are two things.
- Reload is the only action on this page with a duration — the process stops and
  restarts and the catalogue is walked again. It uses the interface's existing
  breath and nudge for "working" and "done" rather than introducing a spinner,
  and keeps the established colour semantics. A refusal has to be felt, but every
  displacement in this interface is about 2px; larger is a different language.
  `.rnote` carries what the button cannot: what it is doing, or why it did not.
- A switch has two states, so it is its own entire label — no "enabled" beside
  it. The pill is 30×17 because that is how big it should look, not how big it
  has to be to hit; the target grows underneath. On is the normal state and
  should carry no weight on the page: the fill is neutral and only the position
  says the state, with the accent left for things that actually need you.
- Remove is the one irreversible action in a row, so it does not look like the
  other buttons and appears only on hover.

## Community

- The QR code sits on a white plate in both themes: a scanner needs dark modules
  on a light field with a quiet zone, and an inverted code in the dark theme
  does not scan. The plate is the image's own quiet zone, not a card behind it.
- The image is drawn at 5 CSS pixels per module (185px for 37 modules) so the
  squares land on whole pixels; `image-rendering: pixelated` keeps them hard.

## Version list

- The list is one grid and each row a subgrid of it, so version, description,
  date and action are columns every row shares: a date lines up with the dates
  around it however long the status beside it is.
- Measuring in script would lag a frame behind every progress update.
- The action column is the only one that shrinks below its content: a long
  action ellipsizes rather than pushing the date or overflowing the card.
- Below 600px of list there is no action column. A status that stays on screen
  (`data-keep`: downloading, ready) takes a second line of its own: a progress
  figure cut to "Download…" says nothing.
- A hover-only action is transparent but still has a width; as a column it
  starved the description to nothing. Below 600px it is laid over the row's end
  and hides the description and date while shown, on hover or keyboard focus.
- Without hover such an action is always visible, so it takes the second line.
- Below 600px the version column drops its 104px floor for its content's width,
  and the description keeps at least its longest word and wraps.
- A phone's settings sheet is the screen's width; a description cut to one
  glyph and an ellipsis reads as nothing.

## Adding a source

- Paste, confirm, then the result. Three stages share one place through
  `data-stage`, and a plugin package walks the same path (source → plan →
  result), so they are one layout.
- `data-over` is set by the page's own drop layer: which element it landed on is
  decided by the DOM, not by coordinates the shell supplies — a zoomed interface
  puts those coordinates out of step with CSS pixels.
- The plan is grouped by risk: the group that will run something carries its own
  warning line and the rest are only files. The level is the kernel's judgement;
  the rule here only makes it visible at a glance. The kernel's reasons are
  written for a model reading JSON, so they sit behind one click — but they have
  to be there, or a new reason this page does not recognise disappears silently.
- What a package brings falls in two kinds: what can be called (skills, commands)
  and what runs by itself (resident processes, hooks, external services). The
  second must not read like the first.
- The scope bar says which directory this page is answering for. Several projects
  are open in one shell and the panel follows the frontmost, so the heading has
  to name the directory or switching tabs replaces the list under an unchanged
  title. Worktrees of one repository share one answer, and saying how many exist
  stops "switched branch, settings changed" from being the conclusion. Looking at
  another project puts an accent border on the whole bar, because the page does
  not represent the current session at that moment.
- An exception marker exists because a switch answers for this project by
  default — which is both the wanted default and an invisible one. A row with an
  exception says so itself and offers the way back.

## Roles and models

- The assignment band puts the main model above and four positions on a rail
  under it. No cards: cards draw the four as four equal things, while the real
  relation is "all of these follow the one above unless you pull one out". State
  lives on the rail's nodes — hollow follows, filled is assigned, a ring alone is
  borrowed. The rail drops straight from the anchor and stops on the last node,
  since it joins "this model" to "this work" and neither end should hang.
- `.grp` carries a transform entrance, which makes it a stacking context, so a
  later group covers an earlier group's overlay, and `.mgrp`'s overflow clips the
  last row's menu. Both yield only while a menu is actually open.
- Four rows' values line up on one vertical, so the role column's width belongs
  to all four rather than each row. 52px was measured against the Chinese names;
  "Subagents" is wider and pushes its value out. `subgrid` hands the column to
  `.fan` so it follows the longest role name.
- A node sits on the rail and needs an opaque ground to cover it. Filling is the
  moment the branch actually connects and is worth a transition; delegation uses
  `deleg`, which is what that colour means in the token table. Borrowed means it
  really is working but its switch is not on this row, so it gets the ring
  without the fill.
- The role menu is conditionally rendered (see `Roles.tsx`), so only the entry is
  declared; `@starting-style` gives the value it enters from. It is 288px tall
  with a scrolling list, so a row near the bottom opens past the visible area and
  `position-try-fallbacks` flips it up.
- The model library groups by endpoint, one row per model. One host reached over
  two protocols is one service with two routes, so they fold into one row with
  the protocol moved to a trailing picker; laid flat, the same model appears
  twice with nothing to tell the rows apart.
- `.msearch` wraps a field and its count and is not a field, which is why it does
  not share the menu's name.
- The selection mark shares `.prow`'s semantics, so it looks the same, only
  round; the ring contracts from full to empty so selection has a visible
  landing.
- The access method is two doors onto one account. It hangs under the connection
  row rather than in a model row: the protocol is a property of the source, and
  changing model should not change it. Its segmented control sizes to content
  rather than filling the row — the base `.seg`'s `flex: 1` stretches two Chinese
  labels of different lengths to two widths and wraps the longer one. What one
  door has that the other does not is written on the door: finding out after
  switching is too late.
- The model catalogue and callability are two things, kept side by side by the
  heading and the evidence row. A probed capability is a fact, not a switch.
- The list is the scrolling part: without a cap, a gateway's few hundred rows
  push the save button off the bottom. "Add a model" is not a row in that list —
  looking like one gets it ticked like one.
- The route menu hangs off the button's right edge so it is not clipped against
  the panel's.
- Adding a source is two fields, with the rest growing once the probe answers.
  The fields reuse the network panel's, so two forms look like one person made
  them. The extra settings an endpoint requires are the three things a probe
  cannot answer, and each carries a line saying why: getting them wrong does not
  produce an error, it produces a fold at the wrong moment or a request the relay
  rejects.
- Dozens of models is normal, so the picker caps and scrolls rather than pushing
  the button off screen.

## Permissions, hooks and shell

- Three risks, one layout: a sentence on the left, what it will actually do on
  the right. Two options side by side do not fill a row — they are one choice,
  not the page's subject.
- Only "write into the repository" touches tracked files, so only it carries a
  warning, and it is not the default.
- Automation puts finished recipes first and the event table after: most people
  need the first, and the second can block the agent, so it is behind one click
  and blocking rules carry their own warning line.
- A rule that can block the agent, misconfigured, presents as "this software is
  stuck" and nobody suspects the rule they added two weeks ago — so it is
  coloured while it is being edited. A dry run reports the consequence, not the
  exit code; the exit code is corroboration beside it.
- Permission rules read as one decision table, not three boxes. The tool name is
  said once on the group head and rows keep only what actually differs; strength
  is a property of the row, edited in place. As three lists of twenty, "this one
  is too strict" could only be answered by deleting and retyping.
- Three numbers are the shape of the boundary: how many deny, how many ask, how
  many allow. What a rule will block is shown before it is saved — the syntax can
  be learned, but not guessed. How a tool class is matched is said once on the
  group head; repeated fifteen times in the rows it is noise.
- A segmented control following a short label sizes to content: filled, it reads
  as a peer of the tier above it. A version number is an annotation, not part of
  the name — the "7" in PowerShell 7 is one size down and one tier lighter, or
  four names of different lengths scan as four different kinds of thing. The
  selected tier's path and caveat are said on one line, so it has to hold a long
  path.

## Sandbox and network panels

- The sandbox reads as two sections: where it may write, then how it runs. The
  writable rows are the expanded answer, not the literal values typed in, so a
  root left blank still has a name here.
- A writable directory fills the row with its actions pinned right. The add row
  is the same shape with an input in place of the content: one list should not
  grow two kinds of row.
- Pinning an executable is a corner case of that section and stays folded.
- A switch row is name, consequence, switch, with the name and the consequence on
  separate lines: packed into one they read as a sentence that never breaks.
- Network puts the three-way choice first and reveals the manual fields only when
  manual is chosen; most people never expand it. Diagnostics read as paragraphs,
  green passing by, and only a red line carries a suggestion — indented to line
  up with its detail, because it is that row's next step.
- Strength is a dot, not a box: one pass down the column shows how hard each rule
  is. Deleting is irreversible, so it waits until the pointer is on the row.

## Memory

- Grouping says when a fact will be recalled, not which category it was filed
  under — the second is the writer's taxonomy and means nothing to a reader. The
  dot on the left is the one pixel on this page that matters: it joins a memory
  to the behaviour just observed.
- The title is the one part of a row nobody bounds, so it wraps inside a share of
  the row and the description gives way to it; the scope, date and action keep
  their width and never break.
- Older versions are indented and one tier darker so they read as what is behind
  the current one rather than as more memories beside it.
- The write card uses the memory colour rather than a tool colour: this step
  changed not a file but how it will behave later. Undo is only cheap now, so the
  button is on the card rather than hidden in settings.

## Tables and rows

- `.lrow` is one thing and the one action to its right, with name and consequence
  on separate lines. Switch rows, action rows and read-only rows share it.
- A three-column row is name, description, ownership. The typeable string is a
  machine literal and the description is for a person, and the two faces are a
  deliberate contrast; a fixed-width name column aligns the descriptions down the
  whole table.
- A skill row has one column the service table does not: how to make it run. That
  column is what separates this table from the slash menu, so it comes before the
  capability tags and after the description.
- `.seg` is a recessed slot with the chosen cell raised. An underline filling the
  row drew a two-way choice as a navigation bar; sized to content it reads as a
  control. Chinese labels in the mono stack fall to a fallback face and stop
  lining up with the interface text beside them. The chosen cell already has a
  raised chip and a shadow saying so; a hue would say a second thing, and the
  cells below it that really are coloured by semantics (allow-everything takes
  `err`) have to outrank the default.
- Two kinds of "does not click" must not look alike: a save in flight is this
  instant, a host with no OS sandbox is permanent.

## Entrance exclusions

- The rail, rows, values and popovers are entrance decoration only. Whether a
  node is filled is the state itself and stays.
- The queue can take dozens of rows at once, and one entrance each is dozens of
  movements. Whether a row is there is state; whether it moves is not.

## Errors and runtime notices

- A swallowed failure is worse than the failure: a dropped request has to say so.
- The runtime's report about itself is not something said in the conversation, so
  it does not enter the prose; it sits against the composer until it has been
  read. It is one tier lighter than `.errbar`, which says "this request failed".
- `.errbar` floats: it rides a zero-height anchor (`.cmpalert`) pinned to the
  top edge of the composer card of the pane in front, so it never moves the
  conversation and rises with the card instead of covering its send button.
- The anchor spans the card's width because the bar's width is a share of it.
  With no pane on screen the bar takes the main area's bottom-right corner. It
  floats over content, so its fill is mixed into `--surface`, never into
  transparency.

## Deck readings

- Sub-agents and background jobs are readings about this turn, so they sit with
  the turn's other readings at the right end of the run rail rather than in a
  panel of their own above it.
- The detail is inside the same anchor as the reading, which is what lets hover
  and keyboard focus reach it. A click pins it: a panel that closes when the
  pointer leaves is one nobody can scroll.
- One gesture for every summary on this row — the context reading opens the
  same way, and a row where each control answers to a different one is a row
  you have to learn.

## Token readings

- Sent steps when a round bills; received climbs while the model writes, from
  the chunks themselves, because usage only lands when the round closes.
- The two readings share one place, so which one is on screen is said rather
  than inferred: an estimate is drawn in `--muted` and its unit carries `≈`.
  A number the next event will correct must not read as the measured one.
- Motion marks the moment a number moves and then stops: the arrow rises into
  place and the figure ticks through `--accent`, both dropped on a timer so a
  stream that stalls does not leave the row lit. Reduced motion removes both
  and leaves the numbers.

## Turn receipt

- A receipt and a notice are one runtime speaking, so a receipt with anything to
  report is one of the host's cards: same gutter, same `.hl`, and each item a
  `.find` rather than a private row vocabulary that drifts from it.
- A clean turn is the exception and stays one ghost line, because the weight a
  card carries is what it has to say. Its indent is `--call-sym` + `--call-gap`,
  the grid's own numbers, so it cannot fall out of step with the cards above.
- What the turn declared itself is not what the host found, so it carries no
  warning bar and the card no severity.

## Markdown

- The stream lands mid-sentence, so vertical rhythm is set by collapsing margins
  and the first and last child must not push the block around.
- Headings move with the body: the step between heading and body is fixed. It was
  +1.5 / +.5 / 0 / −.5 pixels — the interface's own scale already ruled that half
  a pixel is not a tier, but that guard passed over `calc(--read)`.
- A listing the assistant wrote is not a tool result and no longer renders in the
  box every tool result renders in. Its frame is what the corner is measured
  against, because the listing scrolls sideways and anything positioned inside it
  slides away with the code. `tab-size: 4`, not the terminal's 8: a listing is
  set for reading, and tab-indented Go reaches the right edge two levels in.
  Machine output keeps 8, where it is what the emitter aligned to.
- The language label sits in the top padding rather than over the first line, and
  hands the corner to the copy control while the pointer is on the block: both
  answer "what is this", one after the other. The prose's own copy control is
  permanent, because an entry point nobody can find is one nobody added; a
  pointer is not assumed, so on touch it stays printed. The block's border is not
  decoration: it is the one thing in the prose that answers a pointer, and its
  edge is how it says so beforehand.
- KaTeX inherits the surface, and a long display equation scrolls in its own box
  rather than widening the transcript. A wide table does the same: the transcript
  never scrolls sideways.
- Tables draw no vertical rules: the horizontal ones already say which cells are
  one row, and verticals cut them into a form. The first column takes no left
  padding so the table's left edge lands on the prose's. The header is set in the
  interface face: it is a sentence, not a value.
- The caret trails the last line instead of dropping to one of its own.

## Account, versions, extensions

- Signed out, the account button is an outline at the icon cluster's weight;
  signed in it carries the initial, so the bar states a fact.
- The device code is the one thing a user must copy by eye, so it gets the room a
  one-shot secret needs.
- A version label rides one beat behind the number it explains: the version is
  the answer, the words are the footnote. Rows take the settings list's shape —
  hairline separated, mono for the machine literal, prose for meaning. Where you
  are reads at full weight and what you left behind recedes; position in the list
  is carried by type, not by a badge. The action stays out of the way until the
  row is under the pointer: the list is for reading first.
- `.hl .fail` is `.tag`'s shape — a small mono pill — in the colour that means
  broken, so a failed command reads as failed without a word.
- An extension's surface reuses `.find` / `.opt` / `.btn`, so a plugin card reads
  as part of the transcript rather than as a foreign panel. A composed view is a
  tree of primitives drawn with the appearance already in use: tone says what a
  node means and the palette stays ours — an extension can say "this one failed",
  not "failure is green". Markdown it hands over is drawn as plain text: the
  transcript's renderer assumes trusted content and a view is not.
- A standing panel sits in the side rail, so it is denser than the same content
  in the transcript. The slot rail is a narrow band against the composer's top
  edge that neither scrolls nor takes the composer's height: a plugin may stand
  there and may not fill it. Its move handle is the one control the host adds per
  view and is normally invisible — the usual action is reading it, not moving it.
  A takeover keeps the host's frame, name and status and replaces only the body,
  so the attribution line has to be there.
- A palette is chosen by looking at it, so each card is that palette drawing the
  window it is about to paint. The selection dot reads over a photo the way a
  border alone does not. Name over source, each on one line: side by side both
  wrapped, and a grid of two-line labels reads as noise. Line height is stated
  rather than inherited from `normal`, which a CJK name and a Latin one resolve
  to different heights.

## The opening sequence

- Fixed dark, not following the app theme: it is a ceremony, not part of the
  interface. It plays once in a lifetime, any key skips it, and every animation
  moves only transform and opacity.
- A highlight sweeps the wordmark; the gradient is the glyph's fill, so `color`
  must be transparent. The name is written in the product's own mother tongue —
  Inter 200 is the weight every AI product reaches for, while mono is the coding
  agent's own face and is already in the bundle.
- The subtitle is a tracked, uppercased line: chrome that looks the same in any
  theme. It is a sentence, not a badge.
- The progress line says only how much of the opening is left; once the sequence
  ends it refers to nothing and would read as "still loading", so it leaves with
  the skip hint.
- Gathering shrinks the introduction upward into the card's head. It neither
  fades nor cuts — the wordmark stays on stage — and the displacement is in `vh`
  so it settles at the same proportion at any window height. A user who has seen
  the opening but still owes a key gets the gathered state directly.
- Onboarding's service chips only fill the address; they do not state what is
  supported. Model choice uses the app's own Picker, overridden here to the
  scene's palette so its button aligns with the card's fields, while the menu
  keeps the app theme — an overlay should rise out of the scene. Only the trigger
  is overridden: menu items are buttons too, and an unscoped selector paints them
  white-on-transparent, which lands as white on white in a light menu.

## Panes

- Panes are stacked, one visible at a time. Side by side they squeeze each other
  and a glance cannot tell which composer belongs to which run; as tabs the
  background one keeps running.
- Only the current pane is named, so the name is globally unique: handing it from
  the old pane to the new one lets the browser read a session switch as one
  action rather than two unrelated replacements.
- A tab shrinks by default: without `flex: none` tabs are squeezed rather than
  scrolled, and seven or eight become unreadable. They keep a floor and scroll
  horizontally (the wheel is handled in `PaneTabs`).
- Two machines' panes sit side by side and the tab decides where a command lands;
  unwritten, it has to be remembered. A background pane that is running shows it
  on its tab.
- `.panes` became a stacking container, so `flex: 1` no longer applies and the
  empty state stuck to the top: it fills like a pane and then centres.
- A tab is a control, not prose: selectable text means the right button always
  opens the system menu and never ours. The rename field stays selectable.
- The rename field edits in place — double-click a tab, or choose it from the
  context menu. Closing something still running asks first and says which runs
  stop. The confirmation floats under the tab it is about: replacing the whole
  strip reads as the interface changing face, and does not say which tab is being
  confirmed. It enters with a displacement and a fade only; the context menu
  beside it has no exit animation, and a confirmation should not be heavier.

## The workspace tree

- Three levels read as three only when each starts further in than its parent;
  machine and project both began at the rail's edge. A project is this column's
  unit of work and they had 0px between them — one block of adjacent rows.
- A project takes two rows: its name, then what it is. The name then gets the
  full width — in a 236px column, with name, tag, count and delete on one line,
  the name is what gets clipped, and it is the one thing not readable anywhere
  else. The `+` spans both rows because it belongs to the whole node. The
  actions align to the name rather than to the midpoint of two rows: the elbow,
  the dot and the name's centre are on one line, and a chip centred across two
  rows sits 8px lower.
- The open project is the node, not a session row: both rows sit on one raised
  ground. That ground used to be gold. Amber says one thing in this palette —
  a decision is waiting — and a permanently present row wearing it stops the
  waiting approval card from being the loudest thing on screen, which is its
  entire reason for existing. On the raised ground `--ghost` loses its margin,
  which was calibrated against `--page`.
- The dot was this state's only visible mark back when `data-open` changed a text
  colour and nothing else.
- With the body's 1.6 line height, a 10px annotation occupies 15.2px — half again
  its own height. Prose needs that leading because it wraps; this line does not.
- Add and delete are permanent, not hover-revealed: an invisible entry point
  reads as a missing feature. Delete is one tier quieter than add, by contrast
  rather than by visibility, and matches a machine row's add button — no fill, a
  stroked icon, a ground only on hover.
- Children form their own block so they can carry the hierarchy guide line.
  Indentation alone does not say level: 18px between two tiers scans as "slightly
  out of line".
- A selected session row cannot share hover's colour: both at `--overlay` makes
  any row the mouse passes look selected. Its hue changed too — `--net` already
  means "this session has a live runtime" (the dot is that), and reusing it for
  "you are looking at this one" merges two meanings. The blue dot means a live
  runtime, not that it is reasoning now: the heartbeat goes only to the one
  actually running, with the global run state on `.app` and the current session
  as `data-on`.
- The rename and delete controls leave the layout but stay on the row: the width
  they held belonged to the title, and a control nobody can see is the other
  failure. They reserve their width whatever they look like, so hiding them until
  hover bought nothing back. Rename and delete are a pair in one box on one grid:
  an 11px glyph beside a 15px one reads as "delete matters more than rename".
- Conflict copies fold into the conversation they came from. The count is
  permanent, unlike rename and delete: it is the only hint that something
  happened here, and hidden it says nothing.
- Both `+` controls are permanent: a new session grows inside the folder it
  belongs to, and open/new project is pinned to the column's foot. The new-session
  row is indented to the session rows, so it reads as one of them.
- With hundreds of conversations only the recent ones are listed and the rest
  fold behind one row. Two folders with the same name — worktrees, most often —
  are separated by the small tag.
- The confirmation bar replaces the whole row rather than competing for width.
  Both actions are full buttons: cancel on the left, the one that actually
  changes something on the right, with deletion taking a further red tier.
- A project's delete is one tier down from "new session" 6px away: the cost
  differs by a whole project, so the two must not carry the same weight. Quieter
  is not hidden — it holds its width, and hiding it leaves a gap that reads as
  "there is no such feature".

## Remote machines

- A host key question is modal because the link is stuck on it: nothing happens
  on the far machine until someone answers. The fingerprint is deliberately not
  easy to click past — that is its entire purpose. The folder picker sits under
  the ask: browsing an unconnected machine is what raises the host-key question.
  The veil is `--page`, not `--bg`: there has never been a `--bg`, so the whole
  declaration was invalid and the card floated over an untinted window.
- A fingerprint wraps rather than truncating: nobody can check one against
  anything with its middle eaten by an ellipsis.
- The folder picker runs over the file protocol, so it opens on a machine. The
  path field is the breadcrumb and the way in by hand at once: a folder too deep
  to walk to is typed. While the far side answers the list dims rather than
  emptying — clearing the rows takes away the only account of where the reader is.
- A probe's result hangs under the machine it asked, separated by a vertical
  rule: it is not part of the row, it is the answer to a question. The rule's
  colour is the verdict, so no badge is needed. Each disabled route gets its own
  paragraph: they are sentences to read, not readings, and packed into one line
  nobody reads them. Status takes the middle and pushes both actions right: a
  status is a sentence, not a third button.
- A machine that already connects has its address next door; copying beats
  retyping. The project list is the one field here that is a list, so it takes
  the whole width instead of a 13ch label column.
- Repeated notices fold into one row with a count rather than stacking three
  identical ones.
- Machines separate by space, not by a line: a line here says "local above,
  remote below", and in this list they are one kind of thing. 8px in a 264px
  column does not read as a boundary, only as "these two rows are slightly
  further apart" — between groups has to be clearly larger than within, which is
  the same rule the right column follows. The local group and the first remote
  take the same number.
- A machine row is the same node shape: name on one line, which machine it is on
  the next. With the target squeezed beside the name, a length like
  `ada@build.internal` clipped the hostname. Weight and colour used to point
  opposite ways — the machine name was the column's heaviest (700) in `--muted`
  while a project name was lighter in `--text`. People look for projects and
  sessions here, not machines, so the weight came down rather than the colour
  going up.
- A host has six states, not two: between connected and not there are
  connecting, reconnecting, and connected-with-a-forward-that-did-not-attach —
  the last of which looks most like normal. This machine is not connected, it is
  here: a filled pip is a connection state and a ring says where something is.
- First connection installs a binary on the far side and takes tens of seconds. A
  spinner cannot separate "downloading" from "stuck", so the kernel reports step
  by step and this shows it step by step.
- Once connected, that machine reports its own folders and conversations,
  indented one level deeper than local: they belong to the host above, not to
  another local project. `.wsrow` already brings the node's whole layout; the
  rule here only moves it under its host. Same ceiling and same exit as the local
  column.
- A workspace row is itself the connect button: on a machine with no pane open
  there is nothing else to press, and splitting it in two makes someone say
  "connect" and then "open" for one action. `direction: rtl` cannot be used to
  move the ellipsis to the left — it also reverses the path's segments, so
  `/srv/training` displays as `srv/training/`. Clipping from the right is
  preferred.

## Appearance controls

- A line drawn in the chosen face is the only way to tell whether that name
  exists on this machine.
- The slider takes the accent because it sets an amount, not a switch.
  `accent-color` only tints the system control, and the slider is otherwise the
  one control in this interface nobody designed: the track and the thumb are
  drawn here.
- The wallpaper preview is the window's shape, not a strip: `cover` only leaves a
  focal point room on the axis it overflows.
- The settings sheet lets the wallpaper through as well.
  - It is a full-screen overlay and the picture is on `body` (z-index −2), so it
    shows the moment this layer stops being solid.
  - Settings is dense, so it lets less through than the workbench: reading
    settings, the picture is background, not content.
  - Translucency alone shows the original picture — a high-contrast photo
    pressed against dense prose — so it is blurred: the depth stays and what is
    under the text is a wash.
  - A raised panel over a picture is one set of numbers: `.compose` and
    `.runhead` are also 80% with `blur(16)`.
  - The columns and headers inside it do not frost again — glass over glass and
    the picture is gone.

## Context gauge

- The context breakdown keeps only a segmented bar on show: in a column this
  narrow, five rows of numbers cost more than they are worth to most readers.
  Hovering unfolds it. Its legend is permanent, though: the block exists for the
  composition, and hiding that behind hover leaves one bar.
- The line that stands in for the bar when there is no denominator keeps the
  bar's height, so the column does not jump when the state changes.
- The denominator is editable, so its dotted underline is always there: an entry
  revealed on hover reads as "there is nothing here".
- The column read as a list because it was typeset as one — total, fold point,
  capacity and five category names all at 13px, with nothing saying where to look
  first. Hierarchy is not made by shrinking five things, since Han glyphs start
  to blur at 11px, but by letting the one that matters leave the group. Each
  ceiling carries its own share, set apart from the figure it is a share of: read
  together they would be one number.
- The window size is the one thing that can fill a missing ceiling, so its field
  is permanent rather than behind hover or a fold. The maintenance point is
  editable where it is read: filed into a settings table, only someone who
  already knows it exists would find it, and this footnote is written for the
  people who do not. Its fine print takes its own line and one size down, so it
  reads as a condition rather than as the explanation.
- Segments are separated by 2px of ground rather than 1px: that gap is the mark
  saying "these are two segments", and at 1px it is nearly invisible in dark.
- Two tiers lead to the maintenance point: the kernel narrows at 75% and lands
  results at 92%, and the bar draws the same thing. Compaction is routine
  maintenance rather than a fault, so it takes `accent` — coloured as a warning,
  an ordinary fold looks like a problem.
- Model capacity is a secondary diagnostic: it answers "is this model simply too
  small", not a countdown, so it is half the height and one tier grey — a
  footnote, not a second progress bar. The maintenance point is notched onto the
  span of window it occupies: 160k against 1M is a division nobody performs while
  reading a panel. The notch overhangs the bar, or it is unrecognisable in a 3px
  track, and it is square and in the base ink — `accent` is one step from the
  "model reply" category colour.
- In the adjustable state that same notch grows into a handle. Putting a separate
  control beside it makes the reader match "the line I am reading" to "the thing
  I drag", and they were one thing. No transition while dragging: the position
  has to follow the pointer, and easing leaves the handle behind it.
- A native `range` supplies the keyboard, the touch target and the spoken value;
  what is seen is the bar already being read, so what is grabbed is what was just
  read. Its own track and thumb are fully transparent.
- Attribution appears only when the maintenance point is not self-evident, so it
  is the footnote's footnote — one tier below `.ctxnote`.
- The metrics column scrolls, and a scroller clips, so the popover is fixed to
  the viewport with its position measured by `Context` and written inline.

## Cost and storage

- Cost reports the total first and then its composition. In a column of small
  rows the total and its parts looked the same, and a glance could not find the
  total. The leading block's figure is a size larger — it is the one that will be
  seen without being looked for. That row was once called `.mrow`, sharing a name
  with the connection panel's clickable model rows: two components under one name
  means the later rule only overrides the properties it lists.
- A second currency is one tier shorter: it is the same money said another way,
  not another expense. The inline figures follow the draft's row — weak name,
  strong value, laid across rather than stacked into a table.
- Storage is three sections on one screen: how much, where, and what is moving.
  Bars normalise to the largest item, so what is read is the relation rather than
  absolute pixels, and every figure is tabular because they are there to be
  compared. A pinned or immovable item says why it has no button.
- Usage is a single-series chart, so colour does not re-encode what length
  already says and every bar shares one. `.ufill` is `display: block` — an `<i>`
  is inline, and an inline box ignores width and height.

## Presses and choreography

- A press is the same physical event everywhere, so what answers it is what the
  markup says is a control. A full-width row squeezed by a percentage moves its
  far edge several pixels while a 24px button moves a quarter of one, so rows
  take a displacement and buttons take a scale.
- A file changed since the checkpoint is the case that needs a decision, so it
  reads as one rather than as another confirmation.
- Choreography has one mechanism: the engine counts who is nth and the call site
  says how far apart with `--m-step`. A hand-written `nth-child` chain is not
  ugly, it is a rule that has to be revisited every time the list changes length.
- The reduced-motion block is deliberately the last motion rule in the file, so a
  new component inherits the user's request instead of depending on a selector
  written before it.

## MCP authorization state

- A server awaiting project launch approval uses a static `--warn-ink` pip.
  It is an authorization decision, so it does not use the animated connecting
  indicator or the failed-connection color.

## MCP rows in narrow settings groups

- Below 560px of available row width, server identity, status metadata and
  actions use separate lines. Status and provenance wrap instead of disappearing
  behind an ellipsis; long unbroken source strings may break within the line.
- The action group keeps its switches and connection buttons inside the row,
  with room to wrap when needed. The container boundary scopes this to MCP rows,
  including rows inside packages; wide server rows retain their inline layout.

## Control tooltips

- `.studio-control-tip` is a child of the button it describes, so it inherits
  that button's `white-space` and sits inside its opacity context. It therefore
  declares `white-space: normal` and wraps inside `max-width`; any string,
  Chinese or English, stays inside the box.
- A disabled host dims its glyph (`> .studio-icon`), never itself: opacity
  multiplies down the tree, so a dimmed button would fade the tooltip that
  explains why it is disabled.
- `perf/tooltip.mjs` measures both in Chromium; `controltip.test.ts` pins the
  rules statically.

## The feedback veil

- `.fbk-veil` is a flat scrim, not a blurred backdrop.
- A backdrop filter over the whole window is redone every frame while anything
  beneath it moves; a running turn's spinners are enough.
- The scrim is opaque enough that what shows through reads as dimmed, which is
  the job the blur did.
- `feedback-veil.test.ts` scans every stylesheet: no rule on `.fbk-veil` may
  carry a backdrop filter or an endless animation.
