package evidence

import (
	"archive/tar"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"
)

// VerifyResult reports the outcome of offline verification.
type VerifyResult struct {
	OK         bool     `json:"ok"`
	Identity   string   `json:"identity"`
	RootDigest string   `json:"root_digest"`
	Files      int      `json:"files"`
	Problems   []string `json:"problems,omitempty"`
}

// Verify checks a bundle entirely offline: it recomputes every file digest, recomputes
// the root digest, and verifies the Ed25519 signature with the public key carried in the
// bundle. No network access is used.
func Verify(path string) (VerifyResult, error) {
	files, err := readTarZst(path)
	if err != nil {
		return VerifyResult{}, err
	}
	mraw, ok := files["manifest.json"]
	if !ok {
		return VerifyResult{OK: false, Problems: []string{"manifest.json missing"}}, nil
	}
	var m Manifest
	if err := json.Unmarshal(mraw, &m); err != nil {
		return VerifyResult{OK: false, Problems: []string{"manifest.json unparsable"}}, nil
	}

	res := VerifyResult{Identity: m.Identity, RootDigest: m.RootDigest, Files: len(m.Files)}

	// Recompute each file digest.
	recomputed := map[string]string{}
	for name, want := range m.Files {
		content, present := files[name]
		if !present {
			res.Problems = append(res.Problems, "missing file: "+name)
			continue
		}
		sum := sha256.Sum256(content)
		got := hex.EncodeToString(sum[:])
		recomputed[name] = got
		if got != want {
			res.Problems = append(res.Problems, "digest mismatch: "+name)
		}
	}

	// Recompute root digest and compare.
	if rootDigest(m.Files) != m.RootDigest {
		res.Problems = append(res.Problems, "root digest does not match file digests")
	}
	if rootDigest(recomputed) != m.RootDigest {
		res.Problems = append(res.Problems, "recomputed content does not match root digest")
	}

	// Verify the signature with the bundled public key.
	pub, err := hex.DecodeString(m.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		res.Problems = append(res.Problems, "invalid public key")
	} else {
		sig, derr := hex.DecodeString(m.Signature)
		if derr != nil || !ed25519.Verify(pub, []byte(m.RootDigest), sig) {
			res.Problems = append(res.Problems, "signature verification failed")
		}
	}

	res.OK = len(res.Problems) == 0
	return res, nil
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
			return nil, fmt.Errorf("read %s: %w", hdr.Name, err)
		}
		out[hdr.Name] = data
	}
	return out, nil
}
