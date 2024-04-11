package driver

import lcsi "github.com/container-storage-interface/spec/lib/go/csi"

func newControllerPublishVolumeResponse(pdevicePath string) *lcsi.ControllerPublishVolumeResponse {
	return &lcsi.ControllerPublishVolumeResponse{
		PublishContext: map[string]string{
			DevicePathKey: pdevicePath,
		},
	}
}
