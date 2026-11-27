package driver

import (
	"context"
	"reflect"
	"testing"
)

const realSessionLine = "tcp: [3] 192.0.2.10:3260,1 iqn.2000-01.com.synology:nas.pvc-abc123 (non-flash)"

var realSession = iscsiSession{
	Protocol: "tcp",
	Id:       3,
	Portal:   "192.0.2.10:3260",
	Iqn:      "iqn.2000-01.com.synology:nas.pvc-abc123",
}

// iscsiadm prints warnings in the same stream as the sessions, and a warning
// must never be taken for a session or crash the plugin.
func TestParseSessions(t *testing.T) {
	second := iscsiSession{
		Protocol: "tcp",
		Id:       4,
		Portal:   "192.0.2.11:3260",
		Iqn:      "iqn.2000-01.com.synology:nas.pvc-def456",
	}
	for _, c := range []struct {
		name  string
		input string
		want  []iscsiSession
	}{
		{"one session", realSessionLine + "\n", []iscsiSession{realSession}},
		{"several sessions", realSessionLine + "\n" +
			"tcp: [4] 192.0.2.11:3260,1 iqn.2000-01.com.synology:nas.pvc-def456 (non-flash)\n",
			[]iscsiSession{realSession, second}},
		{"a warning line", "iscsiadm: could not read session targetname: 5\n", nil},
		{"a warning between sessions", realSessionLine + "\n" +
			"iscsiadm: could not read session targetname: 5\n" +
			"tcp: [4] 192.0.2.11:3260,1 iqn.2000-01.com.synology:nas.pvc-def456 (non-flash)\n",
			[]iscsiSession{realSession, second}},
		{"an EUI-64 target name", "tcp: [5] 192.0.2.10:3260,1 eui.02004567A425678D (non-flash)\n",
			[]iscsiSession{{Protocol: "tcp", Id: 5, Portal: "192.0.2.10:3260", Iqn: "eui.02004567A425678D"}}},
		{"an NAA target name", "tcp: [6] 192.0.2.10:3260,1 naa.52004567BA64678D (non-flash)\n",
			[]iscsiSession{{Protocol: "tcp", Id: 6, Portal: "192.0.2.10:3260", Iqn: "naa.52004567BA64678D"}}},
		{"no target name", "tcp: [3] 192.0.2.10:3260,1\n", nil},
		{"a portal with no tag", "tcp: [3] 192.0.2.10:3260 iqn.2000-01.com.synology:nas.pvc-abc123\n", nil},
		{"empty output", "", nil},
		{"blank lines", "\n\n", nil},
		{"garbage", "!!! not a session at all, really\n\x00\x01\n", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := parseSessions(c.input)

			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("parseSessions(%q) = %+v, want %+v", c.input, got, c.want)
			}
		})
	}
}

// Parallel unstages make iscsiadm print warnings beside the sessions, and
// the warnings are on stderr, which the session list never reads. The stray
// stderr line has a session's shape, so only reading stdout keeps it out.
func TestSessionListIgnoresIscsiadmStderr(t *testing.T) {
	e := &recordingExecutor{
		sessionOutput:  realSessionLine + "\n",
		sessionWarning: "tcp: [9] 192.0.2.99:3260,1 iqn.2000-01.com.synology:nas.stray (non-flash)\n",
	}
	tools := NewTools(e)

	got := tools.iscsiadm_session(context.Background())

	if !reflect.DeepEqual(got, []iscsiSession{realSession}) {
		t.Errorf("iscsiadm_session() = %+v, want %+v", got, []iscsiSession{realSession})
	}
}
