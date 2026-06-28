package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/faraday-stack/faraday/internal/config"
	"github.com/faraday-stack/faraday/internal/inference"
	"github.com/faraday-stack/faraday/internal/mcp"
	"github.com/faraday-stack/faraday/internal/store"
	"github.com/faraday-stack/faraday/internal/telemetry"
)

// Generator produces chat completions (satisfied by *inference.Manager).
type Generator interface {
	ChatCompletion(ctx context.Context, req inference.ChatCompletionRequest) (inference.ChatCompletionResponse, error)
}

// Runner executes eval suites.
type Runner struct {
	gen    Generator
	broker *mcp.Broker
	tracer *telemetry.Tracer
	st     *store.Store
	seed   int64
}

// NewRunner constructs a Runner.
func NewRunner(gen Generator, broker *mcp.Broker, tracer *telemetry.Tracer, st *store.Store) *Runner {
	return &Runner{gen: gen, broker: broker, tracer: tracer, st: st, seed: 42}
}

// SuiteResult summarizes a suite run.
type SuiteResult struct {
	RunID         string             `json:"run_id"`
	Suite         string             `json:"suite"`
	Kind          string             `json:"kind"`
	Model         string             `json:"model"`
	DatasetDigest string             `json:"dataset_digest"`
	Seed          int64              `json:"seed"`
	Metrics       map[string]float64 `json:"metrics"`
	Passed        bool               `json:"passed"`
	GateFailures  []string           `json:"gate_failures,omitempty"`
	Cases         int                `json:"cases"`
}

var codeRe = regexp.MustCompile("(?s)```(?:python|py)?\\s*\\n(.*?)```")

// RunSuite runs one suite and records a reproducible run + per-case results + spans.
func (r *Runner) RunSuite(ctx context.Context, suite config.Suite, defaultModel string) (SuiteResult, error) {
	model := defaultModel
	if suite.Judge != "" && suite.Kind == "judge" {
		// judge model is used for scoring; generation still uses defaultModel
	}
	ctx, span := r.tracer.Start(ctx, "eval "+suite.Name, telemetry.KindInternal)
	span.SetAttrs(map[string]any{
		telemetry.AttrOperationName: "eval",
		telemetry.AttrEvalSuite:     suite.Name,
	})
	defer span.End()

	runID := fmt.Sprintf("eval-%d", time.Now().UnixNano())
	res := SuiteResult{RunID: runID, Suite: suite.Name, Kind: suite.Kind, Model: model, Seed: r.seed, Metrics: map[string]float64{}}

	var err error
	switch suite.Kind {
	case "code_passk":
		err = r.runCodePassK(ctx, suite, model, &res)
	case "deterministic":
		err = r.runDeterministic(ctx, suite, &res)
	case "judge":
		err = r.runJudge(ctx, suite, model, &res)
	default:
		err = fmt.Errorf("unsupported suite kind %q", suite.Kind)
	}
	if err != nil {
		span.SetStatus("error: " + err.Error())
		return res, err
	}

	// Thresholds.
	res.Passed = true
	for metric, min := range suite.Threshold {
		if got, ok := res.Metrics[metric]; !ok || got < min {
			res.Passed = false
			res.GateFailures = append(res.GateFailures, fmt.Sprintf("threshold %s: got %.3f < %.3f", metric, res.Metrics[metric], min))
		}
	}

	// Record the run.
	if r.st != nil {
		_ = r.st.InsertEvalRun(store.EvalRun{
			ID: runID, Suite: suite.Name, Kind: suite.Kind,
			StartedUnixNano: time.Now().UnixNano(),
			DatasetDigest:   res.DatasetDigest, Seed: r.seed, Model: model,
			Metrics: res.Metrics, Passed: res.Passed,
		})
	}
	for metric, v := range res.Metrics {
		span.SetAttr("faraday.eval."+metric, v)
	}
	span.SetAttr(telemetry.AttrEvalScore, primaryScore(res.Metrics))
	span.SetStatus("ok")
	return res, nil
}

func (r *Runner) runCodePassK(ctx context.Context, suite config.Suite, model string, res *SuiteResult) error {
	ds, err := LoadDataset(suite.Dataset)
	if err != nil {
		return err
	}
	res.DatasetDigest = ds.Digest()
	res.Cases = len(ds.Cases)
	k := suite.K
	if k <= 0 {
		k = 1
	}
	n := suite.NSamples
	if n < k {
		n = k
	}
	var sumPassK float64
	for _, c := range ds.Cases {
		correct := 0
		for s := 0; s < n; s++ {
			code, gerr := r.generateCode(ctx, model, c.Prompt)
			if gerr != nil {
				return gerr
			}
			passed, _ := r.runCaseTests(ctx, ds.Language, code, c.Tests)
			if passed {
				correct++
			}
		}
		pk := passAtK(n, correct, k)
		sumPassK += pk
		if r.st != nil {
			_ = r.st.InsertEvalResult(res.RunID, c.ID, pk >= 1.0, pk, fmt.Sprintf("%d/%d correct", correct, n))
		}
	}
	if len(ds.Cases) > 0 {
		passRate := sumPassK / float64(len(ds.Cases))
		res.Metrics[fmt.Sprintf("pass@%d", k)] = passRate
		res.Metrics["pass_rate"] = passRate
	}
	return nil
}

