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
	"github.com/francisconeves97/jxscout/internal/modules/webcrack"
	jxscouttypes "github.com/francisconeves97/jxscout/pkg/types"
)

//go:embed chunk-discoverer.js
var chunkDiscovererBinary []byte

type chunkDiscovererModule struct {
	sdk                       *jxscouttypes.ModuleSDK
	concurrency               int
	chunkDiscovererBinaryPath string
}

func NewChunkDiscovererModule(concurrency int, chunkBruteForceLimit int) *chunkDiscovererModule {
	return &chunkDiscovererModule{
		concurrency: concurrency,
	}
}

func (m *chunkDiscovererModule) Initialize(sdk *jxscouttypes.ModuleSDK) error {
	m.sdk = sdk

	go func() {
		err := m.subscribeAssetSavedEvent()
		if err != nil {
			m.sdk.Logger.Error("failed to subscribe to asset saved topic", "err", err)
		}
	}()

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
	sdk.DBEventBus.Subscribe(sdk.Ctx, webcrack.TopicArtifactSaved, "chunkdiscoverer-webcrack", func(ctx context.Context, payload []byte) error {
		var event webcrack.EventArtifactSaved
		if err := json.Unmarshal(payload, &event); err != nil {
			return err
		}
		asset, err := sdk.AssetService.GetAssetByID(ctx, event.AssetID)
		if err != nil {
			return err
		}
		asset.Path = event.Path
		return m.discoverPossibleChunks(ctx, asset)
	}, dbeventbus.Options{Concurrency: m.concurrency, MaxRetries: 1, Backoff: common.ExponentialBackoff, PollInterval: time.Second, HeartbeatInterval: 10 * time.Second})

	return nil
}

var validChunkDiscovererContentTypes = map[common.ContentType]bool{
	common.ContentTypeJS: true,
}

func (m *chunkDiscovererModule) subscribeAssetSavedEvent() error {
	err := m.sdk.DBEventBus.Subscribe(m.sdk.Ctx, assetservice.TopicAssetSaved, "chunkdiscoverer", func(ctx context.Context, payload []byte) error {
		// unmarshal payload
		var event assetservice.EventAssetSaved
		err := json.Unmarshal(payload, &event)
		if err != nil {
			return errutil.Wrap(err, "failed to unmarshal payload")
		}

		asset, err := m.sdk.AssetService.GetAssetByID(ctx, event.AssetID)
		if err != nil {
			return dbeventbus.NewRetriableError(errutil.Wrap(err, "failed to get asset"))
		}

		err = m.handleAssetSavedEvent(ctx, asset)
		if err != nil {
			return errutil.Wrapf(err, "failed to handle asset saved event for asset (%s)", asset.URL)
		}

		return nil
	}, dbeventbus.Options{
		Concurrency:       m.concurrency,
		MaxRetries:        3,
		Backoff:           common.ExponentialBackoff,
		PollInterval:      1 * time.Second,
		HeartbeatInterval: 10 * time.Second,
	})
	if err != nil {
		return errutil.Wrap(err, "failed to subscribe to ingestion request topic")
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
	chunksRaw, err := s.execChunkDiscoverer(ctx, asset)
	if err != nil {
		return errutil.Wrap(err, "failed to exec chunk discoverer")
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

	seen := map[string]bool{}
	for _, chunk := range chunksRaw {
		ref, err := url.Parse(strings.TrimSpace(chunk))
		if err != nil {
			continue
		}
		resolved := parsedURL.ResolveReference(ref)
		if resolved.Scheme != "https" && resolved.Scheme != "http" {
			continue
		}
		resolved.Fragment = ""
		target := resolved.String()
		if target == asset.URL || seen[target] || !s.sdk.Scope.IsInScope(target) {
			continue
		}
		seen[target] = true
		chunks = append(chunks, target)
	}

	if len(chunks) > 0 {
		s.sdk.Logger.Info("discovered possible chunks 🔎", "chunks", chunks, "asset_url", asset.URL)
	}
	wg := &sync.WaitGroup{}
	sem := make(chan struct{}, s.concurrency)

	for _, chunk := range chunks {
		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore
		go func(chunkURL string) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			headers := asset.RequestHeaders
			target, _ := url.Parse(chunkURL)
			if target.Scheme != parsedURL.Scheme || target.Host != parsedURL.Host {
				headers = nil
			}
			content, found, err := s.sdk.AssetFetcher.RateLimitedGet(ctx, chunkURL, headers)
			if err != nil {
				s.sdk.Logger.Error("failed to perform get request", "err", err)
				return
			}
			if !found || common.IsProbablyHTML([]byte(content)) {
				s.sdk.Logger.Debug("asset not found", "asset_url", chunkURL)
				return
			}

			asset := assetservice.Asset{
				URL:               chunkURL,
				ContentType:       common.ContentTypeJS,
				Content:           &content,
				Project:           s.sdk.Options.ProjectName,
				RequestHeaders:    asset.RequestHeaders,
				IsChunkDiscovered: common.ToPtr(true),
				ChunkFromAssetID:  &asset.ID,
			}

			if common.StrPtr(asset.GetParentURL()) != "" {
				asset.Parent = &assetservice.Asset{
					URL: *asset.GetParentURL(),
				}
			}

			s.sdk.AssetService.AsyncSaveAsset(ctx, &asset)
		}(chunk)
	}

	wg.Wait()

	return nil
}

func (s *chunkDiscovererModule) execChunkDiscoverer(ctx context.Context, asset assetservice.Asset) ([]string, error) {
	var urls []string
	_ = s.sdk.Database.SelectContext(ctx, &urls, "SELECT url FROM assets WHERE project = ?", s.sdk.Options.ProjectName)
	seeds, _ := json.Marshal(urls)
	// Prepare the command to run the JavaScript script with Bun
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bun", "run", s.chunkDiscovererBinaryPath, asset.Path, fmt.Sprintf("%d", s.sdk.Options.ChunkDiscovererBruteForceLimit), string(seeds))

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
