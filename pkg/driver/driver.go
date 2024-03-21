package driver

import (
	"fmt"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
	"time"
)

const (
	DriverName = "bs.csi.vngcloud.vn"
)

// Mode is the operating mode of the CSI driver.
type Mode string

const (
	// ControllerMode is the mode that only starts the controller service.
	ControllerMode Mode = "controller"
	// NodeMode is the mode that only starts the node service.
	NodeMode Mode = "node"
	// AllMode is the mode that only starts both the controller and the node service.
	AllMode Mode = "all"
)

type Driver struct {
	controllerService
	nodeService

	srv     *grpc.Server
	options *DriverOptions
}

type DriverOptions struct {
	endpoint                          string
	mode                              Mode
	modifyVolumeRequestHandlerTimeout time.Duration
}

func NewDriver(options ...func(driverOptions *DriverOptions)) (*Driver, error) {
	klog.InfoS("Driver Information", "Driver", DriverName, "Version", driverVersion)

	driverOptions := DriverOptions{
		endpoint:                          DefaultCSIEndpoint,
		mode:                              AllMode,
		modifyVolumeRequestHandlerTimeout: DefaultModifyVolumeRequestHandlerTimeout,
	}

	for _, option := range options {
		option(&driverOptions)
	}

	if err := ValidateDriverOptions(&driverOptions); err != nil {
		return nil, fmt.Errorf("Invalid driver options: %w", err)
	}

	driver := Driver{
		options: &driverOptions,
	}

	switch driverOptions.mode {
	case ControllerMode:
		driver.controllerService = newControllerService(&driverOptions)
	case NodeMode:
		driver.nodeService = newNodeService(&driverOptions)
	case AllMode:
		driver.controllerService = newControllerService(&driverOptions)
		driver.nodeService = newNodeService(&driverOptions)
	default:
		return nil, fmt.Errorf("unknown mode: %s", driverOptions.mode)
	}

	return &driver, nil
}
