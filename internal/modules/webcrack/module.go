package webcrack

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/francisconeves97/jxscout/internal/core/common"
	"github.com/francisconeves97/jxscout/internal/core/dbeventbus"
	"github.com/francisconeves97/jxscout/internal/modules/beautifier"
	types "github.com/francisconeves97/jxscout/pkg/types"
)

const TopicArtifactSaved = "webcrack.artifact_saved"

type EventArtifactSaved struct {
	AssetID    int64  `json:"asset_id"`
	ArtifactID int64  `json:"artifact_id"`
	Path       string `json:"path"`
}
type Module struct {
	concurrency int
	mode        string
}

func New(concurrency int, mode string) *Module { return &Module{concurrency: concurrency, mode: mode} }
func (m *Module) Initialize(sdk *types.ModuleSDK) error {
	if m.mode == "off" {
		return nil
	}
	binary, err := exec.LookPath("webcrack")
	if err != nil {
		if m.mode == "required" {
			return err
		}
		sdk.Logger.Warn("webcrack unavailable; skipping recovered-code analysis")
		return nil
	}
	_, err = sdk.Database.Exec(`CREATE TABLE IF NOT EXISTS webcrack_artifacts(id INTEGER PRIMARY KEY,asset_id INTEGER,path TEXT UNIQUE,input_hash TEXT)`)
	if err != nil {
		return err
	}
	return sdk.DBEventBus.Subscribe(sdk.Ctx, beautifier.TopicBeautifierAssetSaved, "webcrack", func(ctx context.Context, payload []byte) error {
		var event beautifier.EventBeautifierAssetSaved
		if err := json.Unmarshal(payload, &event); err != nil {
			return err
		}
		asset, err := sdk.AssetService.GetAssetByID(ctx, event.AssetID)
		if err != nil {
			return err
		}
		if asset.ContentType != common.ContentTypeJS {
			return nil
		}
		input, err := os.ReadFile(asset.Path)
		if err != nil {
			return err
		}
		hash := common.Hash(string(input))
		out := asset.Path + ".webcrack/" + hash
		var cached int
		if err := sdk.Database.GetContext(ctx, &cached, "SELECT COUNT(*) FROM webcrack_artifacts WHERE asset_id=? AND input_hash=?", asset.ID, hash); err != nil {
			return err
		}
		if cached > 0 {
			return nil
		}
		ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, asset.Path, "-o", out, "-f")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("webcrack: %w: %.2000s", err, output)
		}
		return filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".js") {
				return nil
			}
			var id int64
			if err := sdk.Database.GetContext(ctx, &id, "INSERT INTO webcrack_artifacts(asset_id,path,input_hash) VALUES(?,?,?) ON CONFLICT(path) DO UPDATE SET input_hash=excluded.input_hash RETURNING id", asset.ID, p, hash); err != nil {
				return err
			}
			return sdk.DBEventBus.Publish(ctx, sdk.Database, TopicArtifactSaved, EventArtifactSaved{AssetID: asset.ID, ArtifactID: id, Path: p})
		})
	}, dbeventbus.Options{Concurrency: m.concurrency, MaxRetries: 1, Backoff: common.ExponentialBackoff, PollInterval: time.Second, HeartbeatInterval: 10 * time.Second})
}
