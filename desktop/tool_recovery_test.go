package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/provider"
)

func TestRemoteToolRecoveryKeepsIdentityAndNeverReplaysUnknownPost(t *testing.T) {
	posts := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/tool-recovery" || req.Header.Get(expectedSessionPathHeader) != runtimeRemoteTestPath {
			t.Fatalf("unfenced recovery request: %s %v", req.URL, req.Header)
		}
		if req.Method == http.MethodGet {
			return remoteRuntimeTestResponse(req, 200, remoteRuntimeTestJSON(t, control.ToolRecoverySnapshot{SessionPath: runtimeRemoteTestPath, RuntimeEpoch: "epoch", Revision: "revision", Calls: []provider.ToolCallRecord{}})), nil
		}
		posts++
		var body control.ToolRecoveryRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.AttemptID != "attempt" || body.InspectionID != "inspection" || body.RuntimeEpoch != "epoch" || body.Revision != "revision" {
			t.Fatalf("identity changed: %+v", body)
		}
		return nil, errors.New("response lost after commit")
	})}
	a, tab := remoteRuntimeTestApp(client)
	v, err := a.GetToolRecoveryForTab(tab.id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.ResolveToolRecoveryForTab(tab.id, control.ToolRecoveryRequest{SessionPath: v.SessionPath, RuntimeEpoch: v.RuntimeEpoch, Revision: v.Revision, AttemptID: "attempt", InspectionID: "inspection", Action: "confirm"})
	if err == nil || posts != 1 {
		t.Fatalf("unknown post replayed: posts=%d err=%v", posts, err)
	}
}

func TestRemoteToolRecoveryRejectsOtherSessionSnapshot(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return remoteRuntimeTestResponse(req, 200, `{"sessionPath":"/different","runtimeEpoch":"epoch","revision":"revision","calls":[]}`), nil
	})}
	a, tab := remoteRuntimeTestApp(client)
	if _, err := a.GetToolRecoveryForTab(tab.id); err == nil {
		t.Fatal("another session's snapshot was accepted")
	}
}
