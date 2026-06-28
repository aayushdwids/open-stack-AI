// Package store is the pure-Go SQLite persistence layer for faraday: telemetry spans,
// traces, eval runs/results, the model registry, a hash-chained audit log, and a kv table.
// It uses modernc.org/sqlite (no cgo) to preserve the static-binary guarantee.
package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store wraps the SQLite database.
type Store struct {
	db  *sql.DB
	mu  sync.Mutex // serializes audit-chain appends
}

// Open opens (creating if needed) the database at path and applies migrations.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1) // modernc sqlite is happiest single-writer
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the underlying handle for advanced callers (tests).
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS spans (
			span_id TEXT PRIMARY KEY,
			trace_id TEXT NOT NULL,
			parent_id TEXT,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			start_unix_nano INTEGER NOT NULL,
			duration_ns INTEGER NOT NULL,
			status TEXT,
			attrs TEXT,
			resource TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_spans_trace ON spans(trace_id)`,
		`CREATE INDEX IF NOT EXISTS idx_spans_start ON spans(start_unix_nano)`,
		`CREATE TABLE IF NOT EXISTS traces (
			trace_id TEXT PRIMARY KEY,
			root_name TEXT,
			start_unix_nano INTEGER NOT NULL,
			duration_ns INTEGER
		)`,
		`CREATE TABLE IF NOT EXISTS eval_runs (
			id TEXT PRIMARY KEY,
			suite TEXT NOT NULL,
			kind TEXT NOT NULL,
			started_unix_nano INTEGER NOT NULL,
			dataset_digest TEXT,
			seed INTEGER,
			model TEXT,
			metrics TEXT,
			passed INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_eval_runs_suite ON eval_runs(suite, started_unix_nano)`,
		`CREATE TABLE IF NOT EXISTS eval_results (
			run_id TEXT NOT NULL,
			case_id TEXT NOT NULL,
			passed INTEGER,
			score REAL,
			detail TEXT,
			PRIMARY KEY(run_id, case_id)
		)`,
		`CREATE TABLE IF NOT EXISTS registry (
			name TEXT PRIMARY KEY,
			kind TEXT,
			source TEXT,
			digest TEXT,
			meta TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS audit_log (
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			ts_unix_nano INTEGER NOT NULL,
			actor TEXT,
			action TEXT NOT NULL,
			target TEXT,
			detail TEXT,
			prev_hash TEXT NOT NULL,
			hash TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS kv (k TEXT PRIMARY KEY, v TEXT)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("migrate: %w (%s)", err, q)
		}
	}
	return nil
}

// ---- Spans ----

// Span is a stored telemetry span (OpenTelemetry-shaped, gen_ai.* attributes).
type Span struct {
	SpanID        string         `json:"span_id"`
	TraceID       string         `json:"trace_id"`
	ParentID      string         `json:"parent_id,omitempty"`
	Name          string         `json:"name"`
	Kind          string         `json:"kind"`
	StartUnixNano int64          `json:"start_unix_nano"`
	DurationNs    int64          `json:"duration_ns"`
	Status        string         `json:"status,omitempty"`
	Attrs         map[string]any `json:"attrs,omitempty"`
	Resource      map[string]any `json:"resource,omitempty"`
}

// InsertSpan persists one span and upserts its trace summary.
func (s *Store) InsertSpan(sp Span) error {
	attrs, _ := json.Marshal(sp.Attrs)
	res, _ := json.Marshal(sp.Resource)
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO spans(span_id,trace_id,parent_id,name,kind,start_unix_nano,duration_ns,status,attrs,resource)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		sp.SpanID, sp.TraceID, nullStr(sp.ParentID), sp.Name, sp.Kind, sp.StartUnixNano, sp.DurationNs, nullStr(sp.Status), string(attrs), string(res))
	if err != nil {
		return err
	}
	// Maintain trace summary: root span is the one with no parent.
	if sp.ParentID == "" {
		_, err = s.db.Exec(
			`INSERT INTO traces(trace_id,root_name,start_unix_nano,duration_ns) VALUES(?,?,?,?)
			 ON CONFLICT(trace_id) DO UPDATE SET root_name=excluded.root_name, start_unix_nano=excluded.start_unix_nano, duration_ns=excluded.duration_ns`,
			sp.TraceID, sp.Name, sp.StartUnixNano, sp.DurationNs)
	} else {
		_, err = s.db.Exec(
			`INSERT INTO traces(trace_id,root_name,start_unix_nano,duration_ns) VALUES(?,?,?,?)
			 ON CONFLICT(trace_id) DO NOTHING`,
			sp.TraceID, "", sp.StartUnixNano, 0)
	}
	return err
}

