//go:build e2e

package e2e

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
)

// run executes a command and returns its combined output. The output is also
// part of the error, so a failed command explains itself.
func run(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

// ensure runs a command that may already be applied from an earlier run.
func ensure(name string, args ...string) error {
	_, err := run(name, args...)
	if err != nil && !strings.Contains(err.Error(), "AlreadyExists") {
		return err
	}
	return nil
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
