// Package mcp is the daemon's tool broker. Agents reach tools ONLY through the broker —
// never the sandbox directly — and every tool call emits an execute_tool span. Built-in
// tools (code_exec, run_tests, fs_read, fs_write) are hosted here; external MCP servers
// attach through the same interface in later slices.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/faraday-stack/faraday/internal/runtime/pool"
	"github.com/faraday-stack/faraday/internal/runtime/sandbox"
	"github.com/faraday-stack/faraday/internal/telemetry"
)

// Tool is a callable tool.
type Tool struct {
	Name        string
	Description string
	Type        string // builtin | mcp
	Handler     func(ctx context.Context, args json.RawMessage) (any, error)
}

// Broker hosts tools and enforces that execution goes through it.
type Broker struct {
	pool   *pool.Pool
	tracer *telemetry.Tracer
	tools  map[string]Tool
	callN  int
}

// NewBroker builds a broker with the built-in tools registered.
func NewBroker(p *pool.Pool, tracer *telemetry.Tracer) *Broker {
	b := &Broker{pool: p, tracer: tracer, tools: map[string]Tool{}}
	b.registerBuiltins()
	return b
}

// Register adds a tool.
func (b *Broker) Register(t Tool) { b.tools[t.Name] = t }

// ToolInfo describes a registered tool.
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
}

// List returns registered tools.
func (b *Broker) List() []ToolInfo {
	var out []ToolInfo
	for _, t := range b.tools {
		out = append(out, ToolInfo{Name: t.Name, Description: t.Description, Type: t.Type})
	}
	return out
}

// Has reports whether a tool is registered.
func (b *Broker) Has(name string) bool { _, ok := b.tools[name]; return ok }

// Call invokes a tool by name, emitting an execute_tool span. This is the sole entry
// point; the agent cannot allocate a sandbox except via a registered tool.
func (b *Broker) Call(ctx context.Context, name string, args json.RawMessage) (any, error) {
	t, ok := b.tools[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	b.callN++
	callID := fmt.Sprintf("call-%d", b.callN)
	_, span := b.tracer.Start(ctx, "execute_tool "+name, telemetry.KindInternal)
	span.SetAttrs(map[string]any{
		telemetry.AttrOperationName: "execute_tool",
		telemetry.AttrToolName:      name,
		telemetry.AttrToolCallID:    callID,
		telemetry.AttrToolType:      t.Type,
	})
	defer span.End()

	res, err := t.Handler(ctx, args)
	if err != nil {
		span.SetStatus("error: " + err.Error())
		return nil, err
	}
	// Surface exit status on the span when the tool returns an exec result.
	if er, ok := res.(execResult); ok {
		span.SetAttr(telemetry.AttrExitStatus, er.ExitCode)
		if er.SandboxID != "" {
			span.SetAttr(telemetry.AttrSandboxID, er.SandboxID)
		}
	}
	span.SetStatus("ok")
	return res, nil
}

// runInSandbox acquires a sandbox, runs the request, and releases it.
func (b *Broker) runInSandbox(ctx context.Context, req sandbox.ExecRequest) (sandbox.ExecResult, string, error) {
	sb, err := b.pool.Acquire(ctx)
	if err != nil {
		return sandbox.ExecResult{}, "", err
	}
	defer b.pool.Release(sb)
	res, err := sb.Exec(ctx, req)
	return res, sb.ID(), err
}
