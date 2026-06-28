package evidence

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"

	"github.com/faraday-stack/faraday/internal/config"
	"github.com/faraday-stack/faraday/internal/store"
)

func TestBundleBuildVerifyAndTamper(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_, _ = st.AppendAudit("user", "eval.run", "s", "passed=true")
	_ = st.InsertEvalRun(store.EvalRun{ID: "r1", Suite: "s", Kind: "code_passk", StartedUnixNano: 1, DatasetDigest: "abc", Metrics: map[string]float64{"pass_rate": 1}, Passed: true})

	cfg, _ := config.Parse([]byte("version: faraday/v1\nmodels:\n  coder: {source: m, backend: mock}\nagents:\n  fixer: {model: coder}\n"))
	b := NewBuilder(st, cfg)
	out := filepath.Join(dir, "evidence.tar.zst")
	if _, err := b.Build(out, filepath.Join(dir, "k.key"), "Test Identity"); err != nil {
		t.Fatal(err)
	}

	res, err := Verify(out)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("fresh bundle should verify: %+v", res.Problems)
	}

	// Tamper: rewrite an inner file's content and repack, leaving the manifest digests
	// unchanged. Verification MUST fail (digest mismatch).
	tampered := filepath.Join(dir, "tampered.tar.zst")
	tamperInnerFile(t, out, tampered, "eval_report.json", []byte(`{"hacked":true}`))
	res2, err := Verify(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if res2.OK {
		t.Fatal("tampered bundle must NOT verify")
	}
}

func tamperInnerFile(t *testing.T, in, out, target string, newContent []byte) {
	t.Helper()
	files := readAll(t, in)
	files[target] = newContent // manifest still claims the old digest
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw, _ := zstd.NewWriter(f)
	defer zw.Close()
	tw := tar.NewWriter(zw)
	defer tw.Close()
	for name, content := range files {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))})
		_, _ = tw.Write(content)
	}
}

func readAll(t *testing.T, path string) map[string][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, _ := zstd.NewReader(f)
	defer zr.Close()
	tr := tar.NewReader(zr)
	out := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(tr)
		out[hdr.Name] = buf.Bytes()
	}
	return out
}
