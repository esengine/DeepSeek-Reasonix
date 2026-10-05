package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/tool"
)

// fakeElicitor answers each form from a script and keeps what it was shown.
type fakeElicitor struct {
	mu      sync.Mutex
	replies []tool.ElicitReply
	seen    []tool.ElicitRequest
}

func (f *fakeElicitor) Elicit(_ context.Context, req tool.ElicitRequest) (tool.ElicitReply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, req)
	if len(f.replies) == 0 {
		return tool.ElicitReply{Declined: true}, nil
	}
	r := f.replies[0]
	f.replies = f.replies[1:]
	return r, nil
}

const deploySchema = `{"message":"Deploy settings","requestedSchema":{"type":"object","required":["email","count"],"properties":{
	"email":{"type":"string","format":"email","title":"Email"},
	"count":{"type":"integer","minimum":1,"maximum":5},
	"dry":{"type":"boolean","title":"Dry run"},
	"region":{"type":"string","enum":["us","eu"],"enumNames":["United States","Europe"]},
	"tier":{"type":"string","oneOf":[{"const":"s","title":"Small"},{"const":"l","title":"Large"}]},
	"tags":{"type":"array","items":{"anyOf":[{"const":"a","title":"Alpha"},{"const":"b","title":"Beta"}]}}}}}`

