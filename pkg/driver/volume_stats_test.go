package driver

import (
	"context"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/mount-utils"
)

// statsNodeServer is a node server with no DSM behind it. dsmService is nil,
// so a stats call that reaches the DSM panics and fails the test.
func statsNodeServer(mountPoints ...string) *nodeServer {
	mps := []mount.MountPoint{}
	for _, p := range mountPoints {
		mps = append(mps, mount.MountPoint{Path: p, Type: "ext4"})
	}
	return &nodeServer{
		Driver:  testDriver(),
		Mounter: &mount.SafeFormatAndMount{Interface: mount.NewFakeMounter(mps)},
	}
}

func usageOf(resp *csi.NodeGetVolumeStatsResponse, unit csi.VolumeUsage_Unit) *csi.VolumeUsage {
	for _, u := range resp.GetUsage() {
		if u.GetUnit() == unit {
			return u
		}
	}
	return nil
}

func TestNodeGetVolumeStatsReadsTheMountWithoutTheDSM(t *testing.T) {
	volumePath := t.TempDir()
	ns := statsNodeServer(volumePath)

	resp, err := ns.NodeGetVolumeStats(context.Background(), &csi.NodeGetVolumeStatsRequest{
		VolumeId:   "vol-1",
		VolumePath: volumePath,
	})
	if err != nil {
		t.Fatalf("NodeGetVolumeStats: %v", err)
	}

	bytes := usageOf(resp, csi.VolumeUsage_BYTES)
	if bytes == nil || bytes.GetTotal() <= 0 || bytes.GetAvailable() > bytes.GetTotal() {
		t.Errorf("bytes usage = %v, want a positive total with available no larger than it", bytes)
	}
	inodes := usageOf(resp, csi.VolumeUsage_INODES)
	if inodes == nil || inodes.GetTotal() <= 0 {
		t.Errorf("inodes usage = %v, want a positive total", inodes)
	}
}

func TestNodeGetVolumeStatsReportsAnUnmountedPathAsNotFound(t *testing.T) {
	ns := statsNodeServer()

	_, err := ns.NodeGetVolumeStats(context.Background(), &csi.NodeGetVolumeStatsRequest{
		VolumeId:   "vol-1",
		VolumePath: t.TempDir(),
	})

	if status.Code(err) != codes.NotFound {
		t.Errorf("err = %v, want NotFound", err)
	}
}
