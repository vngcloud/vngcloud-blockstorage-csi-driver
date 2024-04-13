package cloud

import (
	lsdkErr "github.com/vngcloud/vngcloud-go-sdk/error"
	lsdkObj "github.com/vngcloud/vngcloud-go-sdk/vngcloud/objects"
	lsnapshotV2 "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/blockstorage/v2/snapshot"
	pvolv2 "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/blockstorage/v2/volume"
)

type Cloud interface {
	GetVolumesByName(n string) ([]*lsdkObj.Volume, error)
	CreateVolume(popts *pvolv2.CreateOpts) (*lsdkObj.Volume, error)
	GetVolume(volumeID string) (*lsdkObj.Volume, *lsdkErr.SdkError)
	DeleteVolume(volID string) error
	AttachVolume(instanceID, volumeID string) (string, error)
	DetachVolume(instanceID, volumeID string) error
	ExpandVolume(volumeTypeID, volumeID string, newSize uint64) error
	ResizeOrModifyDisk(volumeID string, newSizeBytes int64, options *ModifyDiskOptions) (newSize int64, err error)
	GetDeviceDiskID(pvolID string) (string, error)
	GetVolumeSnapshotByName(pvolID, psnapshotName string) (*lsdkObj.Snapshot, error)
	CreateSnapshotFromVolume(pvolID string, popts *lsnapshotV2.CreateOpts) (*lsdkObj.Snapshot, error)
	DeleteSnapshot(psnapshotID string) error
}