// The form keeps the server's field order and its labels, and what comes back
// is typed the way the schema asks: an integer, a boolean, the enum's value
// behind its title, a list for a multi-select.
func TestElicitationFormRoundTrip(t *testing.T) {
	f := &fakeElicitor{replies: []tool.ElicitReply{{Values: map[string][]string{
		"email": {"ada@example.com"}, "count": {"3"}, "dry": {boolYes}, "region": {"Europe"}, "tier": {"Large"}, "tags": {"Alpha", "Beta"},
	}}}}
	got, err := elicit(t.Context(), f, "deployer", json.RawMessage(deploySchema))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"action":"accept","content":{"count":3,"dry":true,"email":"ada@example.com","region":"eu","tags":["a","b"],"tier":"l"}}`
	if b, _ := json.Marshal(got); string(b) != want {
		t.Fatalf("result = %s\nwant     %s", b, want)
	}
	req := f.seen[0]
	var order []string
	for _, field := range req.Fields {
		order = append(order, field.Name)
	}
	if req.Source != "deployer" || req.Message != "Deploy settings" || strings.Join(order, ",") != "email,count,dry,region,tier,tags" {
		t.Fatalf("form = %+v", req)
	}
	if region := req.Fields[3]; strings.Join(region.Choices, ",") != "United States,Europe" || region.Multi {
		t.Fatalf("region = %+v", region)
	}
}

// An answer the schema refuses goes back to the person with the reason, not to
// the server; a person who gives up is a refusal, and so is having nobody to ask.
func TestElicitationRefusesBadAnswersAndDeclines(t *testing.T) {
	f := &fakeElicitor{replies: []tool.ElicitReply{
		{Values: map[string][]string{"email": {"not-an-email"}, "count": {"9"}}},
		{Values: map[string][]string{"email": {"a@b.co"}, "count": {"2"}}},
	}}
	got, err := elicit(t.Context(), f, "s", json.RawMessage(deploySchema))
	if err != nil || got["action"] != "accept" {
		t.Fatalf("result = %v, %v", got, err)
	}
	if len(f.seen) != 2 || !strings.Contains(f.seen[1].Note, "Email") || !strings.Contains(f.seen[1].Note, `"count": out of range`) {
		t.Fatalf("second form's note = %q", f.seen[len(f.seen)-1].Note)
	}
	if got, _ := elicit(t.Context(), &fakeElicitor{}, "s", json.RawMessage(deploySchema)); got["action"] != "decline" {
		t.Fatalf("a refusal = %v, want decline", got)
	}
	if got, _ := elicit(t.Context(), nil, "s", json.RawMessage(deploySchema)); got["action"] != "decline" {
		t.Fatalf("nobody to ask = %v, want decline", got)
	}
	missing := &fakeElicitor{replies: []tool.ElicitReply{{Values: map[string][]string{"count": {"1"}}}, {Values: map[string][]string{"count": {"1"}}}, {Values: map[string][]string{"count": {"1"}}}}}
	if got, _ := elicit(t.Context(), missing, "s", json.RawMessage(deploySchema)); got["action"] != "cancel" || len(missing.seen) != elicitAttempts {
		t.Fatalf("never valid = %v after %d forms, want cancel after %d", got, len(missing.seen), elicitAttempts)
	}
	for _, bad := range []string{
		`{"mode":"unsupported","message":"go"}`,
		`{"message":"x","requestedSchema":{"type":"object","properties":{"o":{"type":"object"}}}}`,
		`{"message":"x","requestedSchema":{"type":"object","properties":{"a":{"type":"string"},"a":{"type":"number"}}}}`,
		`{"message":"x","requestedSchema":{"type":"object","properties":{"e":{"type":"string","enum":["x","y"],"enumNames":["Same","Same"]}}}}`,
		`{"message":"` + strings.Repeat("m", elicitMaxMessage+1) + `","requestedSchema":{"type":"object","properties":{}}}`,
	} {
		if _, err := elicit(t.Context(), f, "s", json.RawMessage(bad)); !errors.Is(err, errBadElicitation) {
			t.Fatalf("%s: err = %v, want errBadElicitation", bad, err)
		}
	}
}

// Over stdio the server asks on a pipe no call owns; the form reaches the
// person behind the call in flight, and without one the server is declined.
func TestStdioElicitationReachesThePersonBehindTheCall(t *testing.T) {
	for _, withPerson := range []bool{true, false} {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		env := map[string]string{"GO_WANT_HELPER_PROCESS": "1", "GO_WANT_HELPER_ELICIT": "1"}
		host, tools, err := StartAll(ctx, []Spec{{Name: "mock", Command: os.Args[0], Args: []string{"-test.run=TestHelperProcess", "--"}, Env: env}})
		if err != nil {
			cancel()
			t.Fatalf("StartAll: %v", err)
		}
		callCtx := ctx
		f := &fakeElicitor{replies: []tool.ElicitReply{{Values: map[string][]string{"name": {"Ada"}, "age": {"36"}}}}}
		if withPerson {
			callCtx = tool.WithElicitor(ctx, f)
		}
		out, err := findToolByName(tools, "mcp__mock__echo").Execute(callCtx, json.RawMessage(`{"msg":"hi"}`))
		host.Close()
		cancel()
		want := `elicited: {"action":"decline"}`
		if withPerson {
			want = `elicited: {"action":"accept","content":{"age":36,"name":"Ada"}}`
		}
		if err != nil || out != want {
			t.Fatalf("person=%v: Execute = %q, %v; want %q", withPerson, out, err, want)
		}
		if withPerson && (len(f.seen) != 1 || f.seen[0].Source != "mock" || f.seen[0].Message != "Who is deploying?") {
			t.Fatalf("form shown = %+v", f.seen)
		}
	}
}

// A server that asks again while a form is open is refused rather than stacking
// forms, and a form belongs to its call: the call ending takes it down.
func TestElicitRouterOneFormPerConnectionBoundToItsCall(t *testing.T) {
	var r elicitRouter
	person := &fakeElicitor{}
	unregister := r.register(tool.WithElicitor(t.Context(), person))
	ctx, e, release := r.claim()
	if e == nil {
		t.Fatal("the call in flight was not offered the form")
	}
	if _, second, done := r.claim(); second != nil {
		t.Fatal("a second form was opened while one was on screen")
	} else {
		done()
	}
	release()
	unregister()
	if ctx.Err() == nil {
		t.Fatal("a form outlived the call it belongs to")
	}
	if _, e, done := r.claim(); e != nil {
		t.Fatal("a form was offered with no call in flight")
	} else {
		done()
	}
}

// A number past what a float64 holds exactly is refused, never sent as another.
func TestElicitationRefusesAnIntegerItCannotCarry(t *testing.T) {
	schema := json.RawMessage(`{"message":"n","requestedSchema":{"type":"object","properties":{"n":{"type":"integer","minimum":0}}}}`)
	for _, in := range []string{"1e300", "9223372036854775808", "9007199254740993"} {
		f := &fakeElicitor{replies: []tool.ElicitReply{{Values: map[string][]string{"n": {in}}}, {Values: map[string][]string{"n": {"7"}}}}}
		got, err := elicit(t.Context(), f, "s", schema)
		if err != nil || len(f.seen) != 2 {
			t.Fatalf("%s: accepted on the first form: %v %v", in, got, err)
		}
		if b, _ := json.Marshal(got); string(b) != `{"action":"accept","content":{"n":7}}` {
			t.Fatalf("%s: %s", in, b)
		}
	}
}

// A form sent back keeps what was given, and a first form starts from the
// server's defaults, each as the person would have given it.
func TestElicitationPrefillsDefaultsAndTheLastAnswer(t *testing.T) {
	schema := json.RawMessage(`{"message":"m","requestedSchema":{"type":"object","required":["n"],"properties":{
		"n":{"type":"integer","minimum":1,"default":2},
		"env":{"type":"string","oneOf":[{"const":"s","title":"Staging"},{"const":"p","title":"Production"}],"default":"p"},
		"dry":{"type":"boolean","default":true},
		"who":{"type":"string","default":"ops"}}}}`)
	f := &fakeElicitor{replies: []tool.ElicitReply{
		{Values: map[string][]string{"n": {"0"}, "env": {"Staging"}}},
		{Values: map[string][]string{"n": {"4"}}},
	}}
	if _, err := elicit(t.Context(), f, "s", schema); err != nil {
		t.Fatal(err)
	}
	first := map[string]string{}
	for _, field := range f.seen[0].Fields {
		first[field.Name] = strings.Join(field.Default, ",")
	}
	if first["n"] != "2" || first["env"] != "Production" || first["dry"] != boolYes || first["who"] != "ops" {
		t.Fatalf("first form defaults = %v", first)
	}
	second := map[string]string{}
	for _, field := range f.seen[1].Fields {
		second[field.Name] = strings.Join(field.Default, ",")
	}
	if second["n"] != "0" || second["env"] != "Staging" || second["dry"] != boolYes {
		t.Fatalf("the form sent back = %v, want the last answer kept", second)
	}
}
