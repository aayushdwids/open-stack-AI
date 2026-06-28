package config

import (
	"reflect"
	"testing"
)

func TestMinimalConfigValidWithDefaults(t *testing.T) {
	cfg, err := Parse([]byte("version: faraday/v1\nagents:\n  fixer: {}\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Target.Kind != "local" {
		t.Errorf("default target.kind = %q, want local", cfg.Target.Kind)
	}
	if cfg.Sandbox.Network != "none" {
		t.Errorf("default sandbox.network = %q, want none (air-gap default)", cfg.Sandbox.Network)
	}
	if !cfg.Telemetry.IsEnabled() {
		t.Error("telemetry should default on")
	}
	if cfg.Telemetry.CaptureContent {
		t.Error("content capture should default off")
	}
	a := cfg.Agents["fixer"]
	if a.Model != DefaultModelName || len(a.Tools) == 0 {
		t.Errorf("empty agent should inherit default model+tools, got %+v", a)
	}
	if _, ok := cfg.Models[DefaultModelName]; !ok {
		t.Error("default coder model should be injected")
	}
}

func TestSameConfigResolvesIdenticallyAcrossTargets(t *testing.T) {
	base := "version: faraday/v1\nmodels:\n  coder: {source: m, backend: mock}\nagents:\n  fixer: {model: coder}\n"
	local, err := Parse([]byte("target: {kind: local}\n" + base))
	if err != nil {
		t.Fatal(err)
	}
	air, err := Parse([]byte("target: {kind: airgap}\n" + base))
	if err != nil {
		t.Fatal(err)
	}
	if local.Target.Kind == air.Target.Kind {
		t.Fatal("targets should differ")
	}
	// Everything except target must be identical.
	if !reflect.DeepEqual(local.Agents, air.Agents) {
		t.Error("agents differ across targets")
	}
	if !reflect.DeepEqual(local.Models, air.Models) {
		t.Error("models differ across targets")
	}
	if !reflect.DeepEqual(local.Sandbox, air.Sandbox) {
		t.Error("sandbox differs across targets")
	}
}

func TestInvalidConfigRejected(t *testing.T) {
	cases := []string{
		"version: faraday/v2\n",                              // bad version
		"version: faraday/v1\nsandbox: {network: open}\n",    // bad network
		"version: faraday/v1\nagents:\n  x: {model: nope}\n",  // unknown model ref
		"version: faraday/v1\nboguskey: 1\n",                  // unknown field
	}
	for _, c := range cases {
		if _, err := Parse([]byte(c)); err == nil {
			t.Errorf("expected error for config: %q", c)
		}
	}
}

func TestSchemaGenerates(t *testing.T) {
	b, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 100 {
		t.Errorf("schema too small: %d bytes", len(b))
	}
}
