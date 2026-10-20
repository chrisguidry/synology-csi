package driver

import (
	"context"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

func TestTheDriverRegistersUnderTheNameItWasGiven(t *testing.T) {
	cases := []string{DriverName, "canary.csi.san.synology.com"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			d, err := NewControllerAndNodeDriver(name, "node-1", "unix:///csi.sock", nil, tools{})
			if err != nil {
				t.Fatalf("NewControllerAndNodeDriver: %v", err)
			}

			info, err := NewIdentityServer(d).GetPluginInfo(context.Background(), &csi.GetPluginInfoRequest{})
			if err != nil {
				t.Fatalf("GetPluginInfo: %v", err)
			}
			if info.GetName() != name {
				t.Errorf("GetPluginInfo name = %q, want %q", info.GetName(), name)
			}
		})
	}
}
