package cloud

import (
	lerr "errors"
)

var (
	ErrDeviceVolumeIdNotFound = lerr.New("device volume id not found")
	ErrInvalidArgument        = lerr.New("invalid argument")
	ErrSnapshotNotFound       = lerr.New("snapshot not found")
)
