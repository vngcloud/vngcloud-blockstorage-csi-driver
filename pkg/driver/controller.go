package driver

import (
	"context"
	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/kubernetes-csi/csi-lib-utils/protosanitizer"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/utils"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/csi/vcontainer/vcontainer"
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
	klog.V(4).Infof("CreateVolume; called with request %+v", protosanitizer.StripSecrets(*req))

	volName := req.GetName()                       // get the volume name
	volCapabilities := req.GetVolumeCapabilities() // get the volume capabilities

	if volName == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume name is required")
	}

	if volCapabilities == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capabilities are required")
	}

	// set the default volume size if 1 GiB
	volSizeBytes := int64(1 * 1024 * 1024 * 1024)

	// get the volume size that user provided
	if req.GetCapacityRange() != nil {
		volSizeBytes = int64(req.GetCapacityRange().GetRequiredBytes())
	}

	// round up the volume size to GiB
	volSizeGB := uint64(utils.RoundUpSize(volSizeBytes, 1024*1024*1024))

	// get the volume type of the StorageClass
	volType := req.GetParameters()["type"]

	volumes, err := d.cloud.GetVolumesByName(volName)
	if err != nil {
		klog.Errorf("CreateVolume; failed to get volumes by name %s; ERR: %v", volName, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get volumes by name; ERR: %v", err))
	}

	if len(volumes) == 1 {
		if volSizeGB != volumes[0].Size {
			klog.Errorf("CreateVolume; volume %s already exists with different size %d", volName, volumes[0].Size)
			return nil, status.Error(codes.AlreadyExists, fmt.Sprintf("volume %s already exists with different size %d", volName, volumes[0].Size))
		}

		klog.V(4).Infof("CreateVolume; volume %s already exists with same size %d", volName, volumes[0].Size)
		return getCreateVolumeResponse(volumes[0]), nil
	} else if len(volumes) > 1 {
		klog.V(3).Infof("CreateVolume; volume %s already exists with different size %d", volName, volumes[0].Size)
		return nil, status.Error(codes.Internal, "Multiple volumes reported with same name")
	}

	// volume creation
	properties := map[string]string{
		vContainerCSIClusterIDKey: "vcontainer-kubernetes-cluster",
	}
	//Tag volume with metadata if present: https://github.com/kubernetes-csi/external-provisioner/pull/399
	for _, mKey := range []string{"csi.storage.k8s.io/pvc/name", "csi.storage.k8s.io/pvc/namespace", "csi.storage.k8s.io/pv/name"} {
		if v, ok := req.Parameters[mKey]; ok {
			properties[mKey] = v
		}
	}

	createdVol, err := d.cloud.CreateVolume(volName, volSizeGB, volType, "", "", "", &properties)

	if err != nil {
		klog.Errorf("CreateVolume; failed to create volume %s; ERR: %v", volName, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to create volume; ERR: %v", err))
	}

	klog.V(4).Infof("CreateVolume; volume %s (%d GiB) created successfully", volName, volSizeGB)

	return getCreateVolumeResponse(createdVol), nil
}

func (d *controllerService) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	klog.V(4).Infof("DeleteVolume; called with request %+v", req)

	volID := req.GetVolumeId()
	if volID == "" {
		klog.Errorf("DeleteVolume; Volume ID is required")
		return nil, status.Error(codes.InvalidArgument, "Volume ID is required")
	}

	vol, err := d.cloud.GetVolume(volID)
	if err != nil {
		klog.Errorf("DeleteVolume; failed to get volume %s; ERR: %v", volID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get volume; ERR: %v", err))
	}

	if vol.PersistentVolume != true {
		klog.Errorf("DeleteVolume; volume %s is not a persistent volume", volID)
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("volume %s is not a persistent volume", volID))
	}

	err = d.cloud.DeleteVolume(volID)
	if err != nil {
		klog.Errorf("DeleteVolume; failed to delete volume %s; ERR: %v", volID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to delete volume; ERR: %v", err))
	}

	klog.V(4).Infof("DeleteVolume; volume %s deleted successfully", volID)

	return &csi.DeleteVolumeResponse{}, nil
}
func (s *controllerService) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (result *csi.ControllerPublishVolumeResponse, err error) {
	klog.V(4).Infof("ControllerPublishVolume; called with request %+v", req)

	instanceID := req.GetNodeId()
	volumeID := req.GetVolumeId()
	volumeCapability := req.GetVolumeCapability()

	if instanceID == "" {
		klog.Errorf("ControllerPublishVolume; Node ID is required")
		return nil, status.Error(codes.InvalidArgument, "Node ID is required")
	}

	if volumeID == "" {
		klog.Errorf("ControllerPublishVolume; Volume ID is required")
		return nil, status.Error(codes.InvalidArgument, "Volume ID is required")
	}

	if volumeCapability == nil {
		klog.Errorf("ControllerPublishVolume; Volume capability is required")
		return nil, status.Error(codes.InvalidArgument, "Volume capability is required")
	}

	vol, err := s.cloud.GetVolume(volumeID)
	if err != nil {
		klog.Errorf("ControllerPublishVolume; failed to get volume %s; ERR: %v", volumeID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get volume; ERR: %v", err))
	}

	_, err = s.cloud.AttachVolume(instanceID, volumeID)
	if err != nil {
		if vol != nil && vol.Status == vcontainer.VolumeInUseStatus {
			klog.V(4).Infof("ControllerPublishVolume; volume %s attached to instance %s successfully", volumeID, instanceID)
			return &csi.ControllerPublishVolumeResponse{}, nil
		}

		klog.Errorf("ControllerPublishVolume; failed to attach volume %s to instance %s; ERR: %v", volumeID, instanceID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to attach volume; ERR: %v", err))
	}

	err = s.cloud.WaitDiskAttached(instanceID, volumeID)
	if err != nil {
		klog.Errorf("ControllerPublishVolume; failed to wait disk attached to instance %s; ERR: %v", instanceID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to wait disk attached; ERR: %v", err))
	}

	_, err = s.cloud.GetAttachmentDiskPath(instanceID, volumeID)
	if err != nil {
		klog.Errorf("ControllerPublishVolume; failed to get device path for volume %s; ERR: %v", volumeID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get device path; ERR: %v", err))
	}

	klog.V(4).Infof("ControllerPublishVolume; volume %s attached to instance %s successfully", volumeID, instanceID)
	return &csi.ControllerPublishVolumeResponse{}, nil
}

func (s *controllerService) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	klog.V(4).Infof("ControllerUnpublishVolume; called with request %+v", req)

	instanceID := req.GetNodeId()
	volumeID := req.GetVolumeId()

	if volumeID == "" {
		klog.Errorf("ControllerUnpublishVolume; Volume ID is required")
		return nil, status.Error(codes.InvalidArgument, "Volume ID is required")
	}

	_ = s.cloud.DetachVolume(instanceID, volumeID)

	if err := s.cloud.WaitDiskDetached(instanceID, volumeID); err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to wait disk detached; ERR: %v", err))
	}

	klog.V(4).Infof("ControllerUnpublishVolume; volume %s detached from instance %s successfully", volumeID, instanceID)
	return &csi.ControllerUnpublishVolumeResponse{}, nil
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
	volCaps := req.GetVolumeCapabilities()
	if len(volCaps) < 1 {
		return nil, status.Error(codes.InvalidArgument, "Volume capabilities are required")
	}

	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID is required")
	}

	_, err := s.cloud.GetVolume(volumeID)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get volume; ERR: %v", err))
	}

	for _, cap := range volCaps {
		if cap.GetAccessMode().GetMode() != SingleNodeWriter {
			return &csi.ValidateVolumeCapabilitiesResponse{Message: "requested volume capability not supported"}, nil
		}
	}

	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: SingleNodeWriter,
					},
				},
			},
		},
	}, nil
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
	klog.V(4).Infof("ControllerExpandVolume; called with request %+v", req)

	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID is required")
	}

	capRange := req.GetCapacityRange()
	if capRange == nil {
		return nil, status.Error(codes.InvalidArgument, "Capacity range is required")
	}

	volSizeBytes := int64(req.GetCapacityRange().GetRequiredBytes())
	volSizeGB := uint64(utils.RoundUpSize(volSizeBytes, 1024*1024*1024))
	maxVolSize := capRange.GetLimitBytes()

	if maxVolSize > 0 && volSizeBytes > maxVolSize {
		return nil, status.Error(codes.OutOfRange, fmt.Sprintf("Requested size %d exceeds limit %d", volSizeBytes, maxVolSize))
	}

	volume, err := s.cloud.GetVolume(volumeID)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get volume; ERR: %v", err))
	}

	if volume == nil {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("volume %s not found", volumeID))
	}

	if volume.Size >= volSizeGB {
		klog.V(2).Infof("ControllerExpandVolume; volume %s already has size %d GiB", volumeID, volume.Size)
		return &csi.ControllerExpandVolumeResponse{
			CapacityBytes:         int64(volume.Size * 1024 * 1024 * 1024),
			NodeExpansionRequired: true,
		}, nil
	}

	err = s.cloud.ExpandVolume(volume.VolumeTypeID, volumeID, volSizeGB)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to expand volume; ERR: %v", err))
	}

	err = s.cloud.WaitVolumeTargetStatus(volumeID, []string{vcontainer.VolumeAvailableStatus, vcontainer.VolumeInUseStatus})
	if err != nil {
		klog.Errorf("ControllerExpandVolume; failed to wait volume %s to be available; ERR: %v", volumeID, err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to wait volume to be available; ERR: %v", err))
	}

	klog.V(4).Infof("ControllerExpandVolume; volume %s expanded to size %d successfully", volumeID, volSizeGB)
	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         volSizeBytes,
		NodeExpansionRequired: true,
	}, nil
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
