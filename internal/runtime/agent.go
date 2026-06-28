// Package runtime drives the air-gap-native code-gen agent: a
// plan→generate→execute→observe→repair loop that talks to a local model via the inference
// Manager and runs code only through the MCP broker. It owns no network access of its own.
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/faraday-stack/faraday/internal/config"
	"github.com/faraday-stack/faraday/internal/inference"
	"github.com/faraday-stack/faraday/internal/mcp"
	"github.com/faraday-stack/faraday/internal/telemetry"
)

// Generator produces chat completions (satisfied by *inference.Manager; an interface so
// the loop is unit-testable with a programmable fake).
type Generator interface {
	ChatCompletion(ctx context.Context, req inference.ChatCompletionRequest) (inference.ChatCompletionResponse, error)
}

// Engine runs agents.
type Engine struct {
	gen    Generator
	broker *mcp.Broker
	tracer *telemetry.Tracer
}

// NewEngine constructs the agent engine.
func NewEngine(gen Generator, broker *mcp.Broker, tracer *telemetry.Tracer) *Engine {
	return &Engine{gen: gen, broker: broker, tracer: tracer}
}

// RunRequest is one agent run.
type RunRequest struct {
	AgentName string
	Agent     config.Agent
	Task      string
	// Tests, when set, is a Python test program (asserts; exit 0 = pass) run against the
	// generated solution. When empty, the loop runs a smoke execution of the solution.
	Tests    string
	Language string
}

// RunResult is the outcome of a run.
type RunResult struct {
	TraceID    string `json:"trace_id"`
	Status     string `json:"status"` // solved | unresolved | error
	Code       string `json:"code"`
	Output     string `json:"output"`
	Iterations int    `json:"iterations"`
	Passed     bool   `json:"passed"`
}

var codeBlockRe = regexp.MustCompile("(?s)```(?:python|py)?\\s*\\n(.*?)```")

// Run executes the plan→generate→execute→observe→repair loop.
func (e *Engine) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	lang := req.Language
	if lang == "" {
		lang = "python"
	}
	maxRepair := req.Agent.MaxRepairIterations
	if maxRepair <= 0 {
		maxRepair = config.DefaultMaxRepair
	}
	model := req.Agent.Model
	if model == "" {
		model = config.DefaultModelName
	}

	ctx, root := e.tracer.Start(ctx, "invoke_agent "+req.AgentName, telemetry.KindInternal)
	root.SetAttrs(map[string]any{
		telemetry.AttrOperationName: "invoke_agent",
		telemetry.AttrAgentName:     req.AgentName,
		telemetry.AttrAgentID:       req.AgentName,
	})
	defer root.End()
	result := RunResult{TraceID: root.TraceID, Status: "unresolved"}

	// PLAN (single light planning step — recorded as a span).
	_, planSpan := e.tracer.Start(ctx, "plan", telemetry.KindInternal)
	planSpan.SetAttr(telemetry.AttrOperationName, "plan")
	planSpan.SetStatus("ok")
	planSpan.End()

	var lastFailure string
	for iter := 0; iter <= maxRepair; iter++ {
		// GENERATE
		messages := e.buildMessages(req, lastFailure, iter)
		resp, err := e.gen.ChatCompletion(ctx, inference.ChatCompletionRequest{
			Model:       model,
			Messages:    messages,
			Temperature: req.Agent.Temperature,
		})
		if err != nil {
			root.SetStatus("error: " + err.Error())
			result.Status = "error"
			return result, err
		}
		var content string
		if len(resp.Choices) > 0 {
			content = resp.Choices[0].Message.Content
		}
		code := extractCode(content)
		if code == "" {
			code = content // fall back to raw content
		}
		result.Code = code
		result.Iterations = iter + 1

		// EXECUTE + OBSERVE (through the broker only)
		passed, output, err := e.verify(ctx, lang, code, req.Tests)
		if err != nil {
			root.SetStatus("error: " + err.Error())
			result.Status = "error"
			result.Output = output
			return result, err
		}
		result.Output = output
		if passed {
			result.Status = "solved"
			result.Passed = true
			root.SetStatus("ok")
			return result, nil
		}

		// REPAIR: record the iteration and feed the failure back in.
		lastFailure = output
		root.SetAttr(telemetry.AttrRepairIter, iter)
	}
	root.SetStatus("unresolved")
	return result, nil
}

func (e *Engine) buildMessages(req RunRequest, lastFailure string, iter int) []inference.Message {
	sys := req.Agent.System
	if sys == "" {
		sys = "You are a meticulous engineer. Return a single fenced code block with a working solution."
	}
	user := req.Task
	if iter > 0 && lastFailure != "" {
		user = fmt.Sprintf("%s\n\nThe previous attempt failed when executed:\n%s\n\nFix it and return the corrected code.", req.Task, truncate(lastFailure, 2000))
	}
	return []inference.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: user},
	}
}

// verify runs the candidate through the broker: run_tests if tests were supplied, else a
// smoke code_exec.
func (e *Engine) verify(ctx context.Context, lang, code, tests string) (bool, string, error) {
	if tests != "" {
		args, _ := json.Marshal(map[string]any{"language": lang, "code": code, "tests": tests})
		res, err := e.broker.Call(ctx, "run_tests", args)
		if err != nil {
			return false, "", err
		}
		return execOutcome(res)
	}
	args, _ := json.Marshal(map[string]any{"language": lang, "code": code, "entrypoint": "main.py"})
	res, err := e.broker.Call(ctx, "code_exec", args)
	if err != nil {
		return false, "", err
	}
	return execOutcome(res)
}

func execOutcome(res any) (bool, string, error) {
	b, _ := json.Marshal(res)
	var er struct {
		ExitCode int    `json:"exit_code"`
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		TimedOut bool   `json:"timed_out"`
		Passed   bool   `json:"passed"`
	}
	_ = json.Unmarshal(b, &er)
	out := strings.TrimSpace(er.Stdout + "\n" + er.Stderr)
	return er.Passed, out, nil
}

func extractCode(s string) string {
	m := codeBlockRe.FindStringSubmatch(s)
	if len(m) == 2 {
		return strings.TrimSpace(m[1]) + "\n"
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
