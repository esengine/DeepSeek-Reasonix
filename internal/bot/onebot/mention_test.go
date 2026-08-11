package onebot

import "testing"

func TestOneBotGroupMentionGate(t *testing.T) {
	if oneBotMessageMentions("hello", "123") {
		t.Fatal("plain group message treated as mention")
	}
	if !oneBotMessageMentions("[CQ:at,qq=123] hello", "123") {
		t.Fatal("matching CQ mention not detected")
	}
	if oneBotMessageMentions("[CQ:at,qq=456] hello", "123") {
		t.Fatal("mention for another user accepted")
	}
}
