package htmlingestion

import "testing"

func TestExtractReferences(t *testing.T) {
	result, err := extractJS(`<base href="/app/"><script src="entry.js?v=2"></script><link rel="modulepreload" href="//cdn.test:8443/lazy.mjs"><script>import('./inline.js')</script>`, "http://site.test:8080/page")
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"http://site.test:8080/app/entry.js?v=2", "http://cdn.test:8443/lazy.mjs"}
	if len(result.scriptURLs) != len(expected) {
		t.Fatalf("got %v", result.scriptURLs)
	}
	for i, value := range expected {
		if result.scriptURLs[i] != value {
			t.Fatalf("got %q want %q", result.scriptURLs[i], value)
		}
	}
	if len(result.inlineScripts) != 1 {
		t.Fatal("missing inline loader")
	}
}
