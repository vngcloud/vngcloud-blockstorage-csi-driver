package server

import "github.com/container-storage-interface/spec/lib/go/csi"

type INonBlockingGRPCServer interface {
	Start(endpoint string, ids csi.IdentityServer, cs csi.ControllerServer, ns csi.NodeServer)
	Wait()
}
