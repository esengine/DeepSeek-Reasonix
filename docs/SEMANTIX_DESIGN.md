# Semantix Design theme

Semantix Design is the shared brand palette for Reasonix terminal and desktop
surfaces. It uses a charcoal base and one semantic green accent. Status colors
remain distinct so cache reuse can be understood without relying on labels
alone.

## Canonical dark tokens

| Role | Value | Usage |
| --- | --- | --- |
| Accent | `#2F967F` | Focus, selection, active controls |
| Hit | `#56B88E` | Strong cache reuse and successful states |
| Information | `#57AE9A` | Moderate reuse and neutral information |
| Neutral | `#78968B` | De-emphasized labels and inactive state |
| Miss | `#D5A657` | Low reuse and warning state |
| Error | `#E0696A` | Failure and destructive feedback |
| Background | `#0B1210` | Desktop base surface |
| Elevated surface | `#16231E` | Desktop elevated content |
| Border | `#29463B` | Separators and quiet outlines |
| Foreground | `#EAF4F0` | Primary text |

The TUI selects these values with `reasonix theme semantix` or
`REASONIX_THEME=semantix`. The desktop pack is `official-semantix`. Light mode
uses darker green text and control values where the canonical accent would not
meet contrast requirements on pale surfaces.

The desktop manifest is the source of truth for surface colors. TUI terminals
do not own their background, so the TUI theme applies the shared interactive
and status tokens only.
