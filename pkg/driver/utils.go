package driver

import (
	lcsi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/cuongpiger/joat/math"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud"
)

func getVolSizeBytes(preq *lcsi.CreateVolumeRequest) (volSizeBytes int64) {
	// get the volume size that user provided
	if preq.GetCapacityRange() != nil {
		volSizeBytes = preq.GetCapacityRange().GetRequiredBytes()
	}

	return math.MaxNumeric(volSizeBytes, cloud.DefaultVolumeSize)
}
