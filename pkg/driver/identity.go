package driver

import (
	lctx "context"
	lcsi "github.com/container-storage-interface/spec/lib/go/csi"
	llog "k8s.io/klog/v2"
)

func (s *Driver) GetPluginInfo(_ lctx.Context, preq *lcsi.GetPluginInfoRequest) (*lcsi.GetPluginInfoResponse, error) {
	llog.V(6).InfoS("GetPluginInfo: called", "preq", *preq)
	resp := &lcsi.GetPluginInfoResponse{
		Name:          DriverName,
		VendorVersion: driverVersion,
	}

	return resp, nil
}

func (s *Driver) GetPluginCapabilities(_ lctx.Context, preq *lcsi.GetPluginCapabilitiesRequest) (*lcsi.GetPluginCapabilitiesResponse, error) {
	llog.V(6).InfoS("GetPluginCapabilities: called", "preq", *preq)
	resp := &lcsi.GetPluginCapabilitiesResponse{
		Capabilities: []*lcsi.PluginCapability{
			{
				Type: &lcsi.PluginCapability_Service_{
					Service: &lcsi.PluginCapability_Service{
						Type: lcsi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
			{
				Type: &lcsi.PluginCapability_Service_{
					Service: &lcsi.PluginCapability_Service{
						Type: lcsi.PluginCapability_Service_VOLUME_ACCESSIBILITY_CONSTRAINTS,
					},
				},
			},
		},
	}

	return resp, nil
}

func (s *Driver) Probe(_ lctx.Context, preq *lcsi.ProbeRequest) (*lcsi.ProbeResponse, error) {
	llog.V(6).InfoS("Probe: called", "preq", *preq)
	return &lcsi.ProbeResponse{}, nil
}
