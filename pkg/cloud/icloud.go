package cloud

import (
	lsdkVolumeV2 "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/services/volume/v2"

	lsentity "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud/entity"
	lserr "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud/errors"
)

type Cloud interface {
	EitherCreateResizeVolume(preq lsdkVolumeV2.ICreateBlockVolumeRequest) (*lsentity.Volume, lserr.IError)
	GetVolumeByName(pvolName string) (*lsentity.Volume, lserr.IError)
	GetVolume(volumeID string) (*lsentity.Volume, lserr.IError)
	DeleteVolume(volID string) lserr.IError
	AttachVolume(instanceID, volumeID string) (*lsentity.Volume, lserr.IError)
	DetachVolume(instanceID, volumeID string) lserr.IError
	ModifyVolumeType(pvolumeId, pvolumeType string, psize int) lserr.IError
	ResizeOrModifyDisk(volumeID string, newSizeBytes int64, options *ModifyDiskOptions) (newSize int64, err error)
	ExpandVolume(volumeID, volumeTypeID string, newSize uint64) error
	GetDeviceDiskID(pvolID string) (string, error)
	GetVolumeSnapshotByName(pvolID, psnapshotName string) (*lsentity.Snapshot, error)
	CreateSnapshotFromVolume(pclusterId, pvolId, psnapshotName string) (*lsentity.Snapshot, error)
	DeleteSnapshot(psnapshotID string) error
	ListSnapshots(pvolID string, ppage int, ppageSize int) (*lsentity.ListSnapshots, lserr.IError)
	GetVolumeTypeById(pvolTypeId string) (*lsentity.VolumeType, lserr.IError)
	GetDefaultVolumeType() (*lsentity.VolumeType, lserr.IError)
	GetVolumeTypeIdByName(zoneId string, volumeName string) (string, lserr.IError)
}
