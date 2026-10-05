package responses

var sendChunkEnterBlocking func()

func notifySendChunkEnterBlocking() {
	if sendChunkEnterBlocking != nil {
		sendChunkEnterBlocking()
	}
}
