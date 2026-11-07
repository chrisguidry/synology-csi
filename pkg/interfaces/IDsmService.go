// Copyright 2021 Synology Inc.

package interfaces

import (
	"context"

	"github.com/SynologyOpenSource/synology-csi/pkg/dsm/common"
	"github.com/SynologyOpenSource/synology-csi/pkg/dsm/webapi"
	"github.com/SynologyOpenSource/synology-csi/pkg/models"
)

// An interface for DSM service. A method that takes a context sends its DSM
// requests with it.

type IDsmService interface {
	AddDsm(client common.ClientInfo) error
	RemoveAllDsms()
	GetDsm(ctx context.Context, ip string) (*webapi.DSM, error)
	GetDsmsCount() int
	ListDsmVolumes(ctx context.Context, ip string) ([]webapi.VolInfo, error)
	CreateVolume(ctx context.Context, spec *models.CreateK8sVolumeSpec) (*models.K8sVolumeRespSpec, error)
	DeleteVolume(ctx context.Context, volId string) error
	// A non-nil error means the listing was incomplete: absence from the result
	// does not prove a volume is gone.
	ListVolumes(ctx context.Context) ([]*models.K8sVolumeRespSpec, error)
	// Returns (nil, nil) only when the volume is known not to exist.
	GetVolume(ctx context.Context, volId string) (*models.K8sVolumeRespSpec, error)
	ExpandVolume(ctx context.Context, volId string, newSize int64) (*models.K8sVolumeRespSpec, error)
	CreateSnapshot(ctx context.Context, spec *models.CreateK8sVolumeSnapshotSpec) (*models.K8sSnapshotRespSpec, error)
	DeleteSnapshot(ctx context.Context, snapshotUuid string) error
	ListAllSnapshots(ctx context.Context) []*models.K8sSnapshotRespSpec
	ListSnapshots(ctx context.Context, volId string) []*models.K8sSnapshotRespSpec
	GetVolumeByName(ctx context.Context, volName string) (*models.K8sVolumeRespSpec, error)
	GetSnapshotByName(ctx context.Context, snapshotName string) *models.K8sSnapshotRespSpec
}
