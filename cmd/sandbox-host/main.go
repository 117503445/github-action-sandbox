package main

import (
	"context"
	"flag"
	"os"
	"time"

	"github.com/117503445/github-action-sandbox/internal/runnerhost"

	"github.com/rs/zerolog"
)

func main() {
	var (
		requestID      = flag.String("request-id", "", "workflow request id")
		uptermServer   = flag.String("upterm-server", "ssh://uptermd.upterm.dev:22", "upterm server")
		metadataPath   = flag.String("metadata-path", "", "path to metadata json output")
		startupTimeout = flag.Duration("startup-timeout", 2*time.Minute, "time limit for publishing metadata")
	)
	flag.Parse()

	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
	ctx := logger.WithContext(context.Background())

	if err := runnerhost.Run(ctx, runnerhost.Options{
		RequestID:      *requestID,
		UptermServer:   *uptermServer,
		MetadataPath:   *metadataPath,
		StartupTimeout: *startupTimeout,
	}); err != nil {
		logger.Error().Err(err).Msg("sandbox host failed")
		os.Exit(1)
	}
}
