// Package daemon runs the faraday engine as a local server. It serves the control API,
// the OpenAI-compatible /v1 surface, and a localhost span-ingest endpoint over a Unix
// socket (and an optional localhost TCP address). It never makes an outbound connection.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/faraday-stack/faraday/internal/core"
	"github.com/faraday-stack/faraday/internal/store"
	"github.com/faraday-stack/faraday/internal/version"
)

// DefaultSocket is the default control socket path.
const DefaultSocket = "/tmp/faraday.sock"

// Daemon serves the engine.
type Daemon struct {
	engine     *core.Engine
	socketPath string
	tcpAddr    string
	srv        *http.Server
}

// Options configures the daemon.
type Options struct {
	SocketPath string
	TCPAddr    string // optional localhost addr for /v1 (e.g. 127.0.0.1:8080); "" disables
}

// New constructs a daemon.
func New(engine *core.Engine, opts Options) *Daemon {
	if opts.SocketPath == "" {
		opts.SocketPath = DefaultSocket
	}
	d := &Daemon{engine: engine, socketPath: opts.SocketPath, tcpAddr: opts.TCPAddr}
	d.srv = &http.Server{Handler: d.routes()}
	return d
}

func (d *Daemon) routes() http.Handler {
	mux := http.NewServeMux()
	d.engine.InferenceHandler().Register(mux) // /v1/*

	mux.HandleFunc("GET /api/version", d.handleVersion)
	mux.HandleFunc("GET /api/health", d.handleHealth)
	mux.HandleFunc("POST /api/run/agent", d.handleRunAgent)
	mux.HandleFunc("POST /api/eval/run", d.handleEvalRun)
	mux.HandleFunc("GET /api/trace/list", d.handleTraceList)
	mux.HandleFunc("GET /api/trace/last", d.handleTraceLast)
	mux.HandleFunc("GET /api/trace/show", d.handleTraceShow)
	mux.HandleFunc("POST /api/evidence/bundle", d.handleEvidenceBundle)
	mux.HandleFunc("POST /api/spans", d.handleSpanIngest) // OTLP-style ingest from ML-plane
	mux.HandleFunc("GET /api/team/summary", d.handleTeamSummary)
	return mux
}

// Serve listens and serves until ctx is cancelled.
func (d *Daemon) Serve(ctx context.Context) error {
	_ = os.Remove(d.socketPath)
	ln, err := net.Listen("unix", d.socketPath)
	if err != nil {
		return err
	}
	// Restrict the socket to owner/group only.
	_ = os.Chmod(d.socketPath, 0o660)

	errCh := make(chan error, 2)
	go func() { errCh <- d.srv.Serve(ln) }()

	if d.tcpAddr != "" {
		tln, terr := net.Listen("tcp", d.tcpAddr)
		if terr != nil {
			return terr
		}
		go func() { errCh <- d.srv.Serve(tln) }()
	}

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = d.srv.Shutdown(shutCtx)
		_ = os.Remove(d.socketPath)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// --- handlers ---

func (d *Daemon) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, version.Get())
}

func (d *Daemon) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"status":           "ok",
		"sandbox_driver":   d.engine.SandboxDriverName(),
		"network_isolated": d.engine.NetworkIsolated(),
	})
}

func (d *Daemon) handleRunAgent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Agent string `json:"agent"`
		Task  string `json:"task"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	res, err := d.engine.RunAgent(r.Context(), req.Agent, req.Task)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, res)
}

func (d *Daemon) handleEvalRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Suite string `json:"suite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	out, err := d.engine.EvalRun(r.Context(), req.Suite)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, out)
}

func (d *Daemon) handleTraceList(w http.ResponseWriter, r *http.Request) {
	traces, err := d.engine.Store().ListTraces(20)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"traces": traces})
}

func (d *Daemon) handleTraceLast(w http.ResponseWriter, r *http.Request) {
	id, err := d.engine.Store().LastTraceID()
	if err != nil || id == "" {
		writeErr(w, 404, "no traces recorded")
		return
	}
	d.writeTrace(w, id)
}

func (d *Daemon) handleTraceShow(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeErr(w, 400, "id required")
		return
	}
	d.writeTrace(w, id)
}

func (d *Daemon) writeTrace(w http.ResponseWriter, id string) {
	spans, err := d.engine.Store().GetTrace(id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"trace_id": id, "spans": spans})
}

func (d *Daemon) handleEvidenceBundle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Out      string `json:"out"`
		Identity string `json:"identity"`
		Key      string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if req.Out == "" {
		req.Out = "evidence.tar.zst"
	}
	m, err := d.engine.BuildEvidence(req.Out, req.Key, req.Identity)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"out": req.Out, "manifest": m})
}

func (d *Daemon) handleTeamSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := d.engine.TeamSummary()
	if err != nil {
		writeErr(w, 402, err.Error()) // 402 Payment Required: paid feature
		return
	}
	writeJSON(w, 200, summary)
}

func (d *Daemon) handleSpanIngest(w http.ResponseWriter, r *http.Request) {
	var sp store.Span
	if err := json.NewDecoder(r.Body).Decode(&sp); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := d.engine.Tracer().IngestRemote(sp); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
}
