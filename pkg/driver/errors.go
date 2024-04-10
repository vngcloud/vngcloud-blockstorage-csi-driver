package driver

import (
	lcodes "google.golang.org/grpc/codes"
	lstt "google.golang.org/grpc/status"
)

var (
	ErrVolumeNameNotProvided          = lstt.Error(lcodes.InvalidArgument, "Volume name not provided")
	ErrVolumeCapabilitiesNotProvided  = lstt.Error(lcodes.InvalidArgument, "Volume capabilities not provided")
	ErrVolumeCapabilitiesNotSupported = lstt.Error(lcodes.InvalidArgument, "Volume capabilities not supported")
	ErrVolumeSizeExceedLimit          = lstt.Error(lcodes.InvalidArgument, "After round-up, volume size exceeds the limit specified")
	ErrParsingVolumeSize              = lstt.Errorf(lcodes.InvalidArgument, "Could not parse volume size")
	ErrModifyMutableParam             = lstt.Errorf(lcodes.InvalidArgument, "Invalid mutable parameters")
	ErrVolumeIDNotProvided            = lstt.Errorf(lcodes.InvalidArgument, "Volume ID not provided")
	ErrNodeIdNotProvided              = lstt.Error(lcodes.InvalidArgument, "Node ID not provided")
	ErrStagingTargetPathNotProvided   = lstt.Error(lcodes.InvalidArgument, "Staging target not provided")
)
