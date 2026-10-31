package chunkdiscoverer

import (
	"context"
	"crypto/sha256"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	assetservice "github.com/francisconeves97/jxscout/internal/core/asset-service"
	types "github.com/francisconeves97/jxscout/pkg/types"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

func TestDeduplicationCacheIsBoundedAndEvictsLeastRecent(t *testing.T) {
	cache := newBoundedSet(2)
	if cache.add("a") || cache.add("b") || !cache.add("a") {
		t.Fatal("incorrect duplicate detection")
	}
	cache.add("c")
	if len(cache.entries) != 2 || cache.entries[sha256.Sum256([]byte("b"))] != nil {
		t.Fatal("cache did not evict least recently used key")
	}
	cache.remove("a")
	if cache.add("a") {
		t.Fatal("removed entry remained cached")
	}
}

func TestPersistentCandidateCacheAvoidsParsingAndInvalidates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	root := t.TempDir()
	count := filepath.Join(root, "parser-calls")
	t.Setenv("JXSCOUT_TEST_PARSER_CALLS", count)
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(root, "bun"), []byte("#!/bin/sh\ncat >/dev/null\nprintf x >> \"$JXSCOUT_TEST_PARSER_CALLS\"\nprintf './lazy.js\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sqlx.Connect("sqlite3", filepath.Join(root, "cache.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.MustExec(`CREATE TABLE assets(project TEXT,url TEXT,content_type TEXT);INSERT INTO assets VALUES('test','https://example.com/app.aabbccdd.js','JS');CREATE TABLE chunk_discovery_cache(project TEXT,asset_id INTEGER,cache_key TEXT,candidates TEXT,updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,PRIMARY KEY(project,asset_id));`)
	source := filepath.Join(root, "app.js")
	if err := os.WriteFile(source, []byte("import('./lazy.js');"), 0600); err != nil {
		t.Fatal(err)
	}
	module := NewChunkDiscovererModule(5, 0)
	module.parserSlots = make(chan struct{}, 1)
	module.processorHash = "processor-v1"
	module.sdk = &types.ModuleSDK{Database: db, Options: types.Options{ProjectName: "test"}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	asset := assetservice.Asset{ID: 1, Path: source, URL: "https://example.com/app.aabbccdd.js"}
	extract := func(expectedCalls int) {
		t.Helper()
		module.processed = newBoundedSet(1024) // Simulate a fresh process.
		result, err := module.extractCandidates(context.Background(), asset)
		if err != nil || !reflect.DeepEqual(result, []string{"./lazy.js"}) {
			t.Fatalf("result=%v err=%v", result, err)
		}
		calls, err := os.ReadFile(count)
		if err != nil || len(calls) != expectedCalls {
			t.Fatalf("parser calls=%q, want %d: %v", calls, expectedCalls, err)
		}
	}
	extract(1)
	extract(1) // Restart reuses candidates but still returns them for missing-file recovery.
	module.processorHash = "processor-v2"
	extract(2)
	module.sdk.Options.ChunkDiscovererBruteForceLimit = 10
	extract(3)
	if err := os.WriteFile(source, []byte("import('./new.js');"), 0600); err != nil {
		t.Fatal(err)
	}
	extract(4)
	db.MustExec("INSERT INTO assets VALUES('test','https://example.com/runtime.12345678.js','JS')")
	extract(5)
	var payload string
	if err := db.Get(&payload, "SELECT candidates FROM chunk_discovery_cache"); err != nil || !strings.Contains(payload, "lazy.js") {
		t.Fatal("candidate cache not persisted")
	}
}
