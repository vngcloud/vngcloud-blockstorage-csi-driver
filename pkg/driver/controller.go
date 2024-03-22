package driver

import (
	"context"
	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/driver/internal"
	"github.com/vngcloud/vngcloud-csi-volume-modifier/pkg/rpc"
	obj "github.com/vngcloud/vngcloud-go-sdk/vngcloud/objects"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
	"strconv"
)

var (
	// NewMetadataFunc is a variable for the cloud.NewMetadata function that can
	// be overwritten in unit tests.
	NewMetadataFunc = cloud.NewMetadataService
	NewCloudFunc    = cloud.NewCloud
)

// Supported access modes
const (
	vContainerCSIClusterIDKey = DriverName + "/cluster"
	SingleNodeWriter          = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	MultiNodeMultiWriter      = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
)

var (
	// controllerCaps represents the capability of controller service
	controllerCaps = []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
		csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS,
		csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
		csi.ControllerServiceCapability_RPC_MODIFY_VOLUME,
	}
)

type controllerService struct {
	cloud               cloud.Cloud
	inFlight            *internal.InFlight
	modifyVolumeManager *modifyVolumeManager
	driverOptions       *DriverOptions

	rpc.UnimplementedModifyServer
}

// newControllerService creates a new controller service
// it panics if failed to create the service
func newControllerService(driverOptions *DriverOptions) controllerService {
	metadata, err := NewMetadataFunc(cloud.DefaultVServerMetadataClient)
	if err != nil {
		klog.ErrorS(err, "Could not determine region from any metadata service. The region can be manually supplied via the AWS_REGION environment variable.")
		panic(err)
	}

	cloudSrv, err := NewCloudFunc(driverOptions.identityURL, driverOptions.vServerURL, driverOptions.clientID, driverOptions.clientSecret, metadata)
	if err != nil {
		panic(err)
	}

	return controllerService{
		cloud:               cloudSrv,
		inFlight:            internal.NewInFlight(),
		driverOptions:       driverOptions,
		modifyVolumeManager: newModifyVolumeManager(),
	}
}

func (d *controllerService) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	return nil, nil
}

func (d *controllerService) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	return nil, nil
}
func (s *controllerService) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (result *csi.ControllerPublishVolumeResponse, err error) {
	return nil, nil
}

func (s *controllerService) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	return nil, nil
}

func (s *controllerService) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "CreateSnapshot is not yet implemented")
}

func (s *controllerService) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "DeleteSnapshot is not yet implemented")
}

func (s *controllerService) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ListSnapshots is not yet implemented")
}

func (s *controllerService) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ValidateVolumeCapabilities is not yet implemented")
}

func (d *controllerService) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	klog.V(4).InfoS("ListVolumes: called", "args", *req)
	return nil, status.Error(codes.Unimplemented, "")
}

func (s *controllerService) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "GetCapacity is not yet implemented")
}

func (d *controllerService) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	klog.V(4).InfoS("ControllerGetCapabilities: called", "args", *req)
	var caps []*csi.ControllerServiceCapability
	for _, cap := range controllerCaps {
		c := &csi.ControllerServiceCapability{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: cap,
				},
			},
		}
		caps = append(caps, c)
	}
	return &csi.ControllerGetCapabilitiesResponse{Capabilities: caps}, nil
}

func (s *controllerService) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ControllerExpandVolume is not yet implemented")
}

func (s *controllerService) ControllerGetVolume(ctx context.Context, req *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	klog.V(4).Infof("ControllerGetVolume; called with request %+v", req)

	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID is required")
	}

	volume, err := s.cloud.GetVolume(volumeID)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get volume; ERR: %v", err))
	}

	volEntry := csi.ControllerGetVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      volume.VolumeId,
			CapacityBytes: int64(volume.Size * (1024 ^ 3))}}

	csiStatus := &csi.ControllerGetVolumeResponse_VolumeStatus{}
	if isAttachment(volume.VmId) {
		csiStatus.PublishedNodeIds = []string{*volume.VmId}
	}

	volEntry.Status = csiStatus

	return &volEntry, nil
}

func (d *controllerService) ControllerModifyVolume(ctx context.Context, req *csi.ControllerModifyVolumeRequest) (*csi.ControllerModifyVolumeResponse, error) {
	klog.V(4).InfoS("ControllerModifyVolume: called", "args", *req)

	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	options, err := parseModifyVolumeParameters(req.GetMutableParameters())
	if err != nil {
		return nil, err
	}

	err = d.modifyVolumeWithCoalescing(ctx, volumeID, options)
	if err != nil {
		return nil, err
	}

	return &csi.ControllerModifyVolumeResponse{}, nil
}

func (d *controllerService) GetCSIDriverModificationCapability(
	_ context.Context,
	_ *rpc.GetCSIDriverModificationCapabilityRequest,
) (*rpc.GetCSIDriverModificationCapabilityResponse, error) {
	return &rpc.GetCSIDriverModificationCapabilityResponse{}, nil
}

func (d *controllerService) ModifyVolumeProperties(
	ctx context.Context,
	req *rpc.ModifyVolumePropertiesRequest,
) (*rpc.ModifyVolumePropertiesResponse, error) {
	klog.V(4).InfoS("ModifyVolumeProperties called", "req", req)
	if err := validateModifyVolumePropertiesRequest(req); err != nil {
		return nil, err
	}

	options, err := parseModifyVolumeParameters(req.GetParameters())
	if err != nil {
		return nil, err
	}

	name := req.GetName()
	err = d.modifyVolumeWithCoalescing(ctx, name, options)
	if err != nil {
		return nil, err
	}

	return &rpc.ModifyVolumePropertiesResponse{}, nil
}

func getCreateVolumeResponse(vol *obj.Volume) *csi.CreateVolumeResponse {
	var volsrc *csi.VolumeContentSource
	resp := &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      vol.VolumeId,
			CapacityBytes: int64(vol.Size * 1024 * 1024 * 1024),
			ContentSource: volsrc,
		},
	}

	return resp

}

func parseModifyVolumeParameters(params map[string]string) (*cloud.ModifyDiskOptions, error) {
	options := cloud.ModifyDiskOptions{}

	for key, value := range params {
		switch key {
		case ModificationKeyIOPS:
			iops, err := strconv.Atoi(value)
			if err != nil {
				return nil, status.Errorf(codes.InvalidArgument, "Could not parse IOPS: %q", value)
			}
			options.IOPS = iops
		case ModificationKeyVolumeType:
			options.VolumeType = value
		}
	}

	return &options, nil
}

func isAttachment(vmId *string) bool {
	if vmId == nil {
		return false
	}
	return true
}

func validateModifyVolumePropertiesRequest(req *rpc.ModifyVolumePropertiesRequest) error {
	name := req.GetName()
	if name == "" {
		return status.Error(codes.InvalidArgument, "Volume name not provided")
	}
	return nil
}

const (
	ModificationKeyVolumeType = "type"

	ModificationKeyIOPS = "iops"
)
