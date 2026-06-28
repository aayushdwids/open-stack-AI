package sandbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// LocalDriver runs commands in an isolated temp directory with OS-enforced network
// denial. It is the cross-platform fallback used when gVisor is absent.
type LocalDriver struct {
	cfg     Config
	wrapper []string // command prefix that enforces network isolation
}

// NewLocalDriver constructs the local driver, selecting an OS network-isolation wrapper.
// If no isolation mechanism is available it returns an error rather than silently running
// without the air-gap guarantee.
func NewLocalDriver(cfg Config) (*LocalDriver, error) {
	d := &LocalDriver{cfg: cfg}
	if cfg.DefaultTimeoutSecs <= 0 {
		d.cfg.DefaultTimeoutSecs = 120
	}
	w, err := networkDenyWrapper()
	if err != nil {
		return nil, err
	}
	d.wrapper = w
	return d, nil
}

// Name returns the driver name.
func (d *LocalDriver) Name() string { return "local" }

// NetworkIsolated reports true: the wrapper enforces no egress.
func (d *LocalDriver) NetworkIsolated() bool { return true }

// New creates a local sandbox backed by a fresh temp directory.
func (d *LocalDriver) New(_ context.Context) (Sandbox, error) {
	dir, err := os.MkdirTemp("", "faraday-sbx-")
	if err != nil {
		return nil, err
	}
	return &localSandbox{id: "sbx-" + randID(), dir: dir, drv: d}, nil
}

// networkDenyWrapper returns a command prefix that denies network egress.
func networkDenyWrapper() ([]string, error) {
	switch runtime.GOOS {
	case "darwin":
		if p, err := exec.LookPath("sandbox-exec"); err == nil {
			// Allow filesystem/process by default, deny all network.
			profile := "(version 1)(allow default)(deny network-outbound)(deny network-inbound)"
			return []string{p, "-p", profile}, nil
		}
		return nil, errors.New("network isolation unavailable: sandbox-exec not found on this macOS host")
	case "linux":
		// unshare -n creates an empty network namespace (no interfaces => no egress).
		// -r maps the current user to root inside so it works unprivileged.
		if p, err := exec.LookPath("unshare"); err == nil {
			return []string{p, "-r", "-n", "--"}, nil
		}
		return nil, errors.New("network isolation unavailable: unshare not found; install util-linux or use the gvisor driver")
	default:
		return nil, fmt.Errorf("local sandbox network isolation unsupported on %s; use the gvisor driver on Linux", runtime.GOOS)
	}
}

type localSandbox struct {
	id  string
	dir string
	drv *LocalDriver
}

func (s *localSandbox) ID() string { return s.id }

func (s *localSandbox) Exec(ctx context.Context, req ExecRequest) (ExecResult, error) {
	if len(req.Cmd) == 0 {
		return ExecResult{}, errors.New("empty command")
	}
	// Write input files into the sandbox dir.
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

	full := append(append([]string{}, s.drv.wrapper...), req.Cmd...)
	cmd := exec.CommandContext(runCtx, full[0], full[1:]...)
	cmd.Dir = s.dir
	// Minimal, network-free environment. No proxy vars, no DNS hints.
	cmd.Env = []string{
		"PATH=/usr/bin:/bin:/usr/local/bin",
		"HOME=" + s.dir,
		"TMPDIR=" + s.dir,
		"PYTHONDONTWRITEBYTECODE=1",
		"FARADAY_SANDBOX=1",
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	setProcAttrs(cmd) // platform-specific: new process group + best-effort limits

	start := time.Now()
	err := cmd.Run()
	dur := time.Since(start)

	res := ExecResult{
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		DurationMs: dur.Milliseconds(),
	}
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
		return res, fmt.Errorf("exec: %w", err)
	}
	res.ExitCode = 0
	return res, nil
}

func (s *localSandbox) Reset() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		_ = os.RemoveAll(filepath.Join(s.dir, e.Name()))
	}
	return nil
}

func (s *localSandbox) Destroy() error { return os.RemoveAll(s.dir) }

func randID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
