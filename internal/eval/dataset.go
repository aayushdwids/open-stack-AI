// Package eval runs offline eval suites: code_passk (sandboxed unit-test execution),
// deterministic (rule checks), and judge (LLM-as-judge against a local model). It records
// reproducible runs and gates CI via exit codes. Everything runs with no network access.
package eval

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

//go:embed datasets/*.json
var bundledFS embed.FS

// Case is one evaluation case.
type Case struct {
	ID     string   `json:"id"`
	Prompt string   `json:"prompt"`
	Tests  string   `json:"tests"`
	Code   string   `json:"code,omitempty"`   // for deterministic suites operating on fixed code
	Checks []string `json:"checks,omitempty"` // for deterministic suites
	Rubric string   `json:"rubric,omitempty"` // for judge suites
}

// Dataset is a collection of cases.
type Dataset struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Language    string `json:"language"`
	Cases       []Case `json:"cases"`
	digest      string
}

// Digest returns the content digest pinned into eval runs for reproducibility.
func (d *Dataset) Digest() string { return d.digest }

// LoadDataset resolves a dataset reference: "bundled:<name>" loads an embedded fixture;
// any other value is treated as a local file path. No network access is ever used.
func LoadDataset(ref string) (*Dataset, error) {
	var data []byte
	var err error
	switch {
	case strings.HasPrefix(ref, "bundled:"):
		name := strings.TrimPrefix(ref, "bundled:")
		data, err = bundledFS.ReadFile("datasets/" + name + ".json")
		if err != nil {
			return nil, fmt.Errorf("unknown bundled dataset %q", name)
		}
	default:
		data, err = os.ReadFile(ref)
		if err != nil {
			return nil, fmt.Errorf("read dataset %q: %w", ref, err)
		}
	}
	var d Dataset
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("parse dataset %q: %w", ref, err)
	}
	if d.Language == "" {
		d.Language = "python"
	}
	sum := sha256.Sum256(data)
	d.digest = hex.EncodeToString(sum[:])
	return &d, nil
}
