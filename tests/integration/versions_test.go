//go:build integration

package integration

import "testing"

// TestVersionsRaisedByEdits (FR-040; audit D7): an edit raises the record's
// version, an update that changes nothing a person edits (a sign-in, the
// billing month, a no-op save) doesn't, and no query can set it.
func TestVersionsRaisedByEdits(t *testing.T) {
	e := newEnv(t)
	office := e.company("Versioned Freight")
	driverID, phone, pin := office.addDriver("Abebe Bikila")
	id, _ := office.book("Addis Ababa", "Adama", "")
	version := func(table, id string) int {
		t.Helper()
		return scalar[int](t, `SELECT version FROM `+table+` WHERE id = $1`, id)
	}
	if v := version("shipments", id); v != 1 {
		t.Fatalf("a new load is version %d, want 1", v)
	}

	// The office's edit counts; saving it again unchanged doesn't.
	edit := bookingForm()
	edit.Set("special_instructions", "Call the gate first")
	office.mustPost("/app/shipments/"+id+"/edit", edit)
	if v := version("shipments", id); v != 2 {
		t.Errorf("after an edit: version %d, want 2", v)
	}
	office.mustPost("/app/shipments/"+id+"/edit", edit)
	if v := version("shipments", id); v != 2 {
		t.Errorf("after saving the same again: version %d, want 2", v)
	}
	exec(t, `UPDATE shipments SET updated_at = now(), confirm_token = 'x' WHERE id = $1`, id)
	if v := version("shipments", id); v != 2 {
		t.Errorf("after bookkeeping only: version %d, want 2", v)
	}
	exec(t, `UPDATE shipments SET version = 99 WHERE id = $1`, id)
	if v := version("shipments", id); v != 2 {
		t.Errorf("set by hand: version %d, want it kept at 2", v)
	}

	// A driver signing in, and choosing their PIN the first time, isn't an
	// edit of them; a new name is.
	before := version("users", driverID)
	e.signIn(phone, pin)
	if v := version("users", driverID); v != before {
		t.Errorf("after signing in: version %d, want %d", v, before)
	}
	exec(t, `UPDATE users SET full_name = 'Abebe B. Bikila' WHERE id = $1`, driverID)
	if v := version("users", driverID); v != before+1 {
		t.Errorf("after a new name: version %d, want %d", v, before+1)
	}

	tenant := scalar[string](t, `SELECT tenant_id::text FROM shipments WHERE id = $1`, id)
	before = version("tenants", tenant)
	exec(t, `UPDATE tenants SET sms_billed_month = date_trunc('month', now()), sms_month_quota = sms_quota WHERE id = $1`, tenant)
	if v := version("tenants", tenant); v != before {
		t.Errorf("after the billing job: version %d, want %d", v, before)
	}
	exec(t, `UPDATE tenants SET contact_phone = '0911223344' WHERE id = $1`, tenant)
	if v := version("tenants", tenant); v != before+1 {
		t.Errorf("after a settings change: version %d, want %d", v, before+1)
	}
}