// TraceSummary is one row in the trace list.
type TraceSummary struct {
	TraceID       string `json:"trace_id"`
	RootName      string `json:"root_name"`
	StartUnixNano int64  `json:"start_unix_nano"`
	DurationNs    int64  `json:"duration_ns"`
	SpanCount     int    `json:"span_count"`
}

// ListTraces returns recent traces, newest first.
func (s *Store) ListTraces(limit int) ([]TraceSummary, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(
		`SELECT t.trace_id, COALESCE(t.root_name,''), t.start_unix_nano, COALESCE(t.duration_ns,0),
		        (SELECT COUNT(*) FROM spans sp WHERE sp.trace_id=t.trace_id)
		 FROM traces t ORDER BY t.start_unix_nano DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TraceSummary
	for rows.Next() {
		var t TraceSummary
		if err := rows.Scan(&t.TraceID, &t.RootName, &t.StartUnixNano, &t.DurationNs, &t.SpanCount); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// LastTraceID returns the most recent trace id, or "" if none.
func (s *Store) LastTraceID() (string, error) {
	var id string
	err := s.db.QueryRow(`SELECT trace_id FROM traces ORDER BY start_unix_nano DESC LIMIT 1`).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

// GetTrace returns all spans for a trace, ordered by start time.
func (s *Store) GetTrace(traceID string) ([]Span, error) {
	rows, err := s.db.Query(
		`SELECT span_id,trace_id,COALESCE(parent_id,''),name,kind,start_unix_nano,duration_ns,COALESCE(status,''),COALESCE(attrs,''),COALESCE(resource,'')
		 FROM spans WHERE trace_id=? ORDER BY start_unix_nano ASC`, traceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Span
	for rows.Next() {
		var sp Span
		var attrs, res string
		if err := rows.Scan(&sp.SpanID, &sp.TraceID, &sp.ParentID, &sp.Name, &sp.Kind, &sp.StartUnixNano, &sp.DurationNs, &sp.Status, &attrs, &res); err != nil {
			return nil, err
		}
		if attrs != "" {
			_ = json.Unmarshal([]byte(attrs), &sp.Attrs)
		}
		if res != "" {
			_ = json.Unmarshal([]byte(res), &sp.Resource)
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// ---- Eval runs ----

// EvalRun summarizes one eval suite execution.
type EvalRun struct {
	ID              string             `json:"id"`
	Suite           string             `json:"suite"`
	Kind            string             `json:"kind"`
	StartedUnixNano int64              `json:"started_unix_nano"`
	DatasetDigest   string             `json:"dataset_digest"`
	Seed            int64              `json:"seed"`
	Model           string             `json:"model"`
	Metrics         map[string]float64 `json:"metrics"`
	Passed          bool               `json:"passed"`
}

// InsertEvalRun persists an eval run.
func (s *Store) InsertEvalRun(r EvalRun) error {
	m, _ := json.Marshal(r.Metrics)
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO eval_runs(id,suite,kind,started_unix_nano,dataset_digest,seed,model,metrics,passed)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Suite, r.Kind, r.StartedUnixNano, r.DatasetDigest, r.Seed, r.Model, string(m), boolInt(r.Passed))
	return err
}

// LastEvalRun returns the most recent run for a suite, or nil.
func (s *Store) LastEvalRun(suite string) (*EvalRun, error) {
	var r EvalRun
	var m string
	var passed int
	err := s.db.QueryRow(
		`SELECT id,suite,kind,started_unix_nano,COALESCE(dataset_digest,''),COALESCE(seed,0),COALESCE(model,''),COALESCE(metrics,''),COALESCE(passed,0)
		 FROM eval_runs WHERE suite=? ORDER BY started_unix_nano DESC LIMIT 1`, suite).
		Scan(&r.ID, &r.Suite, &r.Kind, &r.StartedUnixNano, &r.DatasetDigest, &r.Seed, &r.Model, &m, &passed)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.Passed = passed != 0
	if m != "" {
		_ = json.Unmarshal([]byte(m), &r.Metrics)
	}
	return &r, nil
}

// ListEvalRuns returns recent eval runs, newest first.
func (s *Store) ListEvalRuns(limit int) ([]EvalRun, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(
		`SELECT id,suite,kind,started_unix_nano,COALESCE(dataset_digest,''),COALESCE(seed,0),COALESCE(model,''),COALESCE(metrics,''),COALESCE(passed,0)
		 FROM eval_runs ORDER BY started_unix_nano DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EvalRun
	for rows.Next() {
		var r EvalRun
		var m string
		var passed int
		if err := rows.Scan(&r.ID, &r.Suite, &r.Kind, &r.StartedUnixNano, &r.DatasetDigest, &r.Seed, &r.Model, &m, &passed); err != nil {
			return nil, err
		}
		r.Passed = passed != 0
		if m != "" {
			_ = json.Unmarshal([]byte(m), &r.Metrics)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// EvalResultRow is one per-case result.
type EvalResultRow struct {
	RunID  string  `json:"run_id"`
	CaseID string  `json:"case_id"`
	Passed bool    `json:"passed"`
	Score  float64 `json:"score"`
	Detail string  `json:"detail"`
}

// EvalResults returns per-case results for a run.
func (s *Store) EvalResults(runID string) ([]EvalResultRow, error) {
	rows, err := s.db.Query(`SELECT run_id,case_id,COALESCE(passed,0),COALESCE(score,0),COALESCE(detail,'') FROM eval_results WHERE run_id=? ORDER BY case_id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EvalResultRow
	for rows.Next() {
		var r EvalResultRow
		var passed int
		if err := rows.Scan(&r.RunID, &r.CaseID, &passed, &r.Score, &r.Detail); err != nil {
			return nil, err
		}
		r.Passed = passed != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertEvalResult persists a per-case result.
func (s *Store) InsertEvalResult(runID, caseID string, passed bool, score float64, detail string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO eval_results(run_id,case_id,passed,score,detail) VALUES(?,?,?,?,?)`,
		runID, caseID, boolInt(passed), score, detail)
	return err
}

// ---- Audit log (hash-chained, tamper-evident) ----

// AuditEntry is one event in the chain.
type AuditEntry struct {
	Seq        int64  `json:"seq"`
	TsUnixNano int64  `json:"ts_unix_nano"`
	Actor      string `json:"actor"`
	Action     string `json:"action"`
	Target     string `json:"target"`
	Detail     string `json:"detail"`
	PrevHash   string `json:"prev_hash"`
	Hash       string `json:"hash"`
}

// AppendAudit appends an event, chaining its hash to the previous entry.
func (s *Store) AppendAudit(actor, action, target, detail string) (AuditEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var prev string
	err := s.db.QueryRow(`SELECT hash FROM audit_log ORDER BY seq DESC LIMIT 1`).Scan(&prev)
	if err == sql.ErrNoRows {
		prev = "GENESIS"
	} else if err != nil {
		return AuditEntry{}, err
	}
	e := AuditEntry{
		TsUnixNano: time.Now().UnixNano(),
		Actor:      actor, Action: action, Target: target, Detail: detail,
		PrevHash: prev,
	}
	e.Hash = auditHash(e)
	r, err := s.db.Exec(
		`INSERT INTO audit_log(ts_unix_nano,actor,action,target,detail,prev_hash,hash) VALUES(?,?,?,?,?,?,?)`,
		e.TsUnixNano, e.Actor, e.Action, e.Target, e.Detail, e.PrevHash, e.Hash)
	if err != nil {
		return AuditEntry{}, err
	}
	e.Seq, _ = r.LastInsertId()
	return e, nil
}

// AuditEntries returns the full chain in order.
func (s *Store) AuditEntries() ([]AuditEntry, error) {
	rows, err := s.db.Query(`SELECT seq,ts_unix_nano,actor,action,target,detail,prev_hash,hash FROM audit_log ORDER BY seq ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.Seq, &e.TsUnixNano, &e.Actor, &e.Action, &e.Target, &e.Detail, &e.PrevHash, &e.Hash); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// VerifyAuditChain recomputes the chain and reports the first broken seq (0 = intact).
func (s *Store) VerifyAuditChain() (int64, error) {
	entries, err := s.AuditEntries()
	if err != nil {
		return 0, err
	}
	prev := "GENESIS"
	for _, e := range entries {
		if e.PrevHash != prev {
			return e.Seq, nil
		}
		want := auditHash(e)
		if want != e.Hash {
			return e.Seq, nil
		}
		prev = e.Hash
	}
	return 0, nil
}

func auditHash(e AuditEntry) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d\x00%s\x00%s\x00%s\x00%s\x00%s", e.TsUnixNano, e.Actor, e.Action, e.Target, e.Detail, e.PrevHash)
	return hex.EncodeToString(h.Sum(nil))
}

// ---- KV + registry ----

// KVSet stores a key/value.
func (s *Store) KVSet(k, v string) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO kv(k,v) VALUES(?,?)`, k, v)
	return err
}

// KVGet reads a key.
func (s *Store) KVGet(k string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT v FROM kv WHERE k=?`, k).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return v, err == nil, err
}

// RegisterModel records a model in the registry.
func (s *Store) RegisterModel(name, kind, source, digest string, meta map[string]any) error {
	m, _ := json.Marshal(meta)
	_, err := s.db.Exec(`INSERT OR REPLACE INTO registry(name,kind,source,digest,meta) VALUES(?,?,?,?,?)`,
		name, kind, source, digest, string(m))
	return err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
