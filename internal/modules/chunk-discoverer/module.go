package chunkdiscoverer

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	assetservice "github.com/francisconeves97/jxscout/internal/core/asset-service"
	"github.com/francisconeves97/jxscout/internal/core/common"
	"github.com/francisconeves97/jxscout/internal/core/dbeventbus"
	"github.com/francisconeves97/jxscout/internal/core/errutil"
	"github.com/francisconeves97/jxscout/internal/modules/beautifier"
	jxscouttypes "github.com/francisconeves97/jxscout/pkg/types"
)

//go:embed chunk-discoverer.js
var chunkDiscovererBinary []byte

type chunkDiscovererModule struct {
	sdk                       *jxscouttypes.ModuleSDK
	concurrency               int
	chunkDiscovererBinaryPath string
	fetchSlots                chan struct{}
	claimed                   *boundedSet
	processed                 *boundedSet
	parserSlots               chan struct{}
	processorHash             string
}

func NewChunkDiscovererModule(concurrency int, chunkBruteForceLimit int) *chunkDiscovererModule {
	return &chunkDiscovererModule{
		concurrency: concurrency,
		claimed:     newBoundedSet(4096), processed: newBoundedSet(1024),
	}
}

func (m *chunkDiscovererModule) Initialize(sdk *jxscouttypes.ModuleSDK) error {
	m.sdk = sdk
	m.fetchSlots = make(chan struct{}, max(1, m.concurrency))
	m.parserSlots = make(chan struct{}, max(1, sdk.Options.ChunkParserConcurrency))
	m.processorHash = common.Hash(string(chunkDiscovererBinary))
	if _, err := sdk.Database.Exec(`CREATE TABLE IF NOT EXISTS chunk_discovery_cache (
 project TEXT NOT NULL, asset_id INTEGER NOT NULL, cache_key TEXT NOT NULL, candidates TEXT NOT NULL,
 updated_at DATETIME DEFAULT CURRENT_TIMESTAMP, PRIMARY KEY(project,asset_id))`); err != nil {
		return err
	}
	if _, err := sdk.Database.Exec(`CREATE TABLE IF NOT EXISTS chunk_discoveries (
 project TEXT NOT NULL, url TEXT NOT NULL, source_asset_id INTEGER NOT NULL,
 status TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '', updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(project,url))`); err != nil {
		return err
	}

	saveDir := filepath.Join(common.GetPrivateDirectoryRoot(), "extracted")

	// Create the directory if it doesn't exist
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		return errutil.Wrap(err, "failed to create binaries directory")
	}

	// Define the path for the extracted binary
	binaryPath := filepath.Join(saveDir, "chunk-discoverer.js")
	if err := os.WriteFile(binaryPath, chunkDiscovererBinary, 0755); err != nil {
		return errutil.Wrap(err, "failed to write chunk discoverer file")
	}

	m.chunkDiscovererBinaryPath = binaryPath

	if err := m.subscribeAssetSavedEvent(); err != nil {
		return err
	}
	// Resume from saved JavaScript even when no new Burp traffic arrives.
	var after int64
	for {
		var assets []struct {
			ID int64 `db:"id"`
		}
		if err := sdk.Database.SelectContext(sdk.Ctx, &assets, "SELECT id FROM assets WHERE project=? AND content_type='JS' AND fs_path!='' AND id>? ORDER BY id LIMIT 128", sdk.Options.ProjectName, after); err != nil {
			return err
		}
		if len(assets) == 0 {
			break
		}
		for _, asset := range assets {
			if err := sdk.DBEventBus.Publish(sdk.Ctx, sdk.Database, "chunkdiscovery.resume", assetservice.EventAssetSaved{AssetID: asset.ID}); err != nil {
				return err
			}
			after = asset.ID
		}
	}

	return nil
}

var validChunkDiscovererContentTypes = map[common.ContentType]bool{
	common.ContentTypeJS: true,
}

func (m *chunkDiscovererModule) subscribeAssetSavedEvent() error {
	handler := func(ctx context.Context, payload []byte) error {
		var event assetservice.EventAssetSaved
		if err := json.Unmarshal(payload, &event); err != nil {
			return err
		}
		asset, err := m.sdk.AssetService.GetAssetByID(ctx, event.AssetID)
		if err != nil {
			return dbeventbus.NewRetriableError(err)
		}
		return m.handleAssetSavedEvent(ctx, asset)
	}
	for _, subscription := range []struct{ topic, name string }{
		{assetservice.TopicAssetSaved, "chunkdiscoverer-active-v3"},
		{beautifier.TopicBeautifierAssetSaved, "chunkdiscoverer-formatted-v3"},
		{"chunkdiscovery.resume", "chunkdiscoverer-resume-v3"},
	} {
		if err := m.sdk.DBEventBus.Subscribe(m.sdk.Ctx, subscription.topic, subscription.name, handler, dbeventbus.Options{Concurrency: max(1, m.concurrency), MaxRetries: 3, Backoff: common.ExponentialBackoff, PollInterval: time.Second, HeartbeatInterval: 10 * time.Second}); err != nil {
			return err
		}
	}
	return nil
}

