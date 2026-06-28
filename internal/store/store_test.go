package store

import (
	"path/filepath"
	"testing"
)

func open(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestSpanRoundtripAndTrace(t *testing.T) {
	st := open(t)
	root := Span{SpanID: "s1", TraceID: "t1", Name: "invoke_agent fixer", Kind: "INTERNAL", StartUnixNano: 100, DurationNs: 50}
	child := Span{SpanID: "s2", TraceID: "t1", ParentID: "s1", Name: "chat coder", Kind: "CLIENT", StartUnixNano: 110, DurationNs: 10, Attrs: map[string]any{"gen_ai.usage.output_tokens": 5}}
	if err := st.InsertSpan(root); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertSpan(child); err != nil {
		t.Fatal(err)
	}
	last, _ := st.LastTraceID()
	if last != "t1" {
		t.Fatalf("last trace = %q", last)
	}
	spans, _ := st.GetTrace("t1")
	if len(spans) != 2 {
		t.Fatalf("got %d spans", len(spans))
	}
	traces, _ := st.ListTraces(10)
	if len(traces) != 1 || traces[0].SpanCount != 2 || traces[0].RootName != "invoke_agent fixer" {
		t.Fatalf("trace summary wrong: %+v", traces)
	}
}

func TestAuditChainDetectsTampering(t *testing.T) {
	st := open(t)
	for i := 0; i < 3; i++ {
		if _, err := st.AppendAudit("user", "action", "tgt", "detail"); err != nil {
			t.Fatal(err)
		}
	}
	if broken, _ := st.VerifyAuditChain(); broken != 0 {
		t.Fatalf("fresh chain should be intact, broke at %d", broken)
	}
	// Tamper with the middle entry's detail directly in the DB.
	if _, err := st.DB().Exec(`UPDATE audit_log SET detail='HACKED' WHERE seq=2`); err != nil {
		t.Fatal(err)
	}
	broken, _ := st.VerifyAuditChain()
	if broken == 0 {
		t.Fatal("tampering not detected")
	}
}

func TestEvalRunPersistence(t *testing.T) {
	st := open(t)
	run := EvalRun{ID: "r1", Suite: "s", Kind: "code_passk", StartedUnixNano: 1, Metrics: map[string]float64{"pass_rate": 1.0}, Passed: true}
	if err := st.InsertEvalRun(run); err != nil {
		t.Fatal(err)
	}
	got, _ := st.LastEvalRun("s")
	if got == nil || got.Metrics["pass_rate"] != 1.0 {
		t.Fatalf("eval run not persisted: %+v", got)
	}
}
