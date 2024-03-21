package driver

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/utils/metadata"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/utils/mount"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/utils/server"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/vcontainer/vcontainer"
	"k8s.io/klog/v2"
)

type Driver struct {
	name      string
	fqVersion string
	endpoint  string

	ids csi.IdentityServer
	cs  csi.ControllerServer
	ns  csi.NodeServer

	vcap  []*csi.VolumeCapability_AccessMode
	cscap []*csi.ControllerServiceCapability
	nscap []*csi.NodeServiceCapability
}

func (s *Driver) AddControllerServiceCapabilities(cl []csi.ControllerServiceCapability_RPC_Type) {
	csc := make([]*csi.ControllerServiceCapability, 0, len(cl))

	for _, c := range cl {
		klog.Infof("Enabling controller service capability: %v", c.String())
		csc = append(csc, NewControllerServiceCapability(c))
	}

	s.cscap = csc
}

func (s *Driver) AddVolumeCapabilityAccessModes(vc []csi.VolumeCapability_AccessMode_Mode) []*csi.VolumeCapability_AccessMode {
	vca := make([]*csi.VolumeCapability_AccessMode, 0, len(vc))

	for _, c := range vc {
		klog.Infof("Enabling volume access mode: %v", c.String())
		vca = append(vca, NewVolumeCapabilityAccessMode(c))
	}

	s.vcap = vca

	return vca
}

func (s *Driver) AddNodeServiceCapabilities(nl []csi.NodeServiceCapability_RPC_Type) {
	nsc := make([]*csi.NodeServiceCapability, 0, len(nl))

	for _, n := range nl {
		klog.Infof("Enabling node service capability: %v", n.String())
		nsc = append(nsc, NewNodeServiceCapability(n))
	}

	s.nscap = nsc
}

func (s *Driver) SetupDriver(cloud vcontainer.IVContainer, mounter mount.IMount, metadator metadata.IMetadata) {
	s.ids = NewIdentityServer(s)
	s.cs = NewControllerServer(s, metadator, cloud)
	s.ns = NewNodeServer(s, mounter, metadator, cloud)
}

func (s *Driver) Run() {
	sv := server.NewNonBlockingGRPCServer()
	sv.Start(s.endpoint, s.ids, s.cs, s.ns)
	sv.Wait()
}
