// Package bundle implements the air-gap delivery spine: `create` (online) packs artifacts
// into a signed, digest-pinned bundle; `install` (offline) verifies it with material
// carried inside the bundle and reproduces the identical file set. This is the
// generalized Zarf/Hauler pattern — build a content-addressed archive, carry it on a USB
// stick, verify and unpack offline with zero network access.
package bundle

import (
	"archive/tar"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/klauspost/compress/zstd"
)

// lockName and manifestName are reserved entries inside the bundle.
const (
	lockName     = "faraday.lock.json"
	manifestName = "bundle.manifest.json"
)

// Artifact pins one file by digest (the reproducibility key — never a tag).
type Artifact struct {
	Digest string `json:"digest"` // sha256:<hex>
	Size   int64  `json:"size"`
	Kind   string `json:"kind"`
	Mode   uint32 `json:"mode"` // unix file mode, preserved across install
}

// Lock is the digest-pinned reproducibility lockfile.
type Lock struct {
	Version   string              `json:"version"`
	CreatedAt string              `json:"created_at"`
	Identity  string              `json:"identity"`
	Artifacts map[string]Artifact `json:"artifacts"`
}

// Manifest carries the signature and public key for offline verification.
type Manifest struct {
	RootDigest string `json:"root_digest"`
	PublicKey  string `json:"public_key"`
	Signature  string `json:"signature"`
}

// Source is one artifact to include.
type Source struct {
	Name string // path inside the bundle
	Path string // local file path
	Kind string // binary | config | model | image | mcp | other
}

// Create packs sources into a signed bundle at outPath and returns the lockfile.
func Create(outPath, keyPath, identity string, sources []Source) (*Lock, error) {
	files := map[string][]byte{}
	lock := Lock{Version: "faraday-bundle/v1", CreatedAt: time.Now().UTC().Format(time.RFC3339), Identity: identity, Artifacts: map[string]Artifact{}}
	for _, s := range sources {
		data, err := os.ReadFile(s.Path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", s.Path, err)
		}
		mode := uint32(0o644)
		if fi, serr := os.Stat(s.Path); serr == nil {
			mode = uint32(fi.Mode().Perm())
		}
		files[s.Name] = data
		sum := sha256.Sum256(data)
		lock.Artifacts[s.Name] = Artifact{Digest: "sha256:" + hex.EncodeToString(sum[:]), Size: int64(len(data)), Kind: s.Kind, Mode: mode}
	}
	lockJSON, _ := json.MarshalIndent(lock, "", "  ")
	files[lockName] = lockJSON

	priv, pub, err := loadOrCreateKey(keyPath, outPath)
	if err != nil {
		return nil, err
	}
	root := rootDigest(lock.Artifacts)
	sig := ed25519.Sign(priv, []byte(root))
	man := Manifest{RootDigest: root, PublicKey: hex.EncodeToString(pub), Signature: hex.EncodeToString(sig)}
	manJSON, _ := json.MarshalIndent(man, "", "  ")
	files[manifestName] = manJSON

	if err := writeTarZst(outPath, files); err != nil {
		return nil, err
	}
	return &lock, nil
}

// VerifyResult reports offline verification.
type VerifyResult struct {
	OK         bool     `json:"ok"`
	Identity   string   `json:"identity"`
	Artifacts  int      `json:"artifacts"`
	RootDigest string   `json:"root_digest"`
	Problems   []string `json:"problems,omitempty"`
}

// Verify checks a bundle entirely offline (digests + signature against the bundled key).
func Verify(path string) (VerifyResult, error) {
	files, err := readTarZst(path)
	if err != nil {
		return VerifyResult{}, err
	}
	return verifyFiles(files)
}

