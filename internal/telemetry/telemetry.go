// Package telemetry is an opt-in, GDPR-conscious usage/performance reporter.
// It is a safe no-op unless both TOKENSAVER_TELEMETRY is truthy and
// TOKENSAVER_TELEMETRY_ENDPOINT is set, so callers can construct and use a
// Reporter unconditionally without branching on configuration. When enabled,
// events carry no PII: no source URLs/paths, no error text, no document
// content — only coarse, category-level fields plus a random installation id
// that is not derived from the host, user, or network identity.
package telemetry

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	envEnabled  = "TOKENSAVER_TELEMETRY"
	envEndpoint = "TOKENSAVER_TELEMETRY_ENDPOINT"

	flushInterval = 30 * time.Second // send a batch at least this often
	batchSize     = 20               // or as soon as a batch reaches this size
	bufferSize    = 256              // events queued between flushes; excess is dropped
	configDirName = "tokensaver"
	idFileName    = "telemetry_id"
)

// CloseTimeout is how long Close will wait for a final flush before giving
// up. Callers that don't already have a bounded shutdown context can use it
// to build one: `ctx, cancel := context.WithTimeout(context.Background(),
// telemetry.CloseTimeout)`.
const CloseTimeout = 3 * time.Second

// sendTimeout bounds a single HTTP POST to the collector. It is derived from
// (and kept below) CloseTimeout, not set independently, so a shutdown flush
// can actually finish — or be canceled — within the time Close is willing to
// wait, instead of the two silently drifting apart.
const sendTimeout = CloseTimeout - time.Second

// Config is read from the environment; it is never set via MCP tool
// parameters (those are sent to the model on every request and must stay
// cheap and stable).
type Config struct {
	Enabled  bool
	Endpoint string
}

// ConfigFromEnv reads TOKENSAVER_TELEMETRY (opt-in; "1"/"true"/… — anything
// else, including unset, disables it) and TOKENSAVER_TELEMETRY_ENDPOINT (the
// collector URL; empty disables sending even if opted in, since there is
// nowhere to send to).
func ConfigFromEnv() Config {
	enabled, _ := strconv.ParseBool(strings.TrimSpace(os.Getenv(envEnabled)))
	return Config{
		Enabled:  enabled,
		Endpoint: strings.TrimSpace(os.Getenv(envEndpoint)),
	}
}

// Event is one tool call's usage and performance summary. Every field is
// coarse or categorical: nothing here identifies the user, the content they
// read, or where it came from.
type Event struct {
	Tool          string        `json:"tool"`           // "read" or "read_json"
	Kind          string        `json:"kind,omitempty"` // source.Kind, e.g. "html", "pdf", "json"
	Success       bool          `json:"success"`
	ErrorCategory ErrorCategory `json:"error_category,omitempty"`
	DurationMS    int64         `json:"duration_ms"`
	OutChars      int           `json:"out_chars"`
	Timestamp     time.Time     `json:"timestamp"`
	Version       string        `json:"version"`
	InstallID     string        `json:"install_id"`
}

// Reporter batches Events and POSTs them to Config.Endpoint as JSON. The zero
// value and a disabled/unconfigured Reporter are both safe, inert no-ops:
// Record and Close never block or fail the caller.
type Reporter struct {
	enabled   bool
	endpoint  string
	version   string
	installID string
	logger    *slog.Logger
	client    *http.Client

	events    chan Event
	stopCh    chan context.Context // buffered(1); Close sends its ctx once
	done      chan struct{}
	closeOnce sync.Once
}

// New builds a Reporter. logger may be nil. When cfg is disabled or has no
// endpoint, the returned Reporter is a no-op: Record and Close are cheap and
// safe to call unconditionally.
func New(cfg Config, version string, logger *slog.Logger) *Reporter {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	r := &Reporter{
		enabled:  cfg.Enabled && cfg.Endpoint != "",
		endpoint: cfg.Endpoint,
		version:  version,
		logger:   logger,
	}
	if !r.enabled {
		return r
	}
	r.installID = loadOrCreateInstallID(logger)
	r.client = &http.Client{Timeout: sendTimeout}
	r.events = make(chan Event, bufferSize)
	r.stopCh = make(chan context.Context, 1)
	r.done = make(chan struct{})
	go r.run()
	return r
}

