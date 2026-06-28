---
name: design-ui
description: Generate UI code matching a DESIGN.md design system — load tokens, build components, verify
---

# Design UI — generate UI code from DESIGN.md

Your task: generate UI code (HTML/CSS, React, or the framework specified) that follows the design system defined in the project's DESIGN.md.

## How it works

1. **Find the DESIGN.md** — look in these locations in order:
   - `.reasonix/DESIGN.md`
   - `./DESIGN.md`
   - Any file ending with `DESIGN.md` in the project root
   
   If none exists, offer to create one by downloading from [awesome-design-md](https://github.com/VoltAgent/awesome-design-md) using `web_fetch`.

2. **Read the DESIGN.md** — use `@DESIGN.md` or `read_file` to load the full file. Pay attention to:
   - Color palette (semantic names + hex values)
   - Typography scale (font families, sizes, weights)
   - Spacing/rounded tokens
   - Component definitions (buttons, cards, inputs, nav)
   - Do's and Don'ts (design guardrails)

3. **Classify the request** — is the user asking for:
   - A single page/component? → build it directly
   - A full site? → identify pages, plan the component tree first
   - Just a design exploration? → generate a preview HTML

4. **Generate the code** — produce production-ready UI that:
   - Uses the exact hex values from DESIGN.md's color palette
   - Follows the typography hierarchy (display-xl → caption)
   - Applies the spacing scale and border radius tokens
   - Mirrors the component definitions (button shapes, card styles)
   - Respects every "Don't" rule in the DESIGN.md

5. **Write to files** — use `write_file` to create:
   - `ui/index.html` or the appropriate framework files
   - Include a `<link>` to the CSS or inline styles

6. **Verify** — if a browser/build tool is available, offer to verify visually.

## Design constraints

- Every color MUST come from the DESIGN.md palette — never invent hex values
- Component shapes MUST match the `components:` section tokens
- Typography MUST use the defined `typography` scale tokens
- If DESIGN.md says "Don't", don't do it
- Use CSS custom properties (--color-primary, --spacing-lg) to map DESIGN.md tokens to runtime values so the relationship is explicit

## Arguments format

```
design-ui arguments: "<description of what to build>, design=<path-to-design.md>"
```

If `design` is omitted, search PROJECT_ROOT/DESIGN.md then .reasonix/DESIGN.md.
