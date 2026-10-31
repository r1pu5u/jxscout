package jxscout

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/francisconeves97/jxscout/internal/core/tui"
	"github.com/francisconeves97/jxscout/internal/modules/overrides"
	jxscouttypes "github.com/francisconeves97/jxscout/pkg/types"
)

type tuiJXScoutWrapper struct {
	jxscout *jxscout
}

func (t *tuiJXScoutWrapper) GetAnalysisMatches(ctx context.Context) ([]tui.AnalysisMatch, error) {
	var rows []struct {
		Path    string `db:"asset_path"`
		Results string `db:"results"`
	}
	err := t.jxscout.db.SelectContext(ctx, &rows, `SELECT asset_path, results FROM ast_analysis_results WHERE deleted_at IS NULL AND asset_type != 'webcrack' ORDER BY updated_at DESC`)
	if err != nil {
		// Older projects may not have run the analyzer yet.
		if strings.Contains(err.Error(), "no such table") {
			return nil, nil
		}
		return nil, err
	}
	var matches []tui.AnalysisMatch
	for _, row := range rows {
		var parsed []struct {
			FilePath string `json:"filePath"`
			Analyzer string `json:"analyzerName"`
			Value    string `json:"value"`
			Start    struct {
				Line   int `json:"line"`
				Column int `json:"column"`
			} `json:"start"`
		}
		if err := json.Unmarshal([]byte(row.Results), &parsed); err != nil {
			return nil, fmt.Errorf("decode AST analysis results: %w", err)
		}
		for _, p := range parsed {
			path := p.FilePath
			if path == "" {
				path = row.Path
			}
			matches = append(matches, tui.AnalysisMatch{AssetPath: path, Analyzer: p.Analyzer, Value: p.Value, Line: p.Start.Line, Column: p.Start.Column})
		}
	}
	return matches, nil
}

func (t *tuiJXScoutWrapper) GetLogBuffer() tui.LogBuffer {
	return t.jxscout.logBuffer
}

func (t *tuiJXScoutWrapper) Stop() error {
	return t.jxscout.Stop()
}

func (t *tuiJXScoutWrapper) GetOptions() jxscouttypes.Options {
	return t.jxscout.options
}

func (t *tuiJXScoutWrapper) Restart(options jxscouttypes.Options) (tui.JXScout, error) {
	jxscout, err := t.jxscout.Restart(options)
	if err != nil {
		return nil, err
	}

	t.jxscout = jxscout

	return &tuiJXScoutWrapper{jxscout: jxscout}, nil
}

func (t *tuiJXScoutWrapper) GetOverridesModule() overrides.OverridesModule {
	return t.jxscout.overridesModule
}

func (t *tuiJXScoutWrapper) GetAssetService() jxscouttypes.AssetService {
	return t.jxscout.assetService
}

func (t *tuiJXScoutWrapper) Ctx() context.Context {
	return t.jxscout.ctx
}

func (t *tuiJXScoutWrapper) TruncateTables() error {
	return t.jxscout.TruncateTables()
}

func (s *jxscout) runPrompt() {
	t := tui.New(&tuiJXScoutWrapper{jxscout: s})
	t.RegisterDefaultCommands()
	err := t.Run()
	if err != nil {
		s.log.Error("failed to run prompt", "error", err)
	}
}
