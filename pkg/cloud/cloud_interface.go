package cloud

import "github.com/vngcloud/vngcloud-go-sdk/vngcloud/objects"

type Cloud interface {
	GetVolumesByName(n string) ([]*objects.Volume, error)
	CreateVolume(name string, size uint64, vtype, availability string, snapshotID string, sourcevolID string, tags *map[string]string) (*objects.Volume, error)
	GetVolume(volumeID string) (*objects.Volume, error)
	DeleteVolume(volID string) error
	AttachVolume(instanceID, volumeID string) (string, error)
	WaitDiskAttached(instanceID string, volumeID string) error
	GetAttachmentDiskPath(instanceID, volumeID string) (string, error)
	DetachVolume(instanceID, volumeID string) error
	WaitDiskDetached(instanceID string, volumeID string) error
	ExpandVolume(volumeTypeID, volumeID string, newSize uint64) error
	WaitVolumeTargetStatus(volumeID string, tStatus []string) error
	ResizeOrModifyDisk(volumeID string, newSizeBytes int64, options *ModifyDiskOptions) (newSize int64, err error)
}
