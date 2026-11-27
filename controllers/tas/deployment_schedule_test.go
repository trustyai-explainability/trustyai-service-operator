package tas

import "testing"

func TestMetricsScheduleSeconds(t *testing.T) {
	cases := map[string]string{
		"": "5", "5": "5", "5s": "5", "5m": "300", "1h": "3600", "0": "5", "bogus": "5", "500ms": "1",
	}
	for in, want := range cases {
		if got := metricsScheduleSeconds(in); got != want {
			t.Fatalf("metricsScheduleSeconds(%q)=%q want %q", in, got, want)
		}
	}
}
