package driver

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/mount-utils"

	"github.com/SynologyOpenSource/synology-csi/pkg/dsm/webapi"
	"github.com/SynologyOpenSource/synology-csi/pkg/models"
	"github.com/SynologyOpenSource/synology-csi/pkg/utils"
)

const (
	logoutTestIqn = "iqn.2000-01.com.synology:nas.pvc-logout"
	logoutTestIp  = "192.0.2.10"
)

// iscsiVolumeService finds one iSCSI volume and leaves every other call to
// lookupFailingService.
type iscsiVolumeService struct {
	lookupFailingService
}

func (s *iscsiVolumeService) GetVolume(ctx context.Context, volId string) (*models.K8sVolumeRespSpec, error) {
	return &models.K8sVolumeRespSpec{
		DsmIp:    logoutTestIp,
		VolumeId: volId,
		Protocol: utils.ProtocolIscsi,
		Target: webapi.TargetInfo{
			Iqn:        logoutTestIqn,
			MappedLuns: []webapi.MappedLun{{LunUuid: volId, MappingIndex: 1}},
		},
	}, nil
}

func unstageWith(t *testing.T, e *recordingExecutor) error {
	t.Helper()
	ns := &nodeServer{
		Driver:     testDriver(),
		dsmService: &iscsiVolumeService{},
		Mounter:    &mount.SafeFormatAndMount{Interface: mount.NewFakeMounter(nil)},
		Initiator:  newTestInitiator(e),
		tools:      NewTools(e),
	}
	_, err := ns.NodeUnstageVolume(context.Background(), &csi.NodeUnstageVolumeRequest{
		VolumeId:          "vol-1",
		StagingTargetPath: t.TempDir(),
	})
	return err
}

// The session list here is empty, as when `iscsiadm -m session` misses a
// session that is still logged in. Only the logout's own exit status says
// whether a session was there: a success reported over a session that is
// still logged in ends kubelet's retries, and nothing logs it out after that.
func TestNodeUnstageReportsTheLogoutOutcome(t *testing.T) {
	for _, c := range []struct {
		name              string
		failStatus        int
		fail              string
		wantCode          codes.Code
		wantRecordDeleted bool
	}{
		{"the logout succeeds", 0, "", codes.OK, true},
		{"the session is already gone", 21, "--logout", codes.OK, true},
		{"the session ended during the logout", 2, "--logout", codes.OK, true},
		{"the session list fails", 0, "-m session", codes.OK, true},
		{"the logout fails", 0, "--logout", codes.Internal, false},
		{"the logout fails with another status", 8, "--logout", codes.Internal, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := &recordingExecutor{fail: c.fail, failStatus: c.failStatus}

			err := unstageWith(t, e)

			if got := status.Code(err); got != c.wantCode {
				t.Errorf("NodeUnstageVolume returned %v (%v), want %v; ran: %v", got, err, c.wantCode, e.ran)
			}
			if got := e.find("-o delete") >= 0; got != c.wantRecordDeleted {
				t.Errorf("node record deleted = %v, want %v; ran: %v", got, c.wantRecordDeleted, e.ran)
			}
		})
	}
}

// Deleting the record of a session that is still logged in fails, and if it
// ever succeeded, the session would outlive the record that logs it out.
func TestLogoutKeepsTheRecordWhenTheLogoutFails(t *testing.T) {
	for _, c := range []struct {
		name          string
		sessionOutput string
	}{
		{"the session is listed", "tcp: [1] 192.0.2.10:3260,1 " + logoutTestIqn + " (non-flash)\n"},
		{"the session is not listed", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := &recordingExecutor{sessionOutput: c.sessionOutput, fail: "--logout"}

			err := newTestInitiator(e).logout(context.Background(), logoutTestIqn, logoutTestIp)

			if err == nil {
				t.Errorf("logout returned success after the logout failed; ran: %v", e.ran)
			}
			if e.find("-o delete") >= 0 {
				t.Errorf("deleted the node record after the logout failed; ran: %v", e.ran)
			}
		})
	}
}

// The flush before logout needs the device, and a session list that misses
// the session, or fails, must not hide it: the logout would then tear down a
// session whose last writes are still in the page cache. udev names the
// device after the target and the LUN, so the name alone finds it.
func TestUnstageFindsTheDeviceWithoutTheSessionList(t *testing.T) {
	for _, c := range []struct {
		name   string
		device string
		want   bool
	}{
		{"the device of the target and LUN", "ip-192.0.2.10:3260-iscsi-" + logoutTestIqn + "-lun-1", true},
		{"a device of another LUN", "ip-192.0.2.10:3260-iscsi-" + logoutTestIqn + "-lun-2", false},
		{"a device of a target whose name extends this one", "ip-192.0.2.10:3260-iscsi-" + logoutTestIqn + "-2-lun-1", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			device := filepath.Join(dir, c.device)
			if err := os.WriteFile(device, nil, 0600); err != nil {
				t.Fatal(err)
			}
			tools := tools{executor: &recordingExecutor{fail: "-m session"}, devDiskByPath: dir}

			got := tools.getExistedVolumeMountPath(logoutTestIqn, 1)

			if found := got == device; found != c.want {
				t.Errorf("getExistedVolumeMountPath() = %q with %s present, want found = %v", got, c.device, c.want)
			}
		})
	}
}
