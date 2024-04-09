package driver

import (
	"errors"
	"fmt"
	lcsi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud"
	"k8s.io/klog/v2"
	"strconv"
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
		case ModificationVolumeSize:
			volumeSize, err := strconv.Atoi(value)
			if err != nil {
				return nil, ErrParsingVolumeSize
			}
			options.VolumeSize = volumeSize
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
