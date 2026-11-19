package events

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/rs/zerolog"
)

func TestMatches(t *testing.T) {
	for _, c := range []struct {
		pattern, typ string
		want         bool
	}{
		{"shipment.assigned", "shipment.assigned", true},
		{"shipment.assigned", "shipment.delivered", false},
		{"shipment.*", "shipment.delivered", true},
		{"shipment.*", "shipments.delivered", false},
		{"shipment.*", "shipment", false},
	} {
		if got := Matches(c.pattern, c.typ); got != c.want {
			t.Errorf("Matches(%q, %q) = %v", c.pattern, c.typ, got)
		}
	}
}

// Within subscribers run at once, in order, and their errors come back;
// After subscribers wait for the commit.
func TestPublish(t *testing.T) {
	b := New(false, nil, zerolog.Nop())
	var ran []string
	b.Within("shipment.*", func(_ context.Context, _ Tx, e Event) error {
		ran = append(ran, "within "+e.Type)
		return nil
	})
	b.Within("shipment.delivered", func(context.Context, Tx, Event) error { return errors.New("no") })
	b.After("shipment.assigned", func(_ context.Context, e Event) { ran = append(ran, "after "+e.Type) })

	var committed []func()
	tx := Tx{After: func(f func()) { committed = append(committed, f) }}
	if err := b.Publish(context.Background(), tx, Event{Type: "shipment.assigned"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ran, []string{"within shipment.assigned"}) {
		t.Fatalf("before the commit: %v", ran)
	}
	for _, f := range committed {
		f()
	}
	if !slices.Equal(ran, []string{"within shipment.assigned", "after shipment.assigned"}) {
		t.Errorf("after the commit: %v", ran)
	}
	if err := b.Publish(context.Background(), tx, Event{Type: "shipment.delivered"}); err == nil {
		t.Error("a subscriber's error was lost")
	}
	if err := b.Publish(context.Background(), Tx{}, Event{Type: "shipment.assigned"}); err != nil || ran[len(ran)-1] != "after shipment.assigned" {
		t.Errorf("without a transaction, After runs at once: %v, %v", ran, err)
	}
}
