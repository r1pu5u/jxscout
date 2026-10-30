package assetfetcher

import (
	"net/http"
	"strings"
	"testing"
)

func TestRedirectOriginAndScope(t *testing.T) {
	fetcher := NewAssetFetcher(AssetFetcherOptions{AllowedURL: func(raw string) bool { return !strings.Contains(raw, "denied") }})
	original, _ := http.NewRequest("GET", "https://app.test/entry.js", nil)
	other, _ := http.NewRequest("GET", "https://cdn.test/chunk.js", nil)
	other.Header.Set("X-Api-Key", "secret")
	if err := fetcher.client.CheckRedirect(other, []*http.Request{original}); err != nil {
		t.Fatal(err)
	}
	if other.Header.Get("X-Api-Key") != "" {
		t.Fatal("cross-origin redirect retained custom credentials")
	}
	denied, _ := http.NewRequest("GET", "https://denied.test/chunk.js", nil)
	if err := fetcher.client.CheckRedirect(denied, []*http.Request{original}); err == nil {
		t.Fatal("accepted excluded redirect")
	}
	same, _ := http.NewRequest("GET", "https://app.test/chunk.js", nil)
	same.Header.Set("Authorization", "Bearer secret")
	if err := fetcher.client.CheckRedirect(same, []*http.Request{original}); err != nil {
		t.Fatal(err)
	}
	if same.Header.Get("Authorization") == "" {
		t.Fatal("lost same-origin credentials")
	}
}
