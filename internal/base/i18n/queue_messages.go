package i18n

// QueueMessages is the text the terminal's queue listing adds to a line; Messages embeds it.
type QueueMessages struct {
	QueueImageWait       string // a queued line with a picture waits for the running turn to end
	TUIQueueUnrecovered  string // the /queue line for an inbox whose contents could not be read
	QueueRowsMoreFmt     string // rows below the queue list when more items wait than are drawn
	QueueRowsPaused      string // the queue is paused
	QueueRowsReadonly    string // the queue is read-only: a newer schema owns it
	QueueReadonlyNote    string // a queue key refused because a newer schema is read-only
	QueueHostTag         string // the marker on a runtime-authored line that needs a decision
	QueueTagFmt          string // appended to the working line while messages are queued
	QueueDeleteAgainFmt  string // first d on a selected queued message
	QueueDeletedFmt      string // a queued message was deleted
	QueueRetriedFmt      string // a blocked queued message was set to run again
	QueueSavedFmt        string // an edit to a queued message was saved
	QueuePausedNote      string // the queue was paused by the key
	QueueResumedNote     string // the queue was resumed by the key
	QueueActionFailedFmt string // a queue key could not be applied
	QueueGoneFmt         string // a selected message left the queue (it ran or was removed)
	// QueueBlockWhy mirrors BLOCK_WHY in desktop/frontend-next/src/i18n/queue_why.ts:
	// a block code -> why that entry stopped.
	QueueBlockWhy     map[string]string
	QueueRetryHeldFmt string // retry key on an entry that may have run — %s short id, %s why
	QueueRetryNoWhy   string // a held entry arrived with neither a code nor a reason
}
