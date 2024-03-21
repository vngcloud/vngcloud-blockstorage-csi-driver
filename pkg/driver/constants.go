package driver

import "time"

const (
	DefaultCSIEndpoint                       = "unix://tmp/csi.sock"
	DefaultModifyVolumeRequestHandlerTimeout = 2 * time.Second
)
