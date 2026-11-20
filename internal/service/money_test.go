package service

import "testing"

func TestParseBirr(t *testing.T) {
	for in, want := range map[string]int64{"": 0, "2875": 287500, " 2,875.5 ": 287550, "-100": -10000, "0.05": 5, "1,000,000.00": 100000000} {
		if got, err := ParseBirr(in); err != nil || got != want {
			t.Errorf("ParseBirr(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"12.345", "abc", "1e5", "--5", "5-", "NaN", "1.2.3"} {
		if _, err := ParseBirr(in); err == nil {
			t.Errorf("ParseBirr(%q) was taken", in)
		}
	}
	for santim, want := range map[int64]string{0: "", 287550: "2875.50", -10000: "-100.00", 5: "0.05", -5: "-0.05"} {
		if got := BirrText(santim); got != want {
			t.Errorf("BirrText(%d) = %q, want %q", santim, got, want)
		}
		if back, _ := ParseBirr(BirrText(santim)); back != santim {
			t.Errorf("%d came back as %d", santim, back)
		}
	}
}
