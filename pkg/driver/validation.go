package driver

import (
	"errors"
	"fmt"
	ljoat "github.com/cuongpiger/joat/parser"
	"strconv"
	"strings"

	lcsi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/vngcloud/vngcloud-csi-volume-modifier/pkg/rpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"

	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud"
)

func ValidateDriverOptions(options *DriverOptions) error {
	if err := validateMode(options.mode); err != nil {
		return fmt.Errorf("Invalid mode: %w", err)
	}

	if options.modifyVolumeRequestHandlerTimeout == 0 {
		return errors.New("Invalid modifyVolumeRequestHandlerTimeout: Timeout cannot be zero")
	}

	return nil
}

func validateMode(mode Mode) error {
	if mode != AllMode && mode != ControllerMode && mode != NodeMode {
		return fmt.Errorf("Mode is not supported (actual: %s, supported: %v)", mode, []Mode{AllMode, ControllerMode, NodeMode})
	}

	return nil
}

func validateCreateVolumeRequest(preq *lcsi.CreateVolumeRequest) error {
	// Get the pvc name, the volume name is in the format of pvc-<random-uuid>
	volName := preq.GetName()
	if len(volName) < 1 {
		return ErrVolumeNameNotProvided
	}

	// Get the volue capabilities
	volCaps := preq.GetVolumeCapabilities()
	if len(volCaps) < 0 {
		return ErrVolumeCapabilitiesNotProvided
	}

	// Check if the volume capabilities are supported
	if !isValidVolumeCapabilities(volCaps) {
		return ErrVolumeCapabilitiesNotSupported
	}
	return nil
}

func isValidVolumeCapabilities(pvolCap []*lcsi.VolumeCapability) bool {
	for _, c := range pvolCap {
		if !isValidCapability(c) {
			return false
		}
	}

	return true
}

func isValidCapability(pvolCap *lcsi.VolumeCapability) bool {
	accessMode := pvolCap.GetAccessMode().GetMode()

	//nolint:exhaustive
	switch accessMode {
	case SingleNodeWriter:
		return true

	case MultiNodeMultiWriter:
		if isBlock(pvolCap) {
			return true
		} else {
			klog.InfoS("isValidCapability: access mode is only supported for block devices", "accessMode", accessMode)
			return false
		}

	default:
		klog.InfoS("isValidCapability: access mode is not supported", "accessMode", accessMode)
		return false
	}
}

// isBlock checks if the volume capability is for block device
func isBlock(pvolCap *lcsi.VolumeCapability) bool {
	_, isBlockVol := pvolCap.GetAccessType().(*lcsi.VolumeCapability_Block)
	return isBlockVol
}

func isMultiAttach(pvolCaps []*lcsi.VolumeCapability) bool {
	for _, c := range pvolCaps {
		if c.GetAccessMode().GetMode() == MultiNodeMultiWriter && isBlock(c) {
			return true
		}
	}

	return false
}

func parseModifyVolumeParameters(params map[string]string) (*cloud.ModifyDiskOptions, error) {
	options := cloud.ModifyDiskOptions{}

	for key, value := range params {
		switch key {
		case ModificationKeyVolumeType:
			options.VolumeType = value
		}
	}

	return &options, nil
}

func validateDeleteVolumeRequest(req *lcsi.DeleteVolumeRequest) error {
	if len(req.GetVolumeId()) < 1 {
		return ErrVolumeIDNotProvided
	}
	return nil
}

func validateControllerPublishVolumeRequest(preq *lcsi.ControllerPublishVolumeRequest) error {
	if len(preq.GetVolumeId()) == 0 {
		return ErrVolumeIDNotProvided
	}

	if len(preq.GetNodeId()) == 0 {
		return ErrNodeIdNotProvided
	}

	volCap := preq.GetVolumeCapability()
	if volCap == nil {
		return ErrVolumeCapabilitiesNotProvided
	}

	if !isValidCapability(volCap) {
		return ErrVolumeCapabilitiesNotSupported
	}
	return nil
}

func validateControllerUnpublishVolumeRequest(req *lcsi.ControllerUnpublishVolumeRequest) error {
	if len(req.GetVolumeId()) == 0 {
		return ErrVolumeIDNotProvided
	}

	if len(req.GetNodeId()) == 0 {
		return ErrNodeIdNotProvided
	}

	return nil
}

func validateModifyVolumePropertiesRequest(req *rpc.ModifyVolumePropertiesRequest) error {
	name := req.GetName()
	if name == "" {
		return status.Error(codes.InvalidArgument, "Volume name not provided")
	}
	return nil
}

func isValidVolumeContext(volContext map[string]string) bool {
	//There could be multiple volume attributes in the volumeContext map
	//Validate here case by case
	if partition, ok := volContext[VolumeAttributePartition]; ok {
		partitionInt, err := strconv.ParseInt(partition, 10, 64)
		if err != nil {
			klog.ErrorS(err, "failed to parse partition as int", "partition", partition)
			return false
		}
		if partitionInt < 0 {
			klog.ErrorS(err, "invalid partition config", "partition", partition)
			return false
		}
	}
	return true
}

func recheckFormattingOptionParameter(context map[string]string, key string, fsConfigs map[string]fileSystemConfig, fsType string) (value string, err error) {
	v, ok := context[key]
	if ok {
		parser, _ := ljoat.GetParser()
		// This check is already performed on the controller side
		// However, because it is potentially security-sensitive, we redo it here to be safe
		if isAlphanumeric := parser.StringIsAlphanumeric(value); !isAlphanumeric {
			return "", status.Errorf(codes.InvalidArgument, "Invalid %s (aborting!): %v", key, err)
		}

		// In the case that the default fstype does not support custom sizes we could
		// be using an invalid fstype, so recheck that here
		if supported := fsConfigs[strings.ToLower(fsType)].isParameterSupported(key); !supported {
			return "", status.Errorf(codes.InvalidArgument, "Cannot use %s with fstype %s", key, fsType)
		}
	}
	return v, nil
}

func validateCreateSnapshotRequest(req *lcsi.CreateSnapshotRequest) error {
	if len(req.GetName()) == 0 {
		return ErrSnapshotNameNotProvided
	}

	if len(req.GetSourceVolumeId()) == 0 {
		return ErrSnapshotSourceVolumeNotProvided
	}
	return nil
}

func validateDeleteSnapshotRequest(req *lcsi.DeleteSnapshotRequest) error {
	if len(req.GetSnapshotId()) == 0 {
		return status.Error(codes.InvalidArgument, "Snapshot ID not provided")
	}
	return nil
}
