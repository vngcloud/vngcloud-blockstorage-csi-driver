package vcontainer

import (
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/utils/metadata"
	"github.com/vngcloud/vngcloud-go-sdk/vngcloud/objects"
)

type IVContainer interface {
	GetMetadataOpts() metadata.Opts
	SetupPortalInfo(metadata metadata.IMetadata) error
	ListVolumes(limit int, startingToken string) ([]*objects.Volume, string, error)
	GetVolumesByName(n string) ([]*objects.Volume, error)
	GetVolume(volumeID string) (*objects.Volume, error)
	CreateVolume(name string, size uint64, vtype, availability string, snapshotID string, sourcevolID string, tags *map[string]string) (*objects.Volume, error)
	DeleteVolume(volID string) error
	GetInstanceByID(instanceID string) (*objects.Server, error)
	AttachVolume(instanceID, volumeID string) (string, error)
	GetAttachmentDiskPath(instanceID, volumeID string) (string, error)
	WaitDiskAttached(instanceID string, volumeID string) error
	DetachVolume(instanceID, volumeID string) error
	WaitDiskDetached(instanceID string, volumeID string) error
	ExpandVolume(volumeTypeID, volumeID string, newSize uint64) error
	WaitVolumeTargetStatus(volumeID string, tStatus []string) error
	GetMaxVolLimit() int64
	GetBlockStorageOpts() BlockStorageOpts
	ListSnapshots(page string, size int, volumeID, status, name string) ([]*objects.Snapshot, string, error)
	GetSnapshotByID(snapshotID string) (*objects.Snapshot, error)
	CreateSnapshot(name, volID string) (*objects.Snapshot, error)
	WaitSnapshotReady(snapshotID string) error
	DeleteSnapshot(volumeID, snapshotID string) error
	GetMappingVolume(volumeID string) (string, error)
}
