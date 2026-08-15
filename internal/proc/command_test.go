package proc

import (
	"context"
	"testing"
)

func TestCommandConstructorsPreserveArguments(t *testing.T) {
	background := Command("reasonix-helper", "--probe", "a b")
	if len(background.Args) != 3 || background.Args[1] != "--probe" || background.Args[2] != "a b" {
		t.Fatalf("background args = %#v", background.Args)
	}
	visible := VisibleCommandContext(context.Background(), "reasonix-ui", "--open")
	if len(visible.Args) != 2 || visible.Args[1] != "--open" {
		t.Fatalf("visible args = %#v", visible.Args)
	}
}
