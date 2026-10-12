// Package observability configures how services are observed. Logging only
// for now; metrics and tracing land here later without import churn.
package observability

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// NewLogger builds the service logger, configured from the environment:
//
//	LOG_LEVEL:  debug | info | warn | error   (default info)
//	LOG_FORMAT: json | text                   (default json)
//
// Every line carries a "service" attribute and goes to stdout. Set it as the
// process default in main() so kernel packages (app, events, ...) log through
// it too:
//
//	logger := observability.NewLogger("myservice")
//	slog.SetDefault(logger)
func NewLogger(service string) *slog.Logger {
	logger, _ := NewLoggerWith(service, Options{}) // zero Options cannot fail
	return logger
}

// Options chooses where and how NewLoggerWith logs. Every zero field keeps
// NewLogger's behavior for that field.
type Options struct {
	// Writer receives the log lines; nil is os.Stdout. Compose destinations
	// with io.MultiWriter.
	Writer io.Writer
	// Level is the minimum level; nil reads LOG_LEVEL (default info). When
	// set, the environment is not consulted — pass a *slog.LevelVar to change
	// the level at runtime.
	Level slog.Leveler
	// Format is "json" or "text", case-insensitive; empty reads LOG_FORMAT
	// (default json). Any other value is an error.
	Format string
}

// NewLoggerWith is NewLogger with the destination, level and format chosen
// by the caller. Its only error is an unknown Options.Format; an unknown
// LOG_FORMAT or LOG_LEVEL in the environment falls back to the default, as
// in NewLogger.
func NewLoggerWith(service string, opts Options) (*slog.Logger, error) {
	text := strings.EqualFold(os.Getenv("LOG_FORMAT"), "text")
	if opts.Format != "" {
		switch strings.ToLower(opts.Format) {
		case "json":
			text = false
		case "text":
			text = true
		default:
			return nil, fmt.Errorf("observability: unknown log format %q (want json or text)", opts.Format)
		}
	}

	w := opts.Writer
	if w == nil {
		w = os.Stdout
	}
	level := opts.Level
	if level == nil {
		level = parseLevel(os.Getenv("LOG_LEVEL"))
	}
	hopts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if text {
		handler = slog.NewTextHandler(w, hopts)
	} else {
		handler = slog.NewJSONHandler(w, hopts)
	}
	return slog.New(handler).With("service", service), nil
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
