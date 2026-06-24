package evidence

import "testing"

func TestHasSuccessfulTodoWriteNilLedger(t *testing.T) {
	var l *Ledger
	if l.HasSuccessfulTodoWrite() {
		t.Fatal("nil ledger must report no todo_write")
	}
}

func TestHasSuccessfulTodoWriteMatchesOnlySuccessfulTodoWrite(t *testing.T) {
	cases := []struct {
		name    string
		receipt Receipt
		want    bool
	}{
		{"successful todo_write", Receipt{ToolName: "todo_write", Success: true}, true},
		{"failed todo_write does not count", Receipt{ToolName: "todo_write", Success: false}, false},
		{"successful non-todo tool does not count", Receipt{ToolName: "write_file", Success: true, Write: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := NewLedger()
			l.Record(tc.receipt)
			if got := l.HasSuccessfulTodoWrite(); got != tc.want {
				t.Fatalf("HasSuccessfulTodoWrite() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHasSuccessfulTodoProgressReceipt(t *testing.T) {
	cases := []struct {
		name     string
		receipts []Receipt
		want     bool
	}{
		{"nil", nil, false},
		{"todo only", []Receipt{{ToolName: "todo_write", Success: true}}, false},
		{"read-only context only", []Receipt{{ToolName: "read_file", Success: true, Read: true}}, false},
		{"failed execution only", []Receipt{{ToolName: "bash", Success: false}}, false},
		{"successful command counts", []Receipt{{ToolName: "bash", Success: true}}, true},
		{"complete_step counts", []Receipt{{ToolName: "complete_step", Success: true, Step: "done"}}, true},
		{"todo plus read-only context still does not count", []Receipt{{ToolName: "todo_write", Success: true}, {ToolName: "read_file", Success: true, Read: true}}, false},
		{"todo plus writer counts", []Receipt{{ToolName: "todo_write", Success: true}, {ToolName: "write_file", Success: true, Write: true}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var l *Ledger
			if tc.receipts != nil {
				l = NewLedger()
				for _, r := range tc.receipts {
					l.Record(r)
				}
			}
			if got := l.HasSuccessfulTodoProgressReceipt(); got != tc.want {
				t.Fatalf("HasSuccessfulTodoProgressReceipt() = %v, want %v", got, tc.want)
			}
		})
	}
}
