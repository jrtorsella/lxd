package openvswitch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeNbctl puts an ovn-nbctl on PATH that counts its runs in a file, and fails with the given message on stderr
// for the first failures of them.
func fakeNbctl(t *testing.T, failures int, message string) (countPath string) {
	t.Helper()

	dir := t.TempDir()
	countPath = filepath.Join(dir, "runs")
	script := fmt.Sprintf(`#!/bin/sh
runs=$(cat %[1]q 2>/dev/null || echo 0)
runs=$((runs + 1))
echo "$runs" > %[1]q
if [ "$runs" -le %[2]d ]; then
	echo %[3]q >&2
	exit 1
fi
echo done
`, countPath, failures, message)

	err := os.WriteFile(filepath.Join(dir, "ovn-nbctl"), []byte(script), 0o755)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	return countPath
}

func runs(t *testing.T, countPath string) string {
	t.Helper()

	content, err := os.ReadFile(countPath)
	if err != nil {
		t.Fatal(err)
	}

	return strings.TrimSpace(string(content))
}

func TestXbctlRetriesConnectFailure(t *testing.T) {
	delay := xbctlConnectRetryDelay
	xbctlConnectRetryDelay = time.Millisecond
	t.Cleanup(func() { xbctlConnectRetryDelay = delay })

	o := &OVN{nbDBAddr: "tcp:192.0.2.1:6641"}

	// A command that cannot connect is run again until it can.
	countPath := fakeNbctl(t, 2, "ovn-nbctl: tcp:192.0.2.1:6641: database connection failed ()")
	out, err := o.nbctl("show")
	if err != nil {
		t.Fatalf("Expected the command to succeed on its third run: %v", err)
	}

	if strings.TrimSpace(out) != "done" || runs(t, countPath) != "3" {
		t.Fatalf("Expected output %q after 3 runs, got %q after %s", "done", out, runs(t, countPath))
	}

	// One that never connects is given up on.
	countPath = fakeNbctl(t, 9, "ovn-nbctl: tcp:192.0.2.1:6641: database connection failed ()")
	_, err = o.nbctl("show")
	if err == nil || !strings.Contains(err.Error(), "database connection failed") {
		t.Fatalf("Expected the connection failure to be returned, got: %v", err)
	}

	if runs(t, countPath) != "5" {
		t.Fatalf("Expected %d runs, got %s", xbctlConnectAttempts, runs(t, countPath))
	}

	// A command the database refused is not run again: it may have taken effect in part.
	countPath = fakeNbctl(t, 9, "ovn-nbctl: transaction error: constraint violation")
	_, err = o.nbctl("show")
	if err == nil {
		t.Fatal("Expected the refusal to be returned")
	}

	if runs(t, countPath) != "1" {
		t.Fatalf("Expected a refused command to run once, got %s", runs(t, countPath))
	}
}
