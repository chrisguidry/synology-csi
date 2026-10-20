package main

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestPathFlagsNameTheHostCommands(t *testing.T) {
	cases := []struct {
		flag    string
		command string
	}{
		{"--iscsiadm-path=/sbin/iscsiadm", "iscsiadm"},
		{"--blockdev-path=/sbin/blockdev", "blockdev"},
	}
	for _, c := range cases {
		t.Run(c.command, func(t *testing.T) {
			cmd := &cobra.Command{}
			addFlags(cmd)

			if err := cmd.ParseFlags([]string{c.flag}); err != nil {
				t.Fatalf("parse %s: %v", c.flag, err)
			}

			got := hostCommands()[c.command]
			want := c.flag[len("--"+c.command+"-path="):]
			if got != want {
				t.Errorf("hostCommands()[%q] = %q, want %q", c.command, got, want)
			}
		})
	}
}
