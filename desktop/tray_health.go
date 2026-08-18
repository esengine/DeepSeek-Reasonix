package main

import "strings"

type statusNotifierSnapshot struct {
	WatcherOwner string
	Host         bool
	ItemOwner    string
	Items        []string
}

func evaluateStatusNotifierSnapshot(snapshot statusNotifierSnapshot, itemName string) (bool, string) {
	if strings.TrimSpace(snapshot.WatcherOwner) == "" {
		return false, "no_watcher"
	}
	if !snapshot.Host {
		return false, "no_host"
	}
	if strings.TrimSpace(snapshot.ItemOwner) == "" {
		return false, "item_no_owner"
	}
	for _, registered := range snapshot.Items {
		service := strings.TrimSpace(strings.SplitN(registered, "/", 2)[0])
		if service == itemName || service == snapshot.ItemOwner {
			return true, ""
		}
	}
	return false, "item_not_registered"
}
