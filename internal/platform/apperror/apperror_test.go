package apperror

import (
	"errors"
	"testing"
)

// Problems names every invalid field, keeps the first as the error's Field
// and Message, and a field's first message only.
func TestProblems(t *testing.T) {
	var p Problems
	if p.Err() != nil {
		t.Fatal("no problems, an error")
	}
	p.Add("priority", "Choose a priority.")
	e, _ := As(p.Err())
	if e.Field != "priority" || e.Fields != nil || len(e.FieldMessages()) != 1 {
		t.Errorf("one problem: %+v", e)
	}
	p.Add("customer_phone", "Enter a phone number.")
	p.Add("priority", "Another message.")
	if !p.AddErr(Invalid("origin_city", "Choose a town.")) {
		t.Error("an invalid field's error wasn't taken")
	}
	if p.AddErr(NotFound("no such driver")) || p.AddErr(errors.New("database down")) || p.AddErr(Invalid("", "Bad JSON.")) {
		t.Error("an error naming no field was taken")
	}
	e, _ = As(p.Err())
	want := map[string]string{"priority": "Choose a priority.", "customer_phone": "Enter a phone number.", "origin_city": "Choose a town."}
	if e.Kind != KindInvalid || e.Field != "priority" || e.Message != "Choose a priority." || len(e.Fields) != len(want) {
		t.Fatalf("three problems: %+v", e)
	}
	for f, m := range want {
		if e.FieldMessages()[f] != m {
			t.Errorf("%s: %q, want %q", f, e.FieldMessages()[f], m)
		}
	}
}

// The API's codes are lowercase snake case; an error's own code wins.
func TestCodes(t *testing.T) {
	for k, want := range map[Kind]string{KindInvalid: "validation_error", KindNotFound: "not_found", KindConflict: "conflict",
		KindForbidden: "forbidden", KindUnauthorized: "unauthorized", KindLimit: "plan_limit"} {
		if got := (&Error{Kind: k}).APICode(); got != want {
			t.Errorf("kind %d: %q, want %q", k, got, want)
		}
	}
	if got := (&Error{Kind: KindConflict, Code: "version_mismatch"}).APICode(); got != "version_mismatch" {
		t.Errorf("own code: %q", got)
	}
}
