# Personality Module — Template Files

These example files show how to structure `IDENTITY.md`, `SOUL.md`, and `USER.md`
for the Reasonix personality module.

## How it works

1. Create files in your project's `.reasonix/personality/` directory:
   - `IDENTITY.md` — defines who the agent IS (identity, beliefs, values)
   - `SOUL.md` — defines HOW the agent behaves (tone, quirks, communication style)
   - `USER.md` — defines WHO the user is (role, preferences, context)

2. Enable the personality module in Settings > Personality or in `reasonix.toml`:
   ```toml
   [personality]
   enabled = true
   ```

3. The files are injected into the system prompt under `=== IDENTITY ===`,
   `=== SOUL ===`, and `=== USER ===` sections.

## Order of precedence

Project-level files (in `.reasonix/personality/`) override user-level files
(in `~/.config/reasonix/personality/`). Files are independent — you can have
only `SOUL.md` without `IDENTITY.md` or `USER.md`.
