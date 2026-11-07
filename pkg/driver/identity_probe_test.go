package driver

import (
	"context"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

// Probe reports the process only. A liveness check that failed while the
// DSM was down would restart every plugin each time the NAS reboots, and
// synology_csi_dsm_reachable reports the DSM instead.
func TestProbeAnswersWithNoDSM(t *testing.T) {
	ids := &identityServer{Driver: &Driver{DsmService: &lookupFailingService{}}}

	if _, err := ids.Probe(context.Background(), &csi.ProbeRequest{}); err != nil {
		t.Errorf("Probe = %v, want success while the DSM cannot be listed", err)
	}
}
