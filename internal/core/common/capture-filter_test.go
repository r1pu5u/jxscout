package common

import "testing"

func TestCaptureNoise(t *testing.T) {
	for _, raw := range []string{"https://example.com/favicon.ico?v=1", "https://example.com/cdn-cgi/challenge-platform/h/g/token", "https://example.com/cdn-cgi/scripts/jsd/main.js"} {
		if !IsCaptureNoise(raw) {
			t.Errorf("accepted noise %s", raw)
		}
	}
	for _, raw := range []string{"https://example.com/js/chunk.a1b2.js", "https://example.com/cdn-cgi/application.js", "https://example.com/"} {
		if IsCaptureNoise(raw) {
			t.Errorf("rejected app URL %s", raw)
		}
	}
}
