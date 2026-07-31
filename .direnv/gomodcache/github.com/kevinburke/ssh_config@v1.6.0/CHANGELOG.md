# Changes

## Version 1.6 (released February 16, 2026)

- Support `~` as the user's home directory in `Include` directives, matching
the behavior described in ssh_config(5). Thanks to Neil Williams for the report
(#31).

- Strip surrounding double quotes from parsed values. OpenSSH allows values
like `IdentityFile "/path/to/file"`, but Get/GetAll previously returned the
quotes as literal characters. Quotes are now stripped from the returned value
while preserving the original text for faithful roundtripping via String() and
MarshalText(). Thanks to Furkan Türkal for the report (#61).

- Default to a space before `#` in end-of-line comments. When a Host or KV is
created programmatically with an EOLComment, the output previously had no space
before the `#` (e.g. `Host foo#comment`). A single space is now inserted by
default. Thanks to Yonghui Cheng for the report (#50).

## Version 1.5 (released February 14, 2026)

- Implement Match support. Most of the Match spec is implemented, including
`Match host`, `Match originalhost`, `Match user`, `Match localuser`, and `Match
all`. `Match exec` is not yet implemented.

- Add SECURITY.md

- Add Dependabot configuration

## Version 1.4 (released August 19, 2025)

- Remove .gitattributes file (which was used to test different line endings, and
caused issues in some build environments). Store tests/dos-lines as CRLF in git
directly instead.

## Version 1.3 (released February 20, 2025)

- Add go.mod file (although this project has no dependencies).

- config: add UserSettings.ConfigFinder

- Various updates to CI and build environment

## Version 1.2 (released March 31, 2022)

- config: add DecodeBytes to directly read a byte array.

- Strip trailing whitespace from Host declarations and key/value pairs.
Previously, if a Host declaration or a value had trailing whitespace, that
whitespace would have been included as part of the value. This led to unexpected
consequences. For example:

```
Host example       # A comment
    HostName example.com      # Another comment
```

Prior to version 1.2, the value for Host would have been "example " and the
value for HostName would have been "example.com      ". Both of these are
unintuitive.

Instead, we strip the trailing whitespace in the configuration, which leads to
more intuitive behavior.

- Add fuzz tests.
