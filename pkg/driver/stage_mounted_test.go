package driver

import (
	"context"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"k8s.io/mount-utils"

	"github.com/SynologyOpenSource/synology-csi/pkg/utils"
)

func stageRequest(protocol, stagingPath string) *csi.NodeStageVolumeRequest {
	return &csi.NodeStageVolumeRequest{
		VolumeId:          "vol-1",
		StagingTargetPath: stagingPath,
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{FsType: "ext4"}},
			AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
		},
		VolumeContext: map[string]string{"protocol": protocol, "source": "//1.2.3.4/k8s-csi-pvc-1"},
		Secrets:       map[string]string{"username": "k8s", "password": "secret"},
	}
}

// A kubelet that restarts stages every volume again. A staging path that is
// already mounted answers at once, with no DSM call and no iSCSI login, so a
// DSM that is down or slow then leaves the node's volumes as they are. The
// service here fails every DSM call, so only an answer that made none can
// succeed.
func TestStagingAMountedPathMakesNoDSMCall(t *testing.T) {
	for _, protocol := range []string{utils.ProtocolIscsi, utils.ProtocolNvme, utils.ProtocolSmb, ""} {
		t.Run("protocol="+protocol, func(t *testing.T) {
			stagingPath := t.TempDir()
			ns := &nodeServer{
				Driver:     testDriver(),
				dsmService: &lookupFailingService{},
				Mounter:    &mount.SafeFormatAndMount{Interface: mount.NewFakeMounter([]mount.MountPoint{{Path: stagingPath}})},
			}

			_, err := ns.NodeStageVolume(context.Background(), stageRequest(protocol, stagingPath))

			if err != nil {
				t.Errorf("NodeStageVolume on a mounted path = %v, want success", err)
			}
		})
	}
}

// A staging path that is not mounted still goes to the DSM, so the check
// skips only the work that is already done.
func TestStagingAnUnmountedPathStillLooksTheVolumeUp(t *testing.T) {
	for _, protocol := range []string{utils.ProtocolIscsi, utils.ProtocolNvme, utils.ProtocolSmb} {
		t.Run("protocol="+protocol, func(t *testing.T) {
			ns := &nodeServer{
				Driver:     testDriver(),
				dsmService: &lookupFailingService{},
				Mounter:    &mount.SafeFormatAndMount{Interface: mount.NewFakeMounter(nil)},
			}

			_, err := ns.NodeStageVolume(context.Background(), stageRequest(protocol, t.TempDir()))

			if err == nil {
				t.Error("NodeStageVolume succeeded without the DSM, want the lookup's error")
			}
		})
	}
}
