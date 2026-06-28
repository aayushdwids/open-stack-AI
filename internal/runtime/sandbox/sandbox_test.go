package sandbox

import (
	"context"
	"net"
	"os/exec"
	"strings"
	"testing"
)

func newLocal(t *testing.T) Driver {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	d, err := NewLocalDriver(Config{Network: "none", DefaultTimeoutSecs: 20})
	if err != nil {
		t.Skipf("local driver unavailable: %v", err)
	}
	return d
}

func TestSandboxRunsCode(t *testing.T) {
	d := newLocal(t)
	sb, err := d.New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer sb.Destroy()
	res, err := sb.Exec(context.Background(), ExecRequest{
		Files: map[string]string{"main.py": "print('hello')\n"},
		Cmd:   []string{"python3", "main.py"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || !strings.Contains(res.Stdout, "hello") {
		t.Fatalf("unexpected result: %+v", res)
	}
}

// TestSandboxNetworkDenied is the air-gap guarantee: a connect from inside the sandbox to
// a listener that IS reachable from the host must fail because the sandbox has no egress.
func TestSandboxNetworkDenied(t *testing.T) {
	d := newLocal(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)

	sb, _ := d.New(context.Background())
	defer sb.Destroy()
	code := "import socket,sys\n" +
		"try:\n" +
		"    s=socket.create_connection(('127.0.0.1'," + itoa(addr.Port) + "),timeout=3)\n" +
		"    print('CONNECTED'); s.close()\n" +
		"except Exception as e:\n" +
		"    print('BLOCKED', type(e).__name__); sys.exit(7)\n"
	res, err := sb.Exec(context.Background(), ExecRequest{Files: map[string]string{"net.py": code}, Cmd: []string{"python3", "net.py"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Stdout, "CONNECTED") {
		t.Fatalf("network egress was NOT blocked: %s", res.Stdout)
	}
	if !strings.Contains(res.Stdout, "BLOCKED") {
		t.Fatalf("expected BLOCKED, got stdout=%q stderr=%q", res.Stdout, res.Stderr)
	}
}

func TestSandboxTimeoutTerminates(t *testing.T) {
	d := newLocal(t)
	sb, _ := d.New(context.Background())
	defer sb.Destroy()
	res, err := sb.Exec(context.Background(), ExecRequest{
		Files:       map[string]string{"loop.py": "while True:\n    pass\n"},
		Cmd:         []string{"python3", "loop.py"},
		TimeoutSecs: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut {
		t.Fatalf("expected timeout, got %+v", res)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