// Record queues e for sending. It never blocks: if the internal buffer is
// full (the collector is slow or unreachable) the event is dropped.
func (r *Reporter) Record(e Event) {
	if r == nil || !r.enabled {
		return
	}
	e.Timestamp = time.Now().UTC()
	e.Version = r.version
	e.InstallID = r.installID
	select {
	case r.events <- e:
	default:
		r.logger.Debug("telemetry: buffer full, dropping event")
	}
}

// Close gives the Reporter one bounded-time chance to flush queued events,
// then returns. It never hangs process shutdown, and is safe to call more
// than once or on a disabled Reporter. The in-flight request (if any) is
// itself canceled once ctx expires, rather than left to run past the point
// Close gave up waiting on it.
func (r *Reporter) Close(ctx context.Context) {
	if r == nil || !r.enabled {
		return
	}
	r.closeOnce.Do(func() { r.stopCh <- ctx })
	select {
	case <-r.done:
	case <-ctx.Done():
	}
}

func (r *Reporter) run() {
	defer close(r.done)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	var batch []Event
	for {
		select {
		case e := <-r.events:
			batch = append(batch, e)
			if len(batch) >= batchSize {
				batch = r.flushNow(batch)
			}
		case <-ticker.C:
			batch = r.flushNow(batch)
		case stopCtx := <-r.stopCh:
			r.drain(&batch)
			// Bounded by both the caller's deadline and sendTimeout,
			// whichever is sooner: a shutdown flush never outlives the
			// window Close() is willing to wait for it.
			sendCtx, cancel := context.WithTimeout(stopCtx, sendTimeout)
			r.flush(sendCtx, batch)
			cancel()
			return
		}
	}
}

// flushNow sends batch with a fresh sendTimeout-bounded context; used by the
// regular (non-shutdown) size/interval triggers.
func (r *Reporter) flushNow(batch []Event) []Event {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	return r.flush(ctx, batch)
}

// drain grabs whatever is already queued without blocking, so a shutdown
// flush includes events sent just before Close.
func (r *Reporter) drain(batch *[]Event) {
	for {
		select {
		case e := <-r.events:
			*batch = append(*batch, e)
		default:
			return
		}
	}
}

func (r *Reporter) flush(ctx context.Context, batch []Event) []Event {
	if len(batch) == 0 {
		return batch
	}
	body, err := json.Marshal(batch)
	if err != nil {
		r.logger.Debug("telemetry: marshal failed", "err", err)
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		r.logger.Debug("telemetry: build request failed", "err", err)
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		r.logger.Debug("telemetry: send failed", "err", err)
		return nil
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		r.logger.Debug("telemetry: collector rejected batch", "status", resp.StatusCode)
	}
	return nil
}

// loadOrCreateInstallID returns a random id stable across runs, persisted
// under os.UserConfigDir(). Any failure to read/create/write it falls back to
// an in-memory-only id for this process rather than erroring out.
func loadOrCreateInstallID(logger *slog.Logger) string {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		logger.Debug("telemetry: no user config dir, using in-memory id", "err", err)
		return randomID()
	}
	dir = filepath.Join(dir, configDirName)
	path := filepath.Join(dir, idFileName)
	if data, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id
		}
	}
	id := randomID()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Debug("telemetry: could not create config dir, using in-memory id", "err", err)
		return id
	}
	if err := os.WriteFile(path, []byte(id), 0o600); err != nil {
		logger.Debug("telemetry: could not persist install id, using in-memory id", "err", err)
	}
	return id
}

// randomID is not derived from hostname, username, MAC address, or any other
// host/user identity — it is purely random and anonymous.
func randomID() string {
	var b [16]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
