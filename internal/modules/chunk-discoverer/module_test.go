package chunkdiscoverer

import (
	"net/url"
	"reflect"
	"testing"
)

func TestResolveCandidatesPrefersExplicitRootAndDeduplicates(t *testing.T) {
	source := "https://example.com/js/app.aabbccdd.js"
	base, _ := url.Parse(source)
	candidates := []string{"js/Dashboard.12345678.js", "https://example.com/js/Dashboard.12345678.js", "/js/Dashboard.12345678.js", "./nested.js", "https://outside.invalid/chunk.js", "data:text/javascript,bad", source}
	got := resolveCandidates(base, source, candidates, func(raw string) bool { u, _ := url.Parse(raw); return u.Host == "example.com" })
	want := []string{"https://example.com/js/Dashboard.12345678.js", "https://example.com/js/nested.js"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved URLs=%v, want %v", got, want)
	}
}
