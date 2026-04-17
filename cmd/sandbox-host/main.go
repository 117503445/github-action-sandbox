package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/117503445/github-action-sandbox/internal/runnerhost"

	"github.com/rs/zerolog"
)

func main() {
	var (
		requestID      = flag.String("request-id", "", "workflow request id")
		pinggyToken    = flag.String("pinggy-token", "", "optional pinggy token")
		metadataPath   = flag.String("metadata-path", "", "path to metadata json output")
		startupTimeout = flag.Duration("startup-timeout", 2*time.Minute, "time limit for publishing metadata")
	)
	flag.Parse()

	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx = logger.WithContext(ctx)

	if err := runnerhost.Run(ctx, runnerhost.Options{
		RequestID:      *requestID,
		PinggyToken:    *pinggyToken,
		MetadataPath:   *metadataPath,
		StartupTimeout: *startupTimeout,
	}); err != nil {
		logger.Error().Err(err).Msg("sandbox host failed")
		os.Exit(1)
	}
}
