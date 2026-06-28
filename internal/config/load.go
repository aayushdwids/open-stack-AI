package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/invopop/jsonschema"
	"gopkg.in/yaml.v3"
)

// ValidationError is a located config error.
type ValidationError struct {
	Path string
	Msg  string
}

func (e ValidationError) Error() string {
	if e.Path == "" {
		return e.Msg
	}
	return fmt.Sprintf("%s: %s", e.Path, e.Msg)
}

// Load reads, strictly decodes, defaults, and validates a faraday.yaml file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(data)
}

// Parse decodes config bytes with strict unknown-field detection, applies defaults,
// and validates.
func Parse(data []byte) (*Config, error) {
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // reject unknown keys with a located error
	if err := dec.Decode(&c); err != nil {
		if errors.Is(err, nil) {
			return nil, err
		}
		return nil, ValidationError{Msg: cleanYAMLErr(err)}
	}
	c.ApplyDefaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func cleanYAMLErr(err error) string {
	return fmt.Sprintf("invalid config: %v", err)
}

// Validate performs semantic validation after defaults are applied.
func (c *Config) Validate() error {
	if c.Version != CurrentVersion {
		return ValidationError{Path: "version", Msg: fmt.Sprintf("unsupported version %q (want %q)", c.Version, CurrentVersion)}
	}
	switch c.Target.Kind {
	case "local", "cloud", "airgap":
	default:
		return ValidationError{Path: "target.kind", Msg: fmt.Sprintf("must be local|cloud|airgap, got %q", c.Target.Kind)}
	}
	switch c.Sandbox.Network {
	case "none", "host-deny":
	default:
		return ValidationError{Path: "sandbox.network", Msg: fmt.Sprintf("must be none|host-deny, got %q", c.Sandbox.Network)}
	}
	switch c.Sandbox.Runtime {
	case "auto", "gvisor", "firecracker", "libkrun", "local":
	default:
		return ValidationError{Path: "sandbox.runtime", Msg: fmt.Sprintf("unknown runtime %q", c.Sandbox.Runtime)}
	}
	// Agents must reference a known model.
	for name, a := range c.Agents {
		if _, ok := c.Models[a.Model]; !ok {
			if _, routed := c.Routing[a.Model]; !routed {
				return ValidationError{Path: "agents." + name + ".model", Msg: fmt.Sprintf("references unknown model/route %q", a.Model)}
			}
		}
	}
	// Eval suites must name a kind and (for judge) a judge model.
	for i, s := range c.Eval.Suites {
		if s.Name == "" {
			return ValidationError{Path: fmt.Sprintf("eval.suites[%d].name", i), Msg: "required"}
		}
		switch s.Kind {
		case "code_passk", "deterministic", "judge", "regression":
		default:
			return ValidationError{Path: fmt.Sprintf("eval.suites[%d].kind", i), Msg: fmt.Sprintf("unknown kind %q", s.Kind)}
		}
		if s.Kind == "judge" && s.Judge == "" {
			return ValidationError{Path: fmt.Sprintf("eval.suites[%d].judge", i), Msg: "judge model required for kind=judge"}
		}
	}
	// Routing backends must reference known models.
	for name, r := range c.Routing {
		for j, b := range r.Backends {
			if _, ok := c.Models[b.Model]; !ok {
				return ValidationError{Path: fmt.Sprintf("routing.%s.backends[%d].model", name, j), Msg: fmt.Sprintf("unknown model %q", b.Model)}
			}
		}
	}
	return nil
}

// Schema returns the JSON Schema generated from the config structs.
func Schema() ([]byte, error) {
	r := &jsonschema.Reflector{
		ExpandedStruct:             true,
		AllowAdditionalProperties:  false,
		DoNotReference:             true,
		RequiredFromJSONSchemaTags: true,
	}
	s := r.Reflect(&Config{})
	return s.MarshalJSON()
}
