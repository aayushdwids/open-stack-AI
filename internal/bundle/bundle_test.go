package bundle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateVerifyInstallRoundtrip(t *testing.T) {
	dir := t.TempDir()
	// Two artifacts to bundle.
	bin := filepath.Join(dir, "faraday")
	cfg := filepath.Join(dir, "faraday.yaml")
	_ = os.WriteFile(bin, []byte("FAKE-BINARY-BYTES"), 0o755)
	_ = os.WriteFile(cfg, []byte("version: faraday/v1\n"), 0o644)

	out := filepath.Join(dir, "b.tar.zst")
	lock, err := Create(out, filepath.Join(dir, "k.key"), "ACME", []Source{
		{Name: "faraday", Path: bin, Kind: "binary"},
		{Name: "faraday.yaml", Path: cfg, Kind: "config"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(lock.Artifacts))
	}

	vr, err := Verify(out)
	if err != nil || !vr.OK {
		t.Fatalf("fresh bundle should verify: %+v %v", vr.Problems, err)
	}

	// Install reproduces the identical files.
	destA := filepath.Join(dir, "installA")
	rep, err := Install(out, destA)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Verified || len(rep.Installed) != 2 {
		t.Fatalf("install report wrong: %+v", rep)
	}
	got, _ := os.ReadFile(filepath.Join(destA, "faraday"))
	if string(got) != "FAKE-BINARY-BYTES" {
		t.Fatal("installed binary content differs")
	}
}

func TestTamperedBundleRefusesInstall(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(f, []byte("original"), 0o644)
	out := filepath.Join(dir, "b.tar.zst")
	if _, err := Create(out, filepath.Join(dir, "k.key"), "X", []Source{{Name: "a.txt", Path: f, Kind: "other"}}); err != nil {
		t.Fatal(err)
	}
	// Repack with a tampered inner file but the original lock/manifest.
	files, _ := readTarZst(out)
	files["a.txt"] = []byte("TAMPERED")
	tampered := filepath.Join(dir, "t.tar.zst")
	if err := writeTarZst(tampered, files); err != nil {
		t.Fatal(err)
	}
	if vr, _ := Verify(tampered); vr.OK {
		t.Fatal("tampered bundle must not verify")
	}
	if _, err := Install(tampered, filepath.Join(dir, "dest")); err == nil {
		t.Fatal("tampered bundle must refuse install")
	}
}