func (m *chunkDiscovererModule) handleAssetSavedEvent(ctx context.Context, asset assetservice.Asset) error {

	if isValid, ok := validChunkDiscovererContentTypes[asset.ContentType]; !ok || !isValid {
		return nil
	}

	return m.discoverPossibleChunks(ctx, asset)
}

func (s *chunkDiscovererModule) discoverPossibleChunks(ctx context.Context, asset assetservice.Asset) error {
	chunksRaw, err := s.extractCandidates(ctx, asset)
	if err != nil {
		return dbeventbus.NewRetriableError(err)
	}
	if len(chunksRaw) == 0 {
		return nil
	}

	chunks := []string{}

	baseURL := asset.URL
	if asset.DiscoveryBaseURL != "" {
		baseURL = asset.DiscoveryBaseURL
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return errutil.Wrap(err, "failed to parse original url")
	}

	chunks = resolveCandidates(parsedURL, asset.URL, chunksRaw, s.sdk.Scope.IsInScope)

	if len(chunks) > 0 {
		s.sdk.Logger.Info("chunk candidates", "count", len(chunks), "asset_url", asset.URL)
	}
	wg := &sync.WaitGroup{}
	for _, chunkURL := range chunks {
		if common.IsCaptureNoise(chunkURL) {
			continue
		}
		if s.claimed.add(chunkURL) {
			continue
		}
		// Previously captured URLs need no additional network request.
		if saved, exists, err := s.sdk.AssetService.GetAssetByURL(ctx, chunkURL); err == nil && exists {
			if info, statErr := os.Stat(saved.Path); statErr == nil && info.Mode().IsRegular() {
				status := "existing"
				if saved.IsChunkDiscovered != nil && *saved.IsChunkDiscovered {
					status = "downloaded"
				}
				s.recordDiscovery(ctx, asset.ID, chunkURL, status, "")
				continue
			}
		}
		select {
		case s.fetchSlots <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		wg.Add(1)
		go func(chunkURL string) {
			defer wg.Done()
			defer func() { <-s.fetchSlots }()
			s.recordDiscovery(ctx, asset.ID, chunkURL, "fetching", "")
			headers := asset.RequestHeaders
			target, _ := url.Parse(chunkURL)
			if target.Scheme != parsedURL.Scheme || target.Host != parsedURL.Host {
				headers = nil
			}
			requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			content, found, err := s.sdk.AssetFetcher.RateLimitedGet(requestCtx, chunkURL, headers)
			if err != nil {
				s.recordDiscovery(ctx, asset.ID, chunkURL, "error", err.Error())
				s.sdk.Logger.Debug("chunk fetch failed", "url", chunkURL, "err", err)
				return
			}
			if !found || common.IsProbablyHTML([]byte(content)) {
				s.recordDiscovery(ctx, asset.ID, chunkURL, "not_found", "not JavaScript or HTTP non-200")
				return
			}
			valid, err := s.validateJavaScript(ctx, content)
			if err != nil {
				s.recordDiscovery(ctx, asset.ID, chunkURL, "error", err.Error())
				return
			}
			if !valid {
				s.recordDiscovery(ctx, asset.ID, chunkURL, "invalid_js", "HTTP 200 response is not a valid JavaScript program")
				return
			}
			discovered := assetservice.Asset{URL: chunkURL, ContentType: common.ContentTypeJS, Content: &content, Project: s.sdk.Options.ProjectName, RequestHeaders: headers, IsChunkDiscovered: common.ToPtr(true), ChunkFromAssetID: &asset.ID, Parent: &assetservice.Asset{URL: asset.URL}}
			s.sdk.AssetService.AsyncSaveAsset(ctx, &discovered)
			s.recordDiscovery(ctx, asset.ID, chunkURL, "downloaded", "")
			s.sdk.Logger.Info("downloaded discovered chunk", "url", chunkURL)
		}(chunkURL)
	}

	wg.Wait()

	return nil
}

func (s *chunkDiscovererModule) extractCandidates(ctx context.Context, asset assetservice.Asset) ([]string, error) {
	// All event subscriptions share this gate; only the configured number of ASTs
	// and source streams can be resident, including validation subprocesses.
	select {
	case s.parserSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-s.parserSlots }()
	source, err := os.Open(asset.Path)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, source); err != nil {
		return nil, err
	}
	inputHash := fmt.Sprintf("%x", digest.Sum(nil))
	key := asset.URL + "\x00" + inputHash
	if s.processed.add(key) {
		return nil, nil
	}
	var urls []string
	if err := s.sdk.Database.SelectContext(ctx, &urls, "SELECT url FROM assets WHERE project=? AND content_type='JS' ORDER BY url", s.sdk.Options.ProjectName); err != nil {
		s.processed.remove(key)
		return nil, err
	}
	seeds, err := json.Marshal(urls)
	if err != nil {
		s.processed.remove(key)
		return nil, err
	}
	cacheKey := common.Hash(inputHash + s.processorHash + common.Hash(string(seeds)) + fmt.Sprint(s.sdk.Options.ChunkDiscovererBruteForceLimit))
	var cached string
	if err := s.sdk.Database.GetContext(ctx, &cached, "SELECT candidates FROM chunk_discovery_cache WHERE project=? AND asset_id=? AND cache_key=?", s.sdk.Options.ProjectName, asset.ID, cacheKey); err == nil {
		var candidates []string
		if json.Unmarshal([]byte(cached), &candidates) == nil {
			return candidates, nil
		}
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		s.processed.remove(key)
		return nil, err
	}
	childCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(childCtx, "bun", s.chunkDiscovererBinaryPath, "--stream", fmt.Sprint(s.sdk.Options.ChunkDiscovererBruteForceLimit))
	cmd.Stdin = io.MultiReader(bytes.NewReader(append(seeds, '\n')), source)
	stderr := common.NewTailBuffer(8192)
	cmd.Stderr = stderr
	output, err := cmd.Output()
	if err != nil {
		s.processed.remove(key)
		return nil, fmt.Errorf("chunk discovery: %w: %.2000s", err, stderr.String())
	}
	candidates := []string{}
	if strings.TrimSpace(string(output)) != "" {
		candidates = strings.Split(strings.TrimSpace(string(output)), "\n")
	}
	encoded, _ := json.Marshal(candidates)
	if _, err := s.sdk.Database.ExecContext(ctx, `INSERT INTO chunk_discovery_cache(project,asset_id,cache_key,candidates) VALUES(?,?,?,?) ON CONFLICT(project,asset_id) DO UPDATE SET cache_key=excluded.cache_key,candidates=excluded.candidates,updated_at=CURRENT_TIMESTAMP`, s.sdk.Options.ProjectName, asset.ID, cacheKey, string(encoded)); err != nil {
		s.sdk.Logger.Debug("could not cache chunk candidates", "err", err)
	}
	return candidates, nil
}

