// Package telemetry emits OpenTelemetry-shaped spans using gen_ai.* semantic
// conventions. It exports ONLY to local sinks — the SQLite store and a local OTLP
// JSON-lines file — and never makes a network connection. This is the single stream
// every observability, eval, and monitoring view reads.
package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/faraday-stack/faraday/internal/store"
)

// Span kinds (OpenTelemetry).
const (
	KindInternal = "INTERNAL"
	KindClient   = "CLIENT"
	KindServer   = "SERVER"
)

// gen_ai.* semantic-convention attribute keys (Development conventions, pinned here
// behind this adapter so upstream renames are a one-file change).
const (
	AttrOperationName  = "gen_ai.operation.name"
	AttrProviderName   = "gen_ai.provider.name"
	AttrRequestModel   = "gen_ai.request.model"
	AttrResponseModel  = "gen_ai.response.model"
	AttrInputTokens    = "gen_ai.usage.input_tokens"
	AttrOutputTokens   = "gen_ai.usage.output_tokens"
	AttrFinishReasons  = "gen_ai.response.finish_reasons"
	AttrToolName       = "gen_ai.tool.name"
	AttrToolCallID     = "gen_ai.tool.call.id"
	AttrToolType       = "gen_ai.tool.type"
	AttrAgentName      = "gen_ai.agent.name"
	AttrAgentID        = "gen_ai.agent.id"
	AttrConversationID = "gen_ai.conversation.id"
	AttrInputMessages  = "gen_ai.input.messages"
	AttrOutputMessages = "gen_ai.output.messages"

	// Faraday-specific attributes (namespaced to avoid clashing with gen_ai.*).
	AttrChosenBackend = "faraday.route.backend"
	AttrFallbackDepth = "faraday.route.fallback_depth"
	AttrRouteReason   = "faraday.route.reason"
	AttrSandboxID     = "faraday.sandbox.id"
	AttrExitStatus    = "faraday.exec.exit_status"
	AttrEvalSuite     = "faraday.eval.suite"
	AttrEvalMetric    = "faraday.eval.metric"
	AttrEvalScore     = "faraday.eval.score"
	AttrRepairIter    = "faraday.agent.repair_iteration"
)

// Tracer creates and records spans to local sinks only.
type Tracer struct {
	st             *store.Store
	captureContent bool
	mu             sync.Mutex
	file           *os.File
	resource       map[string]any
}

// Options configures a Tracer.
type Options struct {
	FilePath       string // OTLP JSON-lines sink; "" disables the file sink
	CaptureContent bool
	Resource       map[string]any
}

// New creates a Tracer. It opens the file sink if requested; any failure to open the
// file is non-fatal (the store sink still works).
func New(st *store.Store, opts Options) *Tracer {
	t := &Tracer{st: st, captureContent: opts.CaptureContent, resource: opts.Resource}
	if t.resource == nil {
		t.resource = map[string]any{"service.name": "faraday"}
	}
	if opts.FilePath != "" {
		if dir := filepath.Dir(opts.FilePath); dir != "" {
			_ = os.MkdirAll(dir, 0o755)
		}
		if f, err := os.OpenFile(opts.FilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			t.file = f
		}
	}
	return t
}

// Close flushes the file sink.
func (t *Tracer) Close() error {
	if t.file != nil {
		return t.file.Close()
	}
	return nil
}

// CaptureContent reports whether prompt/completion content may be recorded.
func (t *Tracer) CaptureContent() bool { return t.captureContent }

// Span is an in-flight span.
type Span struct {
	t        *Tracer
	TraceID  string
	SpanID   string
	ParentID string
	Name     string
	Kind     string
	start    time.Time
	attrs    map[string]any
	status   string
	ended    bool
}

type ctxKey struct{}

// Start begins a span, parented to any span already in ctx, and returns the child ctx.
func (t *Tracer) Start(ctx context.Context, name, kind string) (context.Context, *Span) {
	sp := &Span{t: t, Name: name, Kind: kind, start: time.Now(), attrs: map[string]any{}}
	if parent := SpanFromContext(ctx); parent != nil {
		sp.TraceID = parent.TraceID
		sp.ParentID = parent.SpanID
	} else {
		sp.TraceID = newID(16)
	}
	sp.SpanID = newID(8)
	return context.WithValue(ctx, ctxKey{}, sp), sp
}

// SpanFromContext returns the active span or nil.
func SpanFromContext(ctx context.Context) *Span {
	if ctx == nil {
		return nil
	}
	if sp, ok := ctx.Value(ctxKey{}).(*Span); ok {
		return sp
	}
	return nil
}

// SetAttr sets one attribute.
func (s *Span) SetAttr(k string, v any) *Span {
	if s == nil {
		return s
	}
	s.attrs[k] = v
	return s
}

// SetAttrs merges attributes.
func (s *Span) SetAttrs(m map[string]any) *Span {
	if s == nil {
		return s
	}
	for k, v := range m {
		s.attrs[k] = v
	}
	return s
}

// SetStatus records a status string (e.g. "ok", "error: ...").
func (s *Span) SetStatus(status string) *Span {
	if s == nil {
		return s
	}
	s.status = status
	return s
}

// SetContent records messages only when content capture is enabled.
func (s *Span) SetContent(inputKey string, v any) *Span {
	if s == nil || !s.t.captureContent {
		return s
	}
	s.attrs[inputKey] = v
	return s
}

// End finalizes the span and writes it to all local sinks.
func (s *Span) End() {
	if s == nil || s.ended {
		return
	}
	s.ended = true
	dur := time.Since(s.start)
	rec := store.Span{
		SpanID: s.SpanID, TraceID: s.TraceID, ParentID: s.ParentID,
		Name: s.Name, Kind: s.Kind,
		StartUnixNano: s.start.UnixNano(), DurationNs: dur.Nanoseconds(),
		Status: s.status, Attrs: s.attrs, Resource: s.t.resource,
	}
	if s.t.st != nil {
		_ = s.t.st.InsertSpan(rec)
	}
	if s.t.file != nil {
		s.t.mu.Lock()
		if b, err := json.Marshal(rec); err == nil {
			s.t.file.Write(append(b, '\n'))
		}
		s.t.mu.Unlock()
	}
}

// IngestRemote stores a span produced by an ML-plane (Python) service over the local
// ingest endpoint. It is the only way external spans enter the stream, and it too is
// local-only.
func (t *Tracer) IngestRemote(rec store.Span) error {
	if rec.SpanID == "" {
		rec.SpanID = newID(8)
	}
	if rec.Resource == nil {
		rec.Resource = t.resource
	}
	if t.st != nil {
		if err := t.st.InsertSpan(rec); err != nil {
			return err
		}
	}
	if t.file != nil {
		t.mu.Lock()
		if b, err := json.Marshal(rec); err == nil {
			t.file.Write(append(b, '\n'))
		}
		t.mu.Unlock()
	}
	return nil
}

func newID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
