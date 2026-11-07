package driver

import (
	"context"
	"testing"
)

// A login that was killed can finish in iscsid after the call ends, which
// leaves a session whose node record never got its startup set to manual.
// On a host whose default is automatic, a reboot would then log in before
// the kubelet asks for the volume, so a login that finds the session still
// sets it.
func TestLoginToAnExistingSessionSetsManualStartup(t *testing.T) {
	e := &recordingExecutor{sessionOutput: sessionLine(testIqn)}
	if err := newTestInitiator(e).login(context.Background(), testIqn, testPortal); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if e.find("node.startup --value manual") < 0 {
		t.Errorf("expected node.startup to be set to manual, ran: %v", e.ran)
	}
	if e.find("--login") >= 0 {
		t.Errorf("did not expect a second login to a session that exists, ran: %v", e.ran)
	}
}
