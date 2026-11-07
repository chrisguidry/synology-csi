package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SynologyOpenSource/synology-csi/pkg/models"
	"github.com/SynologyOpenSource/synology-csi/pkg/utils"
)

func iscsiVolumeSpec(name string) *models.CreateK8sVolumeSpec {
	return &models.CreateK8sVolumeSpec{
		K8sVolumeName:    name,
		BackendName:      models.GenBackendName(name),
		TargetName:       models.TargetPrefix + "-" + name,
		Location:         "/volume1",
		Size:             1 << 30,
		ThinProvisioning: true,
		Protocol:         utils.ProtocolIscsi,
	}
}

// createLikeTheController looks the volume up by name and creates it only
// when it is not there, as the controller's CreateVolume does.
func createLikeTheController(ctx context.Context, service *DsmService, name string) (*models.K8sVolumeRespSpec, error) {
	volume, err := service.GetVolumeByName(ctx, name)
	if err != nil || volume != nil {
		return volume, err
	}
	return service.CreateVolume(ctx, iscsiVolumeSpec(name))
}

// waitFor polls until done reports true, or fails the test.
func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A provisioner that times out calls CreateVolume again with the same name,
// while the DSM may still carry out the request the first call gave up on.
// The second call must end with the one LUN, and the first call's rollback
// must still run after its context is done.
func TestCreateVolumeAfterATimedOutCallMakesOneLun(t *testing.T) {
	for _, stalled := range []struct {
		api, method      string
		lunsAfterFailure int
	}{
		// The DSM made the LUN after the call gave up, so nothing knew to
		// roll it back. The retry adopts it by name.
		{"SYNO.Core.ISCSI.LUN", "create", 1},
		// The LUN was made before the stall, and the rollback removes it.
		{"SYNO.Core.ISCSI.Target", "create", 0},
		{"SYNO.Core.ISCSI.LUN", "map_target", 0},
	} {
		t.Run(stalled.api+" "+stalled.method, func(t *testing.T) {
			fake := newFakeDSM()
			release := make(chan struct{})
			var first atomic.Bool
			first.Store(true)
			fake.stall = func(api, method string) {
				if api == stalled.api && method == stalled.method && first.CompareAndSwap(true, false) {
					<-release
				}
			}
			service := serviceFor(t, fake)

			timedOut, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			_, err := createLikeTheController(timedOut, service, "pvc-1")
			cancel()
			if err == nil {
				t.Fatal("the first CreateVolume succeeded past its deadline")
			}
			close(release)
			waitFor(t, "the stalled request to finish", func() bool { return fake.count(stalled.api, stalled.method) == 1 })
			if got := fake.lunCount(); got != stalled.lunsAfterFailure {
				t.Errorf("%d LUNs after the timed-out call, want %d", got, stalled.lunsAfterFailure)
			}

			volume, err := createLikeTheController(context.Background(), service, "pvc-1")

			if err != nil || volume == nil {
				t.Fatalf("the retried CreateVolume = (%v, %v), want the volume", volume, err)
			}
			if got := fake.lunCount(); got != 1 {
				t.Errorf("%d LUNs after the retry, want 1", got)
			}
			again, err := service.GetVolumeByName(context.Background(), "pvc-1")
			if err != nil || again == nil || again.VolumeId != volume.VolumeId {
				t.Errorf("GetVolumeByName after the retry = (%v, %v), want %s", again, err, volume.VolumeId)
			}
		})
	}
}
