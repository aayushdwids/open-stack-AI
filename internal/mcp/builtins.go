package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/faraday-stack/faraday/internal/runtime/sandbox"
)

// execResult is the broker's exec-shaped tool result (also lets the broker tag spans).
type execResult struct {
	ExitCode  int    `json:"exit_code"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	TimedOut  bool   `json:"timed_out"`
	Passed    bool   `json:"passed"`
	SandboxID string `json:"sandbox_id"`
}

// codeExecArgs is the input to code_exec.
type codeExecArgs struct {
	Language    string            `json:"language"`
	Code        string            `json:"code"`
	Files       map[string]string `json:"files"`
	Entrypoint  string            `json:"entrypoint"`
	TimeoutSecs int               `json:"timeout_secs"`
}

// runTestsArgs is the input to run_tests.
type runTestsArgs struct {
	Language    string `json:"language"`
	Code        string `json:"code"`  // module under test -> solution.py
	Tests       string `json:"tests"` // test program -> test.py (asserts; exit 0 = pass)
	TimeoutSecs int    `json:"timeout_secs"`
}

func (b *Broker) registerBuiltins() {
	b.Register(Tool{
		Name:        "code_exec",
		Description: "Execute code in a network-isolated sandbox and return stdout/stderr/exit status.",
		Type:        "builtin",
		Handler:     b.codeExec,
	})
	b.Register(Tool{
		Name:        "run_tests",
		Description: "Run a candidate solution against a test program in a sandbox; exit 0 means the tests passed.",
		Type:        "builtin",
		Handler:     b.runTests,
	})
}

func (b *Broker) codeExec(ctx context.Context, raw json.RawMessage) (any, error) {
	var a codeExecArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("code_exec args: %w", err)
	}
	files := map[string]string{}
	for k, v := range a.Files {
		files[k] = v
	}
	entry := a.Entrypoint
	if entry == "" {
		entry = "main.py"
	}
	if a.Code != "" {
		files[entry] = a.Code
	}
	cmd := interpreterCmd(a.Language, entry)
	res, sid, err := b.runInSandbox(ctx, sandbox.ExecRequest{Files: files, Cmd: cmd, TimeoutSecs: a.TimeoutSecs})
	if err != nil {
		return nil, err
	}
	return execResult{
		ExitCode: res.ExitCode, Stdout: res.Stdout, Stderr: res.Stderr,
		TimedOut: res.TimedOut, Passed: res.ExitCode == 0 && !res.TimedOut, SandboxID: sid,
	}, nil
}

func (b *Broker) runTests(ctx context.Context, raw json.RawMessage) (any, error) {
	var a runTestsArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("run_tests args: %w", err)
	}
	files := map[string]string{
		"solution.py": a.Code,
		"test.py":     a.Tests,
	}
	res, sid, err := b.runInSandbox(ctx, sandbox.ExecRequest{
		Files: files, Cmd: interpreterCmd(a.Language, "test.py"), TimeoutSecs: a.TimeoutSecs,
	})
	if err != nil {
		return nil, err
	}
	return execResult{
		ExitCode: res.ExitCode, Stdout: res.Stdout, Stderr: res.Stderr,
		TimedOut: res.TimedOut, Passed: res.ExitCode == 0 && !res.TimedOut, SandboxID: sid,
	}, nil
}

func interpreterCmd(language, entry string) []string {
	switch language {
	case "", "python", "py", "python3":
		return []string{"python3", entry}
	case "bash", "sh":
		return []string{"sh", entry}
	default:
		return []string{"python3", entry}
	}
}
