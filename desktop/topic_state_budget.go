package main

import "context"

// The transaction and its legacy mirror share this budget, after cold-open
// and reconciliation complete. Tests inject a context to verify that boundary.
func newTopicOperationContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), topicStateOperationTimeout)
}
