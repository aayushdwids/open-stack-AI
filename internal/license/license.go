// Package license implements offline, air-gap-safe license validation. A signed license
// payload (tier, seats, expiry, features) is verified locally against a public key
// embedded in the binary — no network call, ever. This is the only model that works inside
// a sealed enclave. Free single-user features never require a license; only the team tier
// is gated.
package license

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// Tiers.
const (
	TierFree = "free"
	TierTeam = "team"
)

// ProductionPublicKey is the vendor's Ed25519 public key, hard-coded into the binary. The
// matching private key never ships. (Placeholder all-zero key in the open-source build; a
// real key is injected at release via -ldflags. Verification against the zero key always
// fails, so unlicensed builds correctly expose only the free tier.)
var ProductionPublicKey = "0000000000000000000000000000000000000000000000000000000000000000"

// Claims is the signed license payload.
type Claims struct {
	Tier       string   `json:"tier"`
	Licensee   string   `json:"licensee"`
	Seats      int      `json:"seats"`
	ExpiryUnix int64    `json:"expiry_unix"`
	Features   []string `json:"features"`
}

// License is signed claims.
type License struct {
	Claims    Claims `json:"claims"`
	Signature string `json:"signature"` // hex Ed25519 over canonical claims JSON
}

// FreeClaims is the default when no license is present.
func FreeClaims() Claims { return Claims{Tier: TierFree, Seats: 1} }

// Issue signs claims with a private key (used by the vendor / in tests).
func Issue(priv ed25519.PrivateKey, c Claims) License {
	sig := ed25519.Sign(priv, canonical(c))
	return License{Claims: c, Signature: hex.EncodeToString(sig)}
}

// Verify checks the signature against a public key and the expiry.
func (l License) Verify(pubHex string) error {
	pub, err := hex.DecodeString(pubHex)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("invalid public key")
	}
	sig, err := hex.DecodeString(l.Signature)
	if err != nil {
		return errors.New("invalid signature encoding")
	}
	if !ed25519.Verify(pub, canonical(l.Claims), sig) {
		return errors.New("signature verification failed")
	}
	if l.Claims.ExpiryUnix != 0 && time.Now().Unix() > l.Claims.ExpiryUnix {
		return fmt.Errorf("license expired on %s", time.Unix(l.Claims.ExpiryUnix, 0).UTC().Format(time.RFC3339))
	}
	return nil
}

// LoadFile reads a license JSON file.
func LoadFile(path string) (License, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return License{}, err
	}
	var l License
	if err := json.Unmarshal(data, &l); err != nil {
		return License{}, fmt.Errorf("parse license: %w", err)
	}
	return l, nil
}

// Active resolves the effective claims: a verified license file against the embedded
// production key, or the free tier when absent/invalid. The second return reports whether a
// valid paid license was loaded.
func Active(path string) (Claims, bool) {
	if path == "" {
		if env := os.Getenv("FARADAY_LICENSE"); env != "" {
			path = env
		}
	}
	if path == "" {
		return FreeClaims(), false
	}
	l, err := LoadFile(path)
	if err != nil {
		return FreeClaims(), false
	}
	if err := l.Verify(ProductionPublicKey); err != nil {
		return FreeClaims(), false
	}
	return l.Claims, l.Claims.Tier == TierTeam
}

// HasFeature reports whether claims grant a named feature (team tier implies all).
func (c Claims) HasFeature(name string) bool {
	if c.Tier == TierTeam {
		for _, f := range c.Features {
			if f == name {
				return true
			}
		}
		return len(c.Features) == 0 // team with no explicit list grants all team features
	}
	return false
}

func canonical(c Claims) []byte {
	b, _ := json.Marshal(c)
	return b
}
