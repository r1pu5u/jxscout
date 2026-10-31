package chunkdiscoverer

import (
	"context"
	"errors"
	assetservice "github.com/francisconeves97/jxscout/internal/core/asset-service"
	"net/url"
	"reflect"
	"testing"
	"time"
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

func TestParserGateBlocksExtractionAndValidationBeforeAllocating(t *testing.T) {
	module := NewChunkDiscovererModule(5, 0)
	module.parserSlots = make(chan struct{}, 1)
	module.parserSlots <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := module.extractCandidates(ctx, assetservice.Asset{Path: "/does-not-exist.js"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("extract bypassed the parser gate: %v", err)
	}
	if _, err := module.validateJavaScript(ctx, "const test=true;"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("validator bypassed the shared parser gate: %v", err)
	}
}