func verifyFiles(files map[string][]byte) (VerifyResult, error) {
	var res VerifyResult
	lraw, ok := files[lockName]
	if !ok {
		return VerifyResult{Problems: []string{"lockfile missing"}}, nil
	}
	mraw, ok := files[manifestName]
	if !ok {
		return VerifyResult{Problems: []string{"manifest missing"}}, nil
	}
	var lock Lock
	var man Manifest
	if json.Unmarshal(lraw, &lock) != nil || json.Unmarshal(mraw, &man) != nil {
		return VerifyResult{Problems: []string{"unparsable lock/manifest"}}, nil
	}
	res.Identity = lock.Identity
	res.Artifacts = len(lock.Artifacts)
	res.RootDigest = man.RootDigest

	for name, art := range lock.Artifacts {
		content, present := files[name]
		if !present {
			res.Problems = append(res.Problems, "missing artifact: "+name)
			continue
		}
		sum := sha256.Sum256(content)
		if "sha256:"+hex.EncodeToString(sum[:]) != art.Digest {
			res.Problems = append(res.Problems, "digest mismatch: "+name)
		}
	}
	if rootDigest(lock.Artifacts) != man.RootDigest {
		res.Problems = append(res.Problems, "lock digests do not match root digest")
	}
	pub, err := hex.DecodeString(man.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		res.Problems = append(res.Problems, "invalid public key")
	} else if sig, derr := hex.DecodeString(man.Signature); derr != nil || !ed25519.Verify(pub, []byte(man.RootDigest), sig) {
		res.Problems = append(res.Problems, "signature verification failed")
	}
	res.OK = len(res.Problems) == 0
	return res, nil
}

// InstallReport summarizes an install.
type InstallReport struct {
	Verified  bool     `json:"verified"`
	Installed []string `json:"installed"`
	DestDir   string   `json:"dest_dir"`
}

// Install verifies a bundle offline and extracts its artifacts into destDir. It refuses to
// install anything if verification fails.
func Install(path, destDir string) (*InstallReport, error) {
	files, err := readTarZst(path)
	if err != nil {
		return nil, err
	}
	vr, err := verifyFiles(files)
	if err != nil {
		return nil, err
	}
	if !vr.OK {
		return nil, fmt.Errorf("bundle verification failed: %v", vr.Problems)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}
	// Re-read the lock to recover per-artifact modes.
	var lock Lock
	_ = json.Unmarshal(files[lockName], &lock)

	rep := &InstallReport{Verified: true, DestDir: destDir}
	for name, content := range files {
		if name == lockName || name == manifestName {
			continue
		}
		dest := filepath.Join(destDir, filepath.Clean("/"+name))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, err
		}
		mode := os.FileMode(0o644)
		if art, ok := lock.Artifacts[name]; ok && art.Mode != 0 {
			mode = os.FileMode(art.Mode)
		}
		if err := os.WriteFile(dest, content, mode); err != nil {
			return nil, err
		}
		// WriteFile honors umask; force the recorded mode.
		_ = os.Chmod(dest, mode)
		rep.Installed = append(rep.Installed, name)
	}
	sort.Strings(rep.Installed)
	return rep, nil
}

// --- shared helpers ---

func rootDigest(artifacts map[string]Artifact) string {
	names := make([]string, 0, len(artifacts))
	for n := range artifacts {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		fmt.Fprintf(h, "%s\x00%s\x00%d\x00%d\n", n, artifacts[n].Digest, artifacts[n].Size, artifacts[n].Mode)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func loadOrCreateKey(keyPath, outPath string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	if keyPath == "" {
		keyPath = filepath.Join(filepath.Dir(outPath), "bundle.key")
	}
	if data, err := os.ReadFile(keyPath); err == nil {
		seed, derr := hex.DecodeString(string(trimSpace(data)))
		if derr != nil || len(seed) != ed25519.SeedSize {
			return nil, nil, fmt.Errorf("invalid bundle key at %s", keyPath)
		}
		priv := ed25519.NewKeyFromSeed(seed)
		return priv, priv.Public().(ed25519.PublicKey), nil
	}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, nil, err
	}
	if dir := filepath.Dir(keyPath); dir != "" {
		_ = os.MkdirAll(dir, 0o700)
	}
	_ = os.WriteFile(keyPath, []byte(hex.EncodeToString(priv.Seed())), 0o600)
	return priv, pub, nil
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ' || b[len(b)-1] == '\t') {
		b = b[:len(b)-1]
	}
	return b
}

func writeTarZst(outPath string, files map[string][]byte) error {
	if dir := filepath.Dir(outPath); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	zw, err := zstd.NewWriter(f)
	if err != nil {
		return err
	}
	defer zw.Close()
	tw := tar.NewWriter(zw)
	defer tw.Close()
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		content := files[n]
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: int64(len(content)), ModTime: time.Unix(0, 0)}); err != nil {
			return err
		}
		if _, err := tw.Write(content); err != nil {
			return err
		}
	}
	return nil
}

func readTarZst(path string) (map[string][]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := zstd.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	out := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		out[hdr.Name] = data
	}
	return out, nil
}
