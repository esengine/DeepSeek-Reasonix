package tui_test

import (
	"context"
	"testing"
	"time"
)

func TestModelCatalogAndSelectionThroughKernel(t *testing.T) {
	c := inProcessKernel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	models, err := c.Models(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ref string
	for _, choice := range models {
		if choice.Provider == "test-model" {
			ref = choice.Ref
		}
	}
	if ref == "" {
		t.Fatalf("fixture model missing from catalog: %+v", models)
	}
	if err := c.SelectModel(ctx, ref); err != nil {
		t.Fatal(err)
	}
	status, err := c.Status(ctx)
	if err != nil || status.ModelRef != ref {
		t.Fatalf("model after selection = %s, %v; want %s", status.ModelRef, err, ref)
	}
}

func TestSkillActivationThroughKernel(t *testing.T) {
	c := inProcessKernel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	choices, err := c.Capabilities(ctx, "skills")
	if err != nil {
		t.Fatal(err)
	}
	if len(choices) == 0 {
		t.Fatal("expected built-in skills")
	}
	choice := choices[0]
	if err := c.SetCapabilityEnabled(ctx, "skills", choice.Name, !choice.Enabled); err != nil {
		t.Fatal(err)
	}
	choices, err = c.Capabilities(ctx, "skills")
	if err != nil {
		t.Fatal(err)
	}
	for _, changed := range choices {
		if changed.Name == choice.Name {
			if changed.Enabled == choice.Enabled {
				t.Fatal("activation switch did not reach catalog")
			}
			return
		}
	}
	t.Fatal("toggled skill disappeared from management catalog")
}
