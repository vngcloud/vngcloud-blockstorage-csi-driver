package driver

const (
	driverName                = "csi.vngcloud.vn"
	topologyKey               = "topology." + driverName + "/zone"
	vContainerCSIClusterIDKey = driverName + "/cluster"
	fsType                    = "ext4"
	layout                    = "2006-01-02T15:04:05.000Z07:00"
)

var (
	// CSI spec version
	specVersion = "1.8.0"
	Version     = "0.0.1"
)
