package main

import "time"

func waitForLaterTopicTimestamp(createdAt int64) {
	for time.Now().UnixMilli() <= createdAt {
		time.Sleep(time.Millisecond)
	}
}
