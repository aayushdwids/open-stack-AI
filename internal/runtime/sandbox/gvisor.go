package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// GvisorDriver runs each execution in a gVisor (runsc) container with no network. This is
// the real air-gap-native path on Linux hosts that have a container runtime + runsc.
type GvisorDriver struct {
	cfg   Config
	image string
}

// NewGvisorDriver constructs the driver if docker + the runsc runtime are available.
func NewGvisorDriver(cfg Config) (*GvisorDriver, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, errors.New("gvisor driver requires docker (with the runsc runtime)")
	}
	out, err := exec.Command("docker", "info", "--format", "{{json .Runtimes}}").Output()
	if err != nil || !strings.Contains(string(out), "runsc") {
		return nil, errors.New("gvisor runtime (runsc) is not registered with docker")
	}
	image := cfg.image()
	return &GvisorDriver{cfg: cfg, image: image}, nil
}

func (c Config) image() string {
	return "faraday/sandbox-python:latest"
}

// Name returns the driver name.
func (d *GvisorDriver) Name() string { return "gvisor" }

// NetworkIsolated reports true: containers run with --network=none.
func (d *GvisorDriver) NetworkIsolated() bool { return true }

// New creates a gVisor sandbox backed by a host working directory bind-mounted in.
func (d *GvisorDriver) New(_ context.Context) (Sandbox, error) {
	dir, err := os.MkdirTemp("", "faraday-gsbx-")
	if err != nil {
		return nil, err
	}
	return &gvisorSandbox{id: "gsbx-" + randID(), dir: dir, drv: d}, nil
}

type gvisorSandbox struct {
	id  string
	dir string
	drv *GvisorDriver
}

func (s *gvisorSandbox) ID() string { return s.id }

func (s *gvisorSandbox) Exec(ctx context.Context, req ExecRequest) (ExecResult, error) {
	if len(req.Cmd) == 0 {
		return ExecResult{}, errors.New("empty command")
	}
	for name, content := range req.Files {
		p := filepath.Join(s.dir, filepath.Clean("/"+name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return ExecResult{}, err
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return ExecResult{}, err
		}
	}
	timeout := req.TimeoutSecs
	if timeout <= 0 {
		timeout = s.drv.cfg.DefaultTimeoutSecs
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	args := []string{
		"run", "--rm",
		"--runtime=runsc",
		"--network=none", // the hard air-gap guarantee at the sandbox boundary
		"--read-only",
		"--tmpfs", "/tmp:rw,size=256m",
		"-v", s.dir + ":/work",
		"-w", "/work",
	}
	if s.drv.cfg.MemoryLimitMB > 0 {
		args = append(args, "-m", fmt.Sprintf("%dm", s.drv.cfg.MemoryLimitMB))
	}
	args = append(args, s.drv.image)
	args = append(args, req.Cmd...)

	cmd := exec.CommandContext(runCtx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	res := ExecResult{Stdout: stdout.String(), Stderr: stderr.String(), DurationMs: time.Since(start).Milliseconds()}
	if runCtx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		res.ExitCode = 124
		return res, nil
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
			return res, nil
		}
		return res, fmt.Errorf("docker run: %w", err)
	}
	return res, nil
}

func (s *gvisorSandbox) Reset() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		_ = os.RemoveAll(filepath.Join(s.dir, e.Name()))
	}
	return nil
}

func (s *gvisorSandbox) Destroy() error { return os.RemoveAll(s.dir) }
