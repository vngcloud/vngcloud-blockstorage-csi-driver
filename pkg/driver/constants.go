package driver

import "time"

const (
	DefaultCSIEndpoint                       = "unix://tmp/csi.sock"
	DefaultModifyVolumeRequestHandlerTimeout = 2 * time.Second
	AgentNotReadyNodeTaintKey                = "bs.csi.vngcloud.vn/agent-not-ready"
)

const (
	volumeCreatingInProgress = "Create volume request for %s is already in progress"
)