func (r *Runner) runDeterministic(ctx context.Context, suite config.Suite, res *SuiteResult) error {
	ds, err := LoadDataset(suite.Dataset)
	if err != nil {
		return err
	}
	res.DatasetDigest = ds.Digest()
	res.Cases = len(ds.Cases)
	pass := 0
	for _, c := range ds.Cases {
		checks := c.Checks
		if len(checks) == 0 {
			checks = suite.Checks
		}
		ok, _ := r.evalChecks(ctx, ds.Language, c.Code, checks)
		if ok {
			pass++
		}
		if r.st != nil {
			_ = r.st.InsertEvalResult(res.RunID, c.ID, ok, boolScore(ok), "")
		}
	}
	if len(ds.Cases) > 0 {
		res.Metrics["pass_rate"] = float64(pass) / float64(len(ds.Cases))
	}
	return nil
}

func (r *Runner) runJudge(ctx context.Context, suite config.Suite, model string, res *SuiteResult) error {
	ds, err := LoadDataset(suite.Dataset)
	if err != nil {
		return err
	}
	res.DatasetDigest = ds.Digest()
	res.Cases = len(ds.Cases)
	var sum float64
	for _, c := range ds.Cases {
		answer, gerr := r.generate(ctx, model, c.Prompt)
		if gerr != nil {
			return gerr
		}
		score := r.judgeScore(ctx, suite.Judge, c.Prompt, answer, c.Rubric)
		sum += score
		if r.st != nil {
			_ = r.st.InsertEvalResult(res.RunID, c.ID, score >= 3.0, score, "")
		}
	}
	if len(ds.Cases) > 0 {
		res.Metrics["mean_score"] = sum / float64(len(ds.Cases))
	}
	return nil
}

// --- helpers ---

func (r *Runner) generate(ctx context.Context, model, prompt string) (string, error) {
	resp, err := r.gen.ChatCompletion(ctx, inference.ChatCompletionRequest{
		Model:    model,
		Messages: []inference.Message{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", nil
	}
	return resp.Choices[0].Message.Content, nil
}

func (r *Runner) generateCode(ctx context.Context, model, prompt string) (string, error) {
	content, err := r.generate(ctx, model, prompt)
	if err != nil {
		return "", err
	}
	if m := codeRe.FindStringSubmatch(content); len(m) == 2 {
		return strings.TrimSpace(m[1]) + "\n", nil
	}
	return content, nil
}

func (r *Runner) runCaseTests(ctx context.Context, lang, code, tests string) (bool, string) {
	args, _ := json.Marshal(map[string]any{"language": lang, "code": code, "tests": tests})
	out, err := r.broker.Call(ctx, "run_tests", args)
	if err != nil {
		return false, err.Error()
	}
	return readPassed(out)
}

func (r *Runner) evalChecks(ctx context.Context, lang, code string, checks []string) (bool, string) {
	args, _ := json.Marshal(map[string]any{"language": lang, "code": code, "entrypoint": "main.py"})
	out, err := r.broker.Call(ctx, "code_exec", args)
	if err != nil {
		return false, err.Error()
	}
	b, _ := json.Marshal(out)
	var er struct {
		ExitCode int    `json:"exit_code"`
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
	}
	_ = json.Unmarshal(b, &er)
	for _, chk := range checks {
		switch {
		case chk == "exit_zero":
			if er.ExitCode != 0 {
				return false, "non-zero exit"
			}
		case chk == "no_stderr":
			if strings.TrimSpace(er.Stderr) != "" {
				return false, "stderr present"
			}
		case strings.HasPrefix(chk, "contains:"):
			if !strings.Contains(er.Stdout, strings.TrimPrefix(chk, "contains:")) {
				return false, "missing expected output"
			}
		case strings.HasPrefix(chk, "regex:"):
			re, e := regexp.Compile(strings.TrimPrefix(chk, "regex:"))
			if e != nil || !re.MatchString(er.Stdout) {
				return false, "regex did not match"
			}
		}
	}
	return true, ""
}

func (r *Runner) judgeScore(ctx context.Context, judgeModel, prompt, answer, rubric string) float64 {
	p := fmt.Sprintf("Score the following answer from 1 to 5 (integer only).\nRubric: %s\nPrompt: %s\nAnswer: %s\nScore:", rubric, prompt, answer)
	content, err := r.generate(ctx, judgeModel, p)
	if err != nil {
		return 0
	}
	if m := regexp.MustCompile(`[1-5]`).FindString(content); m != "" {
		v, _ := strconv.Atoi(m)
		return float64(v)
	}
	return 0
}

func readPassed(out any) (bool, string) {
	b, _ := json.Marshal(out)
	var er struct {
		Passed bool   `json:"passed"`
		Stdout string `json:"stdout"`
		Stderr string `json:"stderr"`
	}
	_ = json.Unmarshal(b, &er)
	return er.Passed, strings.TrimSpace(er.Stdout + "\n" + er.Stderr)
}

// passAtK is the unbiased pass@k estimator: 1 - C(n-c,k)/C(n,k).
func passAtK(n, c, k int) float64 {
	if n-c < k {
		return 1.0
	}
	p := 1.0
	for i := 0; i < k; i++ {
		p *= float64(n-c-i) / float64(n-i)
	}
	return 1 - p
}

func primaryScore(m map[string]float64) float64 {
	if v, ok := m["pass_rate"]; ok {
		return v
	}
	if v, ok := m["mean_score"]; ok {
		return v
	}
	for _, v := range m {
		return v
	}
	return 0
}

func boolScore(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// CheckRegression compares a run's primary metric to the prior run for the suite and
// returns a non-empty reason if a gated regression occurred.
func (r *Runner) CheckRegression(suite string, current float64, baseline string) string {
	if r.st == nil {
		return ""
	}
	prev, err := r.st.LastEvalRun(suite)
	if err != nil || prev == nil {
		return ""
	}
	prevScore := primaryScore(prev.Metrics)
	const eps = 0.001
	if current+eps < prevScore {
		return fmt.Sprintf("regression: %.3f < baseline %.3f", current, prevScore)
	}
	return ""
}
