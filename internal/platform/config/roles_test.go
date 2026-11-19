package config

import "testing"

func TestParseRoles(t *testing.T) {
	for in, want := range map[string]string{
		"all":            "web,worker,scheduler,relay",
		"web":            "web",
		"relay, worker ": "worker,relay",
		"all,web":        "web,worker,scheduler,relay",
	} {
		r, err := ParseRoles(in)
		if err != nil || r.String() != want {
			t.Errorf("ParseRoles(%q) = %s, %v; want %s", in, r, err, want)
		}
	}
	for _, bad := range []string{"", ",", "api", "web,cron"} {
		if _, err := ParseRoles(bad); err == nil {
			t.Errorf("ParseRoles(%q) took it", bad)
		}
	}
	if !(Roles{}).Has("relay") {
		t.Error("an empty set runs every role")
	}
	if (Roles{"web": true}).Has("worker") {
		t.Error("web alone runs no worker")
	}
}
