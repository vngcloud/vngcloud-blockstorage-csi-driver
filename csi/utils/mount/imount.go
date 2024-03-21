package mount

import (
	"k8s.io/mount-utils"
)

type IMount interface {
	Mounter() *mount.SafeFormatAndMount
	GetDeviceStats(path string) (*DeviceStats, error)
	GetDevicePath(volumeID string) (string, error)
	IsLikelyNotMountPointAttach(targetpath string) (bool, error)
	UnmountPath(mountPath string) error
	MakeFile(pathname string) error
	MakeDir(pathname string) error
	GetMountFs(path string) ([]byte, error)
}

type DeviceStats struct {
	Block bool

	AvailableBytes  int64
	TotalBytes      int64
	UsedBytes       int64
	AvailableInodes int64
	TotalInodes     int64
	UsedInodes      int64
}
