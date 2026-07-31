
## Describe your changes

...

## Checklist before requesting a review

- [ ] 👓 I have performed a self-review of my code
- [ ] 👶 This helper does not already exist
- [ ] 🧪 This helper is tested
- [ ] 🏎️ My code limits memory allocation and is fast
- [ ] 🧞‍♂️ This helper is immutable and my tests prove it
- [ ] ✍️ I implemented the parallel, iterator and mutable variants
- [ ] 🔬 An example has been added to lo_example_test.go
- [ ] ⛹️ An example has been created on https://go.dev/play and added in comments
- [ ] 📖 My helper has been added to documentation
  - [ ] in README.md
  - [ ] in docs/data/*.md
  - [ ] in docs/static/llms.txt

## Conventions

- Returning `(ok bool)` is often better than `(err error)`
- `panic(...)` must be limited
- Helpers should receive variadic arguments when relevent
- Add variants of your helper when relevant:
  - Variable number of arguments: `lo.Must0`, `lo.Must1`, `lo.Must2`, ...
  - Predicate with index: `lo.SliceToMap` vs `lo.SliceToMapI` 
  - With lazy callback: `lo.Ternary` vs `lo.TernaryF`
  - ...
