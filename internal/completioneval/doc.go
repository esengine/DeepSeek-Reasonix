// Package completioneval implements the shared completion validator: an
// independent, tool-less, history-less bounded reviewer the host consults on
// candidate terminal turns. It never scans for keywords in the model's text;
// it judges structured evidence against a fixed policy and answers with one of
// five outcomes. Model, policy, and usage are isolated from the main
// conversation: no tools, no session history, no compaction, and usage
// attributed to the completion-evaluator source so the main prompt cache is
// never polluted.
package completioneval
