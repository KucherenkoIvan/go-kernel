package observability_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/KucherenkoIvan/go-kernel/observability"
)

func mustLogger(t *testing.T, opts observability.Options) *slog.Logger {
	t.Helper()
	logger, err := observability.NewLoggerWith("svc", opts)
	if err != nil {
		t.Fatal(err)
	}
	return logger
}

func jsonLines(t *testing.T, out string) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("not a JSON line %q: %v", l, err)
		}
		delete(m, "time")
		lines = append(lines, m)
	}
	return lines
}

func TestNewLoggerWith_WriterReceivesLines(t *testing.T) {
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("LOG_FORMAT", "")
	var stderr, sink bytes.Buffer
	logger := mustLogger(t, observability.Options{Writer: io.MultiWriter(&stderr, &sink)})

	logger.Info("hello", "k", "v")

	for name, buf := range map[string]*bytes.Buffer{"first": &stderr, "second": &sink} {
		lines := jsonLines(t, buf.String())
		if len(lines) != 1 || lines[0]["msg"] != "hello" || lines[0]["k"] != "v" || lines[0]["service"] != "svc" {
			t.Errorf("%s writer got %v", name, lines)
		}
	}
}

func TestNewLoggerWith_ExplicitLevelBeatsEnv(t *testing.T) {
	t.Setenv("LOG_LEVEL", "debug")
	var buf bytes.Buffer
	logger := mustLogger(t, observability.Options{Writer: &buf, Level: slog.LevelWarn})
	logger.Debug("d")
	logger.Info("i")
	logger.Warn("w")
	if lines := jsonLines(t, buf.String()); len(lines) != 1 || lines[0]["msg"] != "w" {
		t.Fatalf("LOG_LEVEL=debug must not lower an explicit warn: %v", lines)
	}

	t.Setenv("LOG_LEVEL", "error")
	buf.Reset()
	logger = mustLogger(t, observability.Options{Writer: &buf, Level: slog.LevelDebug})
	logger.Debug("d")
	if lines := jsonLines(t, buf.String()); len(lines) != 1 {
		t.Fatalf("LOG_LEVEL=error must not raise an explicit debug: %v", lines)
	}
}

func TestNewLoggerWith_LevelVarChangesAtRuntime(t *testing.T) {
	var buf bytes.Buffer
	var level slog.LevelVar
	level.Set(slog.LevelWarn)
	logger := mustLogger(t, observability.Options{Writer: &buf, Level: &level})

	logger.Info("dropped")
	level.Set(slog.LevelInfo)
	logger.Info("kept")

	if lines := jsonLines(t, buf.String()); len(lines) != 1 || lines[0]["msg"] != "kept" {
		t.Fatalf("got %v", lines)
	}
}

func TestNewLoggerWith_ExplicitFormatBeatsEnv(t *testing.T) {
	t.Setenv("LOG_FORMAT", "text")
	var buf bytes.Buffer
	mustLogger(t, observability.Options{Writer: &buf, Format: "JSON"}).Info("hello")
	if !strings.HasPrefix(buf.String(), "{") {
		t.Fatalf("Format JSON must beat LOG_FORMAT=text: %q", buf.String())
	}

	t.Setenv("LOG_FORMAT", "json")
	buf.Reset()
	mustLogger(t, observability.Options{Writer: &buf, Format: "Text"}).Info("hello")
	if !strings.HasPrefix(buf.String(), "time=") || !strings.Contains(buf.String(), "service=svc") {
		t.Fatalf("Format Text must beat LOG_FORMAT=json: %q", buf.String())
	}
}

func TestNewLoggerWith_UnknownFormatIsError(t *testing.T) {
	_, err := observability.NewLoggerWith("svc", observability.Options{Format: "xml"})
	if err == nil || !strings.Contains(err.Error(), `unknown log format "xml"`) {
		t.Fatalf("got %v", err)
	}

	// the environment keeps NewLogger's lenient fallback to json
	t.Setenv("LOG_FORMAT", "xml")
	var buf bytes.Buffer
	mustLogger(t, observability.Options{Writer: &buf}).Info("hello")
	if !strings.HasPrefix(buf.String(), "{") {
		t.Fatalf("unknown LOG_FORMAT must fall back to json: %q", buf.String())
	}
}

// captureStdout runs fn with os.Stdout redirected and returns what it wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	w.Close() //nolint:errcheck
	return <-done
}

func TestNewLoggerWith_ZeroOptionsIsNewLogger(t *testing.T) {
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FORMAT", "")

	viaNew := captureStdout(t, func() { observability.NewLogger("svc").Debug("hello", "n", 1) })
	viaWith := captureStdout(t, func() { mustLogger(t, observability.Options{}).Debug("hello", "n", 1) })

	a, b := jsonLines(t, viaNew), jsonLines(t, viaWith)
	if len(a) != 1 || !reflect.DeepEqual(a, b) {
		t.Fatalf("NewLogger wrote %v, zero Options wrote %v", a, b)
	}
	if a[0]["service"] != "svc" || a[0]["level"] != "DEBUG" {
		t.Fatalf("LOG_LEVEL=debug must reach stdout with the service attribute: %v", a)
	}

	t.Setenv("LOG_LEVEL", "warn")
	t.Setenv("LOG_FORMAT", "TEXT")
	out := captureStdout(t, func() {
		logger := mustLogger(t, observability.Options{})
		logger.Info("dropped")
		logger.Warn("kept")
	})
	if strings.Contains(out, "dropped") || !strings.Contains(out, "msg=kept") || !strings.Contains(out, "service=svc") {
		t.Fatalf("zero Options must follow LOG_LEVEL/LOG_FORMAT on stdout: %q", out)
	}
}
