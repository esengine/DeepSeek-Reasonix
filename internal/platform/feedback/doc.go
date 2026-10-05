// Package feedback sends a person's report to the maintainers and follows what
// becomes of it. One service sits behind every frontend: it validates and
// redacts locally, submits with an idempotency key so a retry never files
// twice, keeps the install identity and receipts on disk, and reads the
// caller's own reports back with the thread under each. A reply from the
// reporter is sent once and never retried; which maintainer replies have been
// shown is kept per report, so a badge can say what is new. Thread text is
// cleaned of escape sequences before any frontend sees it. Nothing here
// reaches a model request.
package feedback
