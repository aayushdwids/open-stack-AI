package license

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIssueVerifyOffline(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	pubHex := hex.EncodeToString(pub)

	l := Issue(priv, Claims{Tier: TierTeam, Licensee: "ACME", Seats: 10, Features: []string{"team-aggregation"}})
	if err := l.Verify(pubHex); err != nil {
		t.Fatalf("valid license should verify: %v", err)
	}

	// Wrong key fails.
	otherPub, _, _ := ed25519.GenerateKey(nil)
	if err := l.Verify(hex.EncodeToString(otherPub)); err == nil {
		t.Fatal("wrong key must fail")
	}

	// Expired fails.
	exp := Issue(priv, Claims{Tier: TierTeam, ExpiryUnix: time.Now().Add(-time.Hour).Unix()})
	if err := exp.Verify(pubHex); err == nil {
		t.Fatal("expired license must fail")
	}

	// Tampered claims fail.
	l.Claims.Seats = 9999
	if err := l.Verify(pubHex); err == nil {
		t.Fatal("tampered claims must fail")
	}
}

func TestActiveResolvesFreeAndTeam(t *testing.T) {
	// No file -> free.
	if _, paid := Active(""); paid {
		t.Fatal("no license should be free tier")
	}

	pub, priv, _ := ed25519.GenerateKey(nil)
	old := ProductionPublicKey
	ProductionPublicKey = hex.EncodeToString(pub)
	defer func() { ProductionPublicKey = old }()

	l := Issue(priv, Claims{Tier: TierTeam, Licensee: "ACME"})
	data, _ := json.Marshal(l)
	path := filepath.Join(t.TempDir(), "lic.json")
	_ = os.WriteFile(path, data, 0o600)

	claims, paid := Active(path)
	if !paid || claims.Tier != TierTeam {
		t.Fatalf("valid team license should activate: %+v paid=%v", claims, paid)
	}
	if !claims.HasFeature("team-aggregation") {
		t.Error("team tier with no explicit list should grant team features")
	}
}
