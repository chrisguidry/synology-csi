package driver

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SynologyOpenSource/synology-csi/pkg/utils/hostexec"
)

// hungCommand returns tools whose command never returns, as when iscsid
// stops answering its socket or an NVMe target stops answering.
func hungCommand(t *testing.T, command string) tools {
	t.Helper()
	script := filepath.Join(t.TempDir(), command)
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	executor, err := hostexec.New(map[string]string{command: script}, "")
	if err != nil {
		t.Fatal(err)
	}
	hung := NewTools(executor)
	hung.commandTimeout = 100 * time.Millisecond
	return hung
}

// An iscsiadm that hangs is stopped, so a stage or an unstage fails and the
// kubelet calls again, where it would otherwise hold the call forever.
func TestAHungIscsiadmIsStopped(t *testing.T) {
	for _, c := range []struct {
		name string
		run  func(tools) error
	}{
		{"login", func(t tools) error { return t.iscsiadm_login(testIqn, testPortal) }},
		{"logout", func(t tools) error { return t.iscsiadm_logout(testIqn) }},
		{"discovery", func(t tools) error { return t.iscsiadm_discovery(testPortal) }},
		{"delete node", func(t tools) error { return t.iscsiadm_delete_node(testIqn, testPortal) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			hung := hungCommand(t, "iscsiadm")
			started := time.Now()

			err := c.run(hung)

			if err == nil {
				t.Error("a hung iscsiadm reported success")
			}
			if elapsed := time.Since(started); elapsed > 10*time.Second {
				t.Errorf("the hung iscsiadm ran for %v", elapsed)
			}
		})
	}
}

// A hung nvme is stopped the same way.
func TestAHungNvmeIsStopped(t *testing.T) {
	for _, c := range []struct {
		name string
		run  func(tools) error
	}{
		{"discover", func(t tools) error { return t.nvmeDiscover("1.2.3.4", NVMePort, "tcp", "") }},
		{"connect", func(t tools) error { return t.nvmeConnect("1.2.3.4", NVMePort, "tcp", "nqn.test", "") }},
		{"disconnect", func(t tools) error { return t.nvmeDisconnect("nqn.test") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			hung := hungCommand(t, "nvme")
			started := time.Now()

			err := c.run(hung)

			if err == nil {
				t.Error("a hung nvme reported success")
			}
			if elapsed := time.Since(started); elapsed > 10*time.Second {
				t.Errorf("the hung nvme ran for %v", elapsed)
			}
		})
	}
}
