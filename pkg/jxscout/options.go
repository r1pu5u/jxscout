package jxscout

import (
	"errors"
	"github.com/francisconeves97/jxscout/internal/core/common"

	jxscouttypes "github.com/francisconeves97/jxscout/pkg/types"
)

func validateOptions(options jxscouttypes.Options) error {
	if options.ChunkParserConcurrency < 0 {
		return errors.New("chunk-parser-concurrency cannot be negative")
	}
	if err := common.ValidateProjectName(options.ProjectName); err != nil {
		return err
	}
	if options.WebcrackMode != "" && options.WebcrackMode != "auto" && options.WebcrackMode != "off" && options.WebcrackMode != "required" {
		return errors.New("webcrack must be auto, off, or required")
	}
	if options.Port == 0 {
		return errors.New("port option is required")
	}

	if options.RateLimitingMaxRequestsPerMinute != 0 && options.RateLimitingMaxRequestsPerSecond != 0 {
		return errors.New("only one of RateLimitingMaxRequestsPerMinute or RateLimitingMaxRequestsPerSecond can be set")
	}

	return nil
}
