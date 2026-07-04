// Package observability configures how services are observed. Logging only
// for now; metrics and tracing land here later without import churn.
package observability

import (
	"log/slog"
	"os"
	"strings"
)

// NewLogger builds the service logger, configured from the environment:
//
//	LOG_LEVEL:  debug | info | warn | error   (default info)
//	LOG_FORMAT: json | text                   (default json)
//
// Every line carries a "service" attribute. Set it as the process default in
// main() so kernel packages (app, events, ...) log through it too:
//
//	logger := observability.NewLogger("myservice")
//	slog.SetDefault(logger)
func NewLogger(service string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(os.Getenv("LOG_LEVEL"))}

	var handler slog.Handler
	if strings.EqualFold(os.Getenv("LOG_FORMAT"), "text") {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	return slog.New(handler).With("service", service)
}

func parseLevel(raw string) slog.Level {
	switch strings.ToLower(raw) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