func (s *chunkDiscovererModule) recordDiscovery(ctx context.Context, source int64, url, status, detail string) {
	_, err := s.sdk.Database.ExecContext(ctx, `INSERT INTO chunk_discoveries(project,url,source_asset_id,status,detail) VALUES(?,?,?,?,?) ON CONFLICT(project,url) DO UPDATE SET status=excluded.status,detail=excluded.detail,updated_at=CURRENT_TIMESTAMP`, s.sdk.Options.ProjectName, url, source, status, detail)
	if err != nil && ctx.Err() == nil {
		s.sdk.Logger.Warn("could not record chunk status", "err", err)
	}
}

func resolveCandidates(parsedURL *url.URL, sourceURL string, raw []string, allowed func(string) bool) []string {
	chunks := []string{}
	explicit := map[string]bool{}
	for _, candidate := range raw {
		ref, err := url.Parse(strings.TrimSpace(candidate))
		if err != nil {
			continue
		}
		if ref.IsAbs() || strings.HasPrefix(ref.Path, "/") {
			explicit[parsedURL.ResolveReference(ref).String()] = true
		}
	}
	seen := map[string]bool{}
	for _, chunk := range raw {
		ref, err := url.Parse(strings.TrimSpace(chunk))
		if err != nil {
			continue
		}
		resolved := parsedURL.ResolveReference(ref)
		if !ref.IsAbs() && !strings.HasPrefix(ref.Path, "/") {
			fromRoot := *ref
			fromRoot.Path = "/" + ref.Path
			rootTarget := parsedURL.ResolveReference(&fromRoot)
			if explicit[rootTarget.String()] {
				resolved = rootTarget
			}
		}
		if resolved.Scheme != "https" && resolved.Scheme != "http" {
			continue
		}
		resolved.Fragment = ""
		target := resolved.String()
		if target == sourceURL || seen[target] || !allowed(target) {
			continue
		}
		seen[target] = true
		chunks = append(chunks, target)
	}

	return chunks
}

func (s *chunkDiscovererModule) validateJavaScript(ctx context.Context, source string) (bool, error) {
	select {
	case s.parserSlots <- struct{}{}:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	defer func() { <-s.parserSlots }()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bun", s.chunkDiscovererBinaryPath, "--validate")
	cmd.Stdin = strings.NewReader(source)
	diagnostics := common.NewTailBuffer(8192)
	cmd.Stderr = diagnostics
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("JavaScript validation: %w: %.1000s", err, diagnostics.String())
	}
	var result struct {
		Valid bool `json:"valid"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return false, err
	}
	return result.Valid, nil
}
