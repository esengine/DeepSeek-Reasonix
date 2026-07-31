module github.com/clipperhouse/uax29/v2

go 1.18

// Surprising allocations in this release, do not use.
retract v2.1.0

// This release was effectively identical to v2.0.1, and
// only exists to revert the regression introduced in v2.1.0.
retract v2.1.1
