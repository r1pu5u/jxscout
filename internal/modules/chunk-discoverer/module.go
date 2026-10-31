package chunkdiscoverer

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
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
	claimed                   sync.Map
	processed                 sync.Map
}

func NewChunkDiscovererModule(concurrency int, chunkBruteForceLimit int) *chunkDiscovererModule {
	return &chunkDiscovererModule{
		concurrency: concurrency,
	}
}

func (m *chunkDiscovererModule) Initialize(sdk *jxscouttypes.ModuleSDK) error {
	m.sdk = sdk
	m.fetchSlots = make(chan struct{}, max(1, m.concurrency))
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
	var assets []struct {
		ID int64 `db:"id"`
	}
	if err := sdk.Database.SelectContext(sdk.Ctx, &assets, "SELECT id FROM assets WHERE project=? AND content_type='JS' AND fs_path!=''", sdk.Options.ProjectName); err != nil {
		return err
	}
	for _, asset := range assets {
		if err := sdk.DBEventBus.Publish(sdk.Ctx, sdk.Database, "chunkdiscovery.resume", assetservice.EventAssetSaved{AssetID: asset.ID}); err != nil {
			return err
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
	source, err := os.ReadFile(asset.Path)
	if err != nil {
		return dbeventbus.NewRetriableError(err)
	}
	key := asset.URL + "\x00" + common.Hash(string(source))
	if _, duplicate := s.processed.LoadOrStore(key, true); duplicate {
		return nil
	}
	chunksRaw, err := s.execChunkDiscoverer(ctx, asset, source)
	if err != nil {
		s.processed.Delete(key)
		return dbeventbus.NewRetriableError(err)
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
		if _, duplicate := s.claimed.LoadOrStore(chunkURL, true); duplicate {
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

func (s *chunkDiscovererModule) execChunkDiscoverer(ctx context.Context, asset assetservice.Asset, source []byte) ([]string, error) {
	var urls []string
	_ = s.sdk.Database.SelectContext(ctx, &urls, "SELECT url FROM assets WHERE project = ?", s.sdk.Options.ProjectName)
	seeds, _ := json.Marshal(urls)
	// Prepare the command to run the JavaScript script with Bun
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bun", "run", s.chunkDiscovererBinaryPath, "--stdin", fmt.Sprintf("%d", s.sdk.Options.ChunkDiscovererBruteForceLimit), string(seeds))

	cmd.Stdin = bytes.NewReader(source)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("chunk discovery: %w: %s", err, stderr.String())
	}
	if strings.TrimSpace(string(output)) == "" {
		return nil, nil
	}
	return strings.Split(strings.TrimSpace(string(output)), "\n"), nil
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
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bun", s.chunkDiscovererBinaryPath, "--validate")
	cmd.Stdin = strings.NewReader(source)
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
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
