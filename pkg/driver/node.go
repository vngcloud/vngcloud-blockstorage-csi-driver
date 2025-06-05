package driver

import (
	lctx "context"
	"encoding/json"
	"fmt"
	lcsi "github.com/container-storage-interface/spec/lib/go/csi"
	lscloud "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud"
	lsinternal "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/driver/internal"
	lsutil "github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/util"
	lsdkClientV2 "github.com/vngcloud/vngcloud-go-sdk/v2/client"
	computev2 "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/services/compute/v2"
	lsdkPortalSvcV1 "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/services/portal/v1"
	lsdkPortalSvcV2 "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/services/portal/v2"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	lutilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/volume"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type JSONPatch struct {
	OP    string      `json:"op,omitempty"`
	Path  string      `json:"path,omitempty"`
	Value interface{} `json:"value"`
}

// nodeService represents the node service of CSI driver
type nodeService struct {
	metadata         lscloud.MetadataService
	mounter          Mounter
	deviceIdentifier DeviceIdentifier
	inFlight         *lsinternal.InFlight
	driverOptions    *DriverOptions
}

func (s *nodeService) NodeStageVolume(_ lctx.Context, preq *lcsi.NodeStageVolumeRequest) (*lcsi.NodeStageVolumeResponse, error) {
	klog.V(4).InfoS("NodeStageVolume: called", "preq", *preq)

	volumeID := preq.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, ErrVolumeIDNotProvided
	}

	target := preq.GetStagingTargetPath()
	if len(target) == 0 {
		return nil, ErrStagingTargetPathNotProvided
	}

	volCap := preq.GetVolumeCapability()
	if volCap == nil {
		return nil, ErrVolumeCapabilitiesNotProvided
	}

	if !isValidVolumeCapabilities([]*lcsi.VolumeCapability{volCap}) {
		return nil, ErrVolumeCapabilitiesNotSupported
	}
	volumeContext := preq.GetVolumeContext()
	if isValid := isValidVolumeContext(volumeContext); !isValid {
		return nil, ErrVolumeAttributesInvalid
	}

	// If the access type is block, do nothing for stage
	switch volCap.GetAccessType().(type) {
	case *lcsi.VolumeCapability_Block:
		return &lcsi.NodeStageVolumeResponse{}, nil
	}

	mountVolume := volCap.GetMount()
	if mountVolume == nil {
		return nil, ErrMountIsNil
	}

	fsType := mountVolume.GetFsType()
	if len(fsType) == 0 {
		fsType = defaultFsType
	}

	_, ok := ValidFSTypes[strings.ToLower(fsType)]
	if !ok {
		return nil, ErrInvalidFstype(fsType)
	}

	context := preq.GetVolumeContext()

	blockSize, err := recheckFormattingOptionParameter(context, BlockSizeKey, FileSystemConfigs, fsType)
	if err != nil {
		return nil, err
	}
	inodeSize, err := recheckFormattingOptionParameter(context, InodeSizeKey, FileSystemConfigs, fsType)
	if err != nil {
		return nil, err
	}
	bytesPerInode, err := recheckFormattingOptionParameter(context, BytesPerInodeKey, FileSystemConfigs, fsType)
	if err != nil {
		return nil, err
	}
	numInodes, err := recheckFormattingOptionParameter(context, NumberOfInodesKey, FileSystemConfigs, fsType)
	if err != nil {
		return nil, err
	}
	ext4BigAlloc, err := recheckFormattingOptionParameter(context, Ext4BigAllocKey, FileSystemConfigs, fsType)
	if err != nil {
		return nil, err
	}
	ext4ClusterSize, err := recheckFormattingOptionParameter(context, Ext4ClusterSizeKey, FileSystemConfigs, fsType)
	if err != nil {
		return nil, err
	}

	mountOptions := collectMountOptions(fsType, mountVolume.GetMountFlags())

	if ok = s.inFlight.Insert(volumeID); !ok {
		return nil, ErrOperationAlreadyExists(volumeID)
	}
	defer func() {
		klog.V(4).InfoS("NodeStageVolume: volume operation finished", "volumeID", volumeID)
		s.inFlight.Delete(volumeID)
	}()

	devicePath, ok := preq.GetPublishContext()[DevicePathKey]
	if !ok {
		return nil, ErrDevicePathNotProvided
	}

	//source, err := s.findDevicePath(devicePath, volumeID, "")
	source, err := s.getDevicePath(devicePath)
	if err != nil {
		return nil, ErrFailedToFindTargetPath(devicePath, err)
	}

	klog.V(4).InfoS("NodeStageVolume: find device path", "devicePath", devicePath, "source", source)
	exists, err := s.mounter.PathExists(target)
	if err != nil {
		return nil, ErrFailedToCheckTargetPathExists(target, err)
	}
	// When exists is true it means target path was created but device isn't mounted.
	// We don't want to do anything in that case and let the operation proceed.
	// Otherwise we need to create the target directory.
	if !exists {
		// If target path does not exist we need to create the directory where volume will be staged
		klog.V(4).InfoS("NodeStageVolume: creating target dir", "target", target)
		if err = s.mounter.MakeDir(target); err != nil {
			return nil, ErrCanNotCreateTargetDir(target, err)
		}
	}

	// Check if a device is mounted in target directory
	device, _, err := s.mounter.GetDeviceNameFromMount(target)
	if err != nil {
		return nil, ErrFailedToCheckVolumeMounted(err)
	}

	// This operation (NodeStageVolume) MUST be idempotent.
	// If the volume corresponding to the volume_id is already staged to the staging_target_path,
	// and is identical to the specified volume_capability the Plugin MUST reply 0 OK.
	klog.V(4).InfoS("NodeStageVolume: checking if volume is already staged", "device", device, "source", source, "target", target)
	if device == source {
		klog.V(4).InfoS("NodeStageVolume: volume already staged", "volumeID", volumeID)
		return &lcsi.NodeStageVolumeResponse{}, nil
	}

	// FormatAndMount will format only if needed
	klog.V(4).InfoS("NodeStageVolume: staging volume", "source", source, "volumeID", volumeID, "target", target, "fstype", fsType)
	formatOptions := []string{}
	if len(blockSize) > 0 {
		if fsType == FSTypeXfs {
			blockSize = "size=" + blockSize
		}
		formatOptions = append(formatOptions, "-b", blockSize)
	}
	if len(inodeSize) > 0 {
		option := "-I"
		if fsType == FSTypeXfs {
			option, inodeSize = "-i", "size="+inodeSize
		}
		formatOptions = append(formatOptions, option, inodeSize)
	}
	if len(bytesPerInode) > 0 {
		formatOptions = append(formatOptions, "-i", bytesPerInode)
	}
	if len(numInodes) > 0 {
		formatOptions = append(formatOptions, "-N", numInodes)
	}
	if ext4BigAlloc == "true" {
		formatOptions = append(formatOptions, "-O", "bigalloc")
	}
	if len(ext4ClusterSize) > 0 {
		formatOptions = append(formatOptions, "-C", ext4ClusterSize)
	}
	err = s.mounter.FormatAndMountSensitiveWithFormatOptions(source, target, fsType, mountOptions, nil, formatOptions)
	if err != nil {
		return nil, ErrCanNotFormatAndMountVolume(source, target, err)
	}

	needResize, err := s.mounter.NeedResize(source, target)
	if err != nil {
		return nil, ErrDetermineVolumeResize(volumeID, source, err)
	}

	if needResize {
		r, err := s.mounter.NewResizeFs()
		if err != nil {
			return nil, ErrAttemptCreateResizeFs(err)
		}
		klog.V(2).InfoS("Volume needs resizing", "source", source)
		if _, err := r.Resize(source, target); err != nil {
			return nil, ErrCanNotResizeVolumeOnNode(volumeID, source, err)
		}
	}
	klog.V(4).InfoS("NodeStageVolume: successfully staged volume", "source", source, "volumeID", volumeID, "target", target, "fstype", fsType)
	return &lcsi.NodeStageVolumeResponse{}, nil
}

func (s *nodeService) NodeUnstageVolume(ctx lctx.Context, preq *lcsi.NodeUnstageVolumeRequest) (*lcsi.NodeUnstageVolumeResponse, error) {
	klog.V(4).InfoS("NodeUnstageVolume: called", "preq", *preq)
	volumeID := preq.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, ErrVolumeIDNotProvided
	}

	target := preq.GetStagingTargetPath()
	if len(target) == 0 {
		return nil, ErrStagingTargetNotProvided
	}

	if ok := s.inFlight.Insert(volumeID); !ok {
		return nil, ErrOperationAlreadyExists(volumeID)
	}
	defer func() {
		klog.V(4).InfoS("NodeUnStageVolume: volume operation finished", "volumeID", volumeID)
		s.inFlight.Delete(volumeID)
	}()

	// Check if target directory is a mount point. GetDeviceNameFromMount
	// given a mnt point, finds the device from /proc/mounts
	// returns the device name, reference count, and error code
	dev, refCount, err := s.mounter.GetDeviceNameFromMount(target)
	if err != nil {
		return nil, ErrFailedCheckTargetPathIsMountPoint(target, err)
	}

	// From the spec: If the volume corresponding to the volume_id
	// is not staged to the staging_target_path, the Plugin MUST
	// reply 0 OK.
	if refCount == 0 {
		klog.V(5).InfoS("[Debug] NodeUnstageVolume: target not mounted", "target", target)
		return &lcsi.NodeUnstageVolumeResponse{}, nil
	}

	if refCount > 1 {
		klog.InfoS("NodeUnstageVolume: found references to device mounted at target path", "refCount", refCount, "device", dev, "target", target)
	}

	klog.V(4).InfoS("NodeUnstageVolume: unmounting", "target", target)
	err = s.mounter.Unstage(target)
	if err != nil {
		return nil, ErrCanNotUnmountTarget(target, err)
	}

	klog.V(4).InfoS("NodeUnStageVolume: successfully unstaged volume", "volumeID", volumeID, "target", target)
	return &lcsi.NodeUnstageVolumeResponse{}, nil
}

func (s *nodeService) NodePublishVolume(_ lctx.Context, preq *lcsi.NodePublishVolumeRequest) (*lcsi.NodePublishVolumeResponse, error) {
	klog.V(4).InfoS("NodePublishVolume: called", "preq", *preq)
	volumeID := preq.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, ErrVolumeIDNotProvided
	}

	source := preq.GetStagingTargetPath()
	if len(source) == 0 {
		return nil, ErrStagingTargetNotProvided
	}

	target := preq.GetTargetPath()
	if len(target) == 0 {
		return nil, ErrTargetPathNotProvided
	}

	volCap := preq.GetVolumeCapability()
	if volCap == nil {
		return nil, ErrVolumeCapabilityNotProvided
	}

	if !isValidVolumeCapabilities([]*lcsi.VolumeCapability{volCap}) {
		return nil, ErrVolumeCapabilityNotSupported
	}

	if ok := s.inFlight.Insert(volumeID); !ok {
		return nil, ErrOperationAlreadyExists(volumeID)
	}
	defer func() {
		klog.V(4).InfoS("NodePublishVolume: volume operation finished", "volumeId", volumeID)
		s.inFlight.Delete(volumeID)
	}()

	mountOptions := []string{"bind"}
	if preq.GetReadonly() {
		mountOptions = append(mountOptions, "ro")
	}

	switch mode := volCap.GetAccessType().(type) {
	case *lcsi.VolumeCapability_Block:
		if err := s.nodePublishVolumeForBlock(preq, mountOptions); err != nil {
			return nil, err
		}
	case *lcsi.VolumeCapability_Mount:
		if err := s.nodePublishVolumeForFileSystem(preq, mountOptions, mode); err != nil {
			return nil, err
		}
	}

	return &lcsi.NodePublishVolumeResponse{}, nil
}

func (s *nodeService) NodeUnpublishVolume(pctx lctx.Context, preq *lcsi.NodeUnpublishVolumeRequest) (*lcsi.NodeUnpublishVolumeResponse, error) {
	klog.V(4).InfoS("NodeUnpublishVolume: called", "args", *preq)
	volumeID := preq.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, ErrVolumeIDNotProvided
	}

	target := preq.GetTargetPath()
	if len(target) == 0 {
		return nil, ErrTargetPathNotProvided
	}
	if ok := s.inFlight.Insert(volumeID); !ok {
		return nil, ErrOperationAlreadyExists(volumeID)
	}
	defer func() {
		klog.V(4).InfoS("NodeUnPublishVolume: volume operation finished", "volumeId", volumeID)
		s.inFlight.Delete(volumeID)
	}()

	klog.V(4).InfoS("NodeUnpublishVolume: unmounting", "target", target)
	err := s.mounter.Unpublish(target)
	if err != nil {
		return nil, ErrCanNotUnmountTarget(target, err)
	}

	return &lcsi.NodeUnpublishVolumeResponse{}, nil
}

func (s *nodeService) NodeGetVolumeStats(_ lctx.Context, preq *lcsi.NodeGetVolumeStatsRequest) (*lcsi.NodeGetVolumeStatsResponse, error) {
	klog.V(4).InfoS("NodeGetVolumeStats: called", "preq", *preq)

	if len(preq.GetVolumeId()) == 0 {
		return nil, ErrVolumeIDNotProvided
	}
	if len(preq.GetVolumePath()) == 0 {
		return nil, ErrVolumePathNotProvided
	}

	exists, err := s.mounter.PathExists(preq.GetVolumePath())
	if err != nil {
		return nil, ErrUnknownStatsOnPath(preq.GetVolumePath(), err)
	}
	if !exists {
		return nil, ErrPathNotExists(preq.GetVolumePath())
	}

	ibv, err := s.IsBlockDevice(preq.GetVolumePath())

	if err != nil {
		return nil, ErrDeterminedBlockDevice(preq.GetVolumePath(), err)
	}
	if ibv {
		bcap, blockErr := s.getBlockSizeBytes(preq.GetVolumePath())
		if blockErr != nil {
			return nil, ErrCanNotGetBlockCapacity(preq.GetVolumePath(), err)
		}
		return &lcsi.NodeGetVolumeStatsResponse{
			Usage: []*lcsi.VolumeUsage{
				{
					Unit:  lcsi.VolumeUsage_BYTES,
					Total: bcap,
				},
			},
		}, nil
	}

	metricsProvider := volume.NewMetricsStatFS(preq.GetVolumePath())

	metrics, err := metricsProvider.GetMetrics()
	if err != nil {
		return nil, ErrFailedToGetFsInfo(preq.GetVolumePath(), err)
	}

	return &lcsi.NodeGetVolumeStatsResponse{
		Usage: []*lcsi.VolumeUsage{
			{
				Unit:      lcsi.VolumeUsage_BYTES,
				Available: metrics.Available.AsDec().UnscaledBig().Int64(),
				Total:     metrics.Capacity.AsDec().UnscaledBig().Int64(),
				Used:      metrics.Used.AsDec().UnscaledBig().Int64(),
			},
			{
				Unit:      lcsi.VolumeUsage_INODES,
				Available: metrics.InodesFree.AsDec().UnscaledBig().Int64(),
				Total:     metrics.Inodes.AsDec().UnscaledBig().Int64(),
				Used:      metrics.InodesUsed.AsDec().UnscaledBig().Int64(),
			},
		},
	}, nil
}

func (s *nodeService) NodeExpandVolume(_ lctx.Context, preq *lcsi.NodeExpandVolumeRequest) (*lcsi.NodeExpandVolumeResponse, error) {
	klog.V(4).InfoS("NodeExpandVolume: called", "preq", *preq)
	volumeID := preq.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, ErrVolumeIDNotProvided
	}
	volumePath := preq.GetVolumePath()
	if len(volumePath) == 0 {
		return nil, ErrVolumePathNotProvided
	}

	volumeCapability := preq.GetVolumeCapability()
	// VolumeCapability is optional, if specified, use that as source of truth
	if volumeCapability != nil {
		caps := []*lcsi.VolumeCapability{volumeCapability}
		if !isValidVolumeCapabilities(caps) {
			return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("VolumeCapability is invalid: %v", volumeCapability))
		}

		if blk := volumeCapability.GetBlock(); blk != nil {
			// Noop for Block NodeExpandVolume
			klog.V(4).InfoS("NodeExpandVolume: called. Since it is a block device, ignoring...", "volumeID", volumeID, "volumePath", volumePath)
			return &lcsi.NodeExpandVolumeResponse{}, nil
		}
	} else {
		// TODO use util.GenericResizeFS
		// VolumeCapability is nil, check if volumePath point to a block device
		ibv, err := s.IsBlockDevice(volumePath)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to determine if volumePath [%v] is a block device: %v", volumePath, err)
		}
		if ibv {
			// Skip resizing for Block NodeExpandVolume
			bcap, err := s.getBlockSizeBytes(volumePath)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to get block capacity on path %s: %v", preq.GetVolumePath(), err)
			}
			klog.V(4).InfoS("NodeExpandVolume: called, since given volumePath is a block device, ignoring...", "volumeID", volumeID, "volumePath", volumePath)
			return &lcsi.NodeExpandVolumeResponse{CapacityBytes: bcap}, nil
		}
	}

	output, err := s.mounter.GetMountFs(volumePath)
	if err != nil {
		klog.Errorf("NodeExpandVolume; failed to get mount fs: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}

	devicePath := strings.TrimSpace(string(output))
	if devicePath == "" {
		klog.Errorf("NodeExpandVolume; failed to get device path for volume %s", volumeID)
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get device path for volume %s", volumeID))
	}

	r, err := s.mounter.NewResizeFs()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error attempting to create new ResizeFs:  %v", err)
	}

	// TODO: lock per volume ID to have some idempotency
	if _, err = r.Resize(devicePath, volumePath); err != nil {
		return nil, status.Errorf(codes.Internal, "Could not resize volume %q (%q):  %v", volumeID, devicePath, err)
	}

	bcap, err := s.getBlockSizeBytes(devicePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get block capacity on path %s: %v", preq.GetVolumePath(), err)
	}
	return &lcsi.NodeExpandVolumeResponse{CapacityBytes: bcap}, nil
}

func (s *nodeService) NodeGetCapabilities(_ lctx.Context, req *lcsi.NodeGetCapabilitiesRequest) (*lcsi.NodeGetCapabilitiesResponse, error) {
	klog.V(4).InfoS("NodeGetCapabilities: called", "args", *req)
	var caps []*lcsi.NodeServiceCapability
	for _, capa := range nodeCaps {
		c := &lcsi.NodeServiceCapability{
			Type: &lcsi.NodeServiceCapability_Rpc{
				Rpc: &lcsi.NodeServiceCapability_RPC{
					Type: capa,
				},
			},
		}
		caps = append(caps, c)
	}
	return &lcsi.NodeGetCapabilitiesResponse{Capabilities: caps}, nil
}

func (s *nodeService) NodeGetInfo(_ lctx.Context, _ *lcsi.NodeGetInfoRequest) (*lcsi.NodeGetInfoResponse, error) {
	klog.V(5).InfoS("[DEBUG] - NodeGetInfo: START to get the node info")
	nodeUUID := s.metadata.GetInstanceID()
	zone := s.metadata.GetAvailabilityZone()
	mvpn := 0

	if s.driverOptions.maxVolumesPerNode < 1 {
		projectID := s.metadata.GetProjectID()

		klog.InfoS("[INFO] - NodeGetInfo: the necessary information is retrieved successfully", "nodeId", nodeUUID, "zone", zone, "projectId", projectID)
		if len(projectID) < 1 {
			klog.ErrorS(nil, "[ERROR] - NodeGetInfo; projectID is empty")
			return nil, fmt.Errorf("projectID is empty")
		}

		clientCfg := lsdkClientV2.NewSdkConfigure().
			WithClientId(s.driverOptions.clientID).
			WithClientSecret(s.driverOptions.clientSecret).
			WithIamEndpoint(s.driverOptions.identityURL).
			WithVServerEndpoint(s.driverOptions.vServerURL)

		cloudClient := lsdkClientV2.NewClient(lctx.TODO()).Configure(clientCfg)

		klog.V(5).InfoS("[DEBUG] - NodeGetInfo: Get the portal info and quota")
		portal, sdkErr := cloudClient.VServerGateway().V1().PortalService().
			GetPortalInfo(lsdkPortalSvcV1.NewGetPortalInfoRequest(projectID))
		if sdkErr != nil {
			klog.ErrorS(sdkErr.GetError(), "[ERROR] - NodeGetInfo; failed to get portal info")
			return nil, sdkErr.GetError()
		}

		klog.InfoS("[INFO] - NodeGetInfo: Received the portal info successfully", "portal", portal)
		cloudClient = cloudClient.WithProjectId(portal.ProjectID)

		quota, sdkErr := cloudClient.VServerGateway().V2().PortalService().
			GetQuotaByName(lsdkPortalSvcV2.NewGetQuotaByNameRequest(lsdkPortalSvcV2.QtVolumeAttachLimit))

		if sdkErr != nil {
			klog.ErrorS(sdkErr.GetError(), "[ERROR] - NodeGetInfo; failed to get quota")
			return nil, sdkErr.GetError()
		}

		mvpn = quota.Limit
		klog.InfoS("[INFO] - NodeGetInfo: Setup the VngCloud Manage CSI driver for this node successfully",
			"quota", quota, "nodeId", nodeUUID, "zone", zone, "projectId", projectID)

		opt := computev2.NewGetServerByIdRequest(nodeUUID)
		server, sdkErr := cloudClient.VServerGateway().V2().ComputeService().GetServerById(opt)
		if sdkErr != nil {
			klog.ErrorS(sdkErr.GetError(), "[ERROR] - GetServerByID: ", "nodeUUID", nodeUUID)
			return nil, sdkErr.GetError()
		}
		zone = lsutil.ConvertPortalZoneToVMZone(server.ZoneId)
		klog.InfoS("[INFO] - NodeGetInfo: Get the server info successfully",
			"nodeId", nodeUUID, "zone", zone, "projectId", projectID)
	} else {
		mvpn = s.driverOptions.maxVolumesPerNode
		klog.InfoS("[INFO] - NodeGetInfo: Setup the VngCloud Manage CSI driver for this node successfully",
			"maxVolumesPerNode", mvpn, "nodeId", nodeUUID, "zone", zone)
	}

	return &lcsi.NodeGetInfoResponse{
		NodeId:            nodeUUID,
		MaxVolumesPerNode: int64(mvpn),
		AccessibleTopology: &lcsi.Topology{
			Segments: map[string]string{
				ZoneTopologyKey: zone,
			},
		},
	}, nil
}

// IsBlock checks if the given path is a block device
func (s *nodeService) IsBlockDevice(fullPath string) (bool, error) {
	var st unix.Stat_t
	err := unix.Stat(fullPath, &st)
	if err != nil {
		return false, err
	}

	return (st.Mode & unix.S_IFMT) == unix.S_IFBLK, nil
}

func (s *nodeService) nodePublishVolumeForBlock(req *lcsi.NodePublishVolumeRequest, mountOptions []string) error {
	target := req.GetTargetPath()
	volumeContext := req.GetVolumeContext()

	devicePath, exists := req.GetPublishContext()[DevicePathKey]
	if !exists {
		return ErrDevicePathNotProvided
	}
	if isValid := isValidVolumeContext(volumeContext); !isValid {
		return ErrVolumeAttributesInvalid
	}

	source, err := s.getDevicePath(devicePath)
	if err != nil {
		return ErrFailedToFindTargetPath(devicePath, err)
	}

	klog.V(4).InfoS("NodePublishVolume [block]: find device path", "devicePath", devicePath, "source", source)

	globalMountPath := filepath.Dir(target)

	// create the global mount path if it is missing
	// Path in the form of /var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/publish/{volumeName}
	exists, err = s.mounter.PathExists(globalMountPath)
	if err != nil {
		return ErrFailedToCheckPathExists(globalMountPath, err)
	}

	if !exists {
		if err = s.mounter.MakeDir(globalMountPath); err != nil {
			return ErrCanNotCreateTargetDir(globalMountPath, err)
		}
	}

	// Create the mount point as a file since bind mount device node requires it to be a file
	klog.V(4).InfoS("NodePublishVolume [block]: making target file", "target", target)
	if err = s.mounter.MakeFile(target); err != nil {
		if removeErr := os.Remove(target); removeErr != nil {
			return ErrCanNotRemoveMountTarget(target, removeErr)
		}
		return ErrCanNotCreateFile(target, err)
	}

	//Checking if the target file is already mounted with a device.
	mounted, err := s.isMounted(source, target)
	if err != nil {
		return ErrCheckDiskIsMounted(target, err)
	}

	if !mounted {
		klog.V(4).InfoS("NodePublishVolume [block]: mounting", "source", source, "target", target)
		if err := s.mounter.Mount(source, target, "", mountOptions); err != nil {
			if removeErr := os.Remove(target); removeErr != nil {
				return ErrCanNotRemoveMountTarget(target, removeErr)
			}
			return ErrCanNotMountAtTarget(source, target, err)
		}
	} else {
		klog.V(4).InfoS("NodePublishVolume [block]: Target path is already mounted", "target", target)
	}

	return nil
}

func (s *nodeService) nodePublishVolumeForFileSystem(req *lcsi.NodePublishVolumeRequest, mountOptions []string, mode *lcsi.VolumeCapability_Mount) error {
	target := req.GetTargetPath()
	source := req.GetStagingTargetPath()
	if m := mode.Mount; m != nil {
		for _, f := range m.GetMountFlags() {
			if !hasMountOption(mountOptions, f) {
				mountOptions = append(mountOptions, f)
			}
		}
	}

	if err := s.preparePublishTarget(target); err != nil {
		return ErrCanNotCreateTargetDir(target, err)
	}

	//Checking if the target directory is already mounted with a device.
	mounted, err := s.isMounted(source, target)
	if err != nil {
		return ErrCheckDiskIsMounted(target, err)
	}

	if !mounted {
		fsType := mode.Mount.GetFsType()
		if len(fsType) == 0 {
			fsType = defaultFsType
		}

		_, ok := ValidFSTypes[strings.ToLower(fsType)]
		if !ok {
			return ErrInvalidFstype(fsType)
		}

		mountOptions = collectMountOptions(fsType, mountOptions)
		klog.V(4).InfoS("NodePublishVolume: mounting", "source", source, "target", target, "mountOptions", mountOptions, "fsType", fsType)
		if err := s.mounter.Mount(source, target, fsType, mountOptions); err != nil {
			return ErrCanNotMountAtTarget(source, target, err)
		}
	}

	return nil
}

func (s *nodeService) preparePublishTarget(target string) error {
	klog.V(4).InfoS("NodePublishVolume: creating dir", "target", target)
	if err := s.mounter.MakeDir(target); err != nil {
		return err
	}
	return nil
}

func (s *nodeService) getBlockSizeBytes(devicePath string) (int64, error) {
	cmd := s.mounter.(*NodeMounter).Exec.Command("blockdev", "--getsize64", devicePath)
	output, err := cmd.Output()
	if err != nil {
		return -1, fmt.Errorf("error when getting size of block volume at path %s: output: %s, err: %w", devicePath, string(output), err)
	}
	strOut := strings.TrimSpace(string(output))
	gotSizeBytes, err := strconv.ParseInt(strOut, 10, 64)
	if err != nil {
		return -1, fmt.Errorf("failed to parse size %s as int", strOut)
	}
	return gotSizeBytes, nil
}

// isMounted checks if target is mounted. It does NOT return an error if target
// doesn't exist.
func (s *nodeService) isMounted(_ string, target string) (bool, error) {
	/*
		Checking if it's a mount point using IsLikelyNotMountPoint. There are three different return values,
		1. true, err when the directory does not exist or corrupted.
		2. false, nil when the path is already mounted with a device.
		3. true, nil when the path is not mounted with any device.
	*/
	notMnt, err := s.mounter.IsLikelyNotMountPoint(target)
	if err != nil && !os.IsNotExist(err) {
		//Checking if the path exists and error is related to Corrupted Mount, in that case, the system could unmount and mount.
		_, pathErr := s.mounter.PathExists(target)
		if pathErr != nil && s.mounter.IsCorruptedMnt(pathErr) {
			klog.V(4).InfoS("NodePublishVolume: Target path is a corrupted mount. Trying to unmount.", "target", target)
			if mntErr := s.mounter.Unpublish(target); mntErr != nil {
				return false, ErrCanNotUnmountTarget(target, mntErr)
			}
			//After successful unmount, the device is ready to be mounted.
			return false, nil
		}

		return false, ErrFailedCheckTargetPathIsMountPoint(target, lutilerrors.NewAggregate([]error{err, pathErr}))
	}

	// Do not return os.IsNotExist error. Other errors were handled above.  The
	// Existence of the target should be checked by the caller explicitly and
	// independently because sometimes prior to mount it is expected not to exist
	// (in Windows, the target must NOT exist before a symlink is created at it)
	// and in others it is an error (in Linux, the target mount directory must
	// exist before mount is called on it)
	if err != nil && os.IsNotExist(err) {
		klog.V(5).InfoS("[Debug] NodePublishVolume: Target path does not exist", "target", target)
		return false, nil
	}

	if !notMnt {
		klog.V(4).InfoS("NodePublishVolume: Target path is already mounted", "target", target)
	}

	return !notMnt, nil
}

func (s *nodeService) getDevicePath(volumeID string) (string, error) {
	var devicePath string
	devicePath, err := s.mounter.GetDevicePathBySerialID(volumeID)
	if err != nil {
		klog.Warningf("Couldn't get device path from mount: %v", err)
	}

	return devicePath, nil
}

// newNodeService creates a new node service it panics if failed to create the service
func newNodeService(driverOptions *DriverOptions) nodeService {
	klog.V(5).Infof("Retrieving node info from metadata service")
	metadata, err := lscloud.NewMetadataService(lscloud.DefaultVServerMetadataClient)
	if err != nil {
		panic(err)
	}

	nodeMounter, err := newNodeMounter()
	if err != nil {
		panic(err)
	}

	// Remove taint from node to indicate driver startup success
	// This is done at the last possible moment to prevent race conditions or false positive removals
	time.AfterFunc(taintRemovalInitialDelay, func() {
		removeTaintInBackground(lscloud.DefaultKubernetesAPIClient, removeNotReadyTaint)
	})

	return nodeService{
		metadata:         metadata,
		mounter:          nodeMounter,
		deviceIdentifier: newNodeDeviceIdentifier(),
		inFlight:         lsinternal.NewInFlight(),
		driverOptions:    driverOptions,
	}
}

// removeTaintInBackground is a goroutine that retries removeNotReadyTaint with exponential backoff
func removeTaintInBackground(k8sClient lscloud.KubernetesAPIClient, removalFunc func(lscloud.KubernetesAPIClient) error) {
	backoffErr := wait.ExponentialBackoff(taintRemovalBackoff, func() (bool, error) {
		err := removalFunc(k8sClient)
		if err != nil {
			klog.ErrorS(err, "Unexpected failure when attempting to remove node taint(s)")
			return false, nil
		}
		return true, nil
	})

	if backoffErr != nil {
		klog.ErrorS(backoffErr, "Retries exhausted, giving up attempting to remove node taint(s)")
	}
}

func removeNotReadyTaint(k8sClient lscloud.KubernetesAPIClient) error {
	nodeName := os.Getenv("CSI_NODE_NAME")
	if nodeName == "" {
		klog.V(4).InfoS("CSI_NODE_NAME missing, skipping taint removal")
		return nil
	}

	clientset, err := k8sClient()
	if err != nil {
		klog.V(4).InfoS("Failed to setup k8s client")
		return nil //lint:ignore nilerr If there are no k8s credentials, treat that as a soft failure
	}

	node, err := clientset.CoreV1().Nodes().Get(lctx.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	err = checkAllocatable(clientset, nodeName)
	if err != nil {
		return err
	}

	var taintsToKeep []corev1.Taint
	for _, taint := range node.Spec.Taints {
		if taint.Key != AgentNotReadyNodeTaintKey {
			taintsToKeep = append(taintsToKeep, taint)
		} else {
			klog.V(4).InfoS("Queued taint for removal", "key", taint.Key, "effect", taint.Effect)
		}
	}

	if len(taintsToKeep) == len(node.Spec.Taints) {
		klog.V(4).InfoS("No taints to remove on node, skipping taint removal")
		return nil
	}

	patchRemoveTaints := []JSONPatch{
		{
			OP:    "test",
			Path:  "/spec/taints",
			Value: node.Spec.Taints,
		},
		{
			OP:    "replace",
			Path:  "/spec/taints",
			Value: taintsToKeep,
		},
	}

	patch, err := json.Marshal(patchRemoveTaints)
	if err != nil {
		return err
	}

	_, err = clientset.CoreV1().Nodes().Patch(lctx.Background(), nodeName, k8stypes.JSONPatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return err
	}
	klog.InfoS("Removed taint(s) from local node", "node", nodeName)
	return nil
}

// hasMountOption returns a boolean indicating whether the given
// slice already contains a mount option. This is used to prevent
// passing duplicate option to the mount command.
func hasMountOption(options []string, opt string) bool {
	for _, o := range options {
		if o == opt {
			return true
		}
	}
	return false
}

func checkAllocatable(clientset kubernetes.Interface, nodeName string) error {
	csiNode, err := clientset.StorageV1().CSINodes().Get(lctx.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("isAllocatableSet: failed to get CSINode for %s: %w", nodeName, err)
	}
	klog.InfoS("CSINode drivers: ", "nodeName", nodeName, "driverName", csiNode.Spec)
	for _, driver := range csiNode.Spec.Drivers {
		klog.InfoS("CSINode driver info", "nodeName", nodeName, "driverName", driver.Name, "count", *driver.Allocatable.Count)
		if driver.Name == DriverName {
			if driver.Allocatable != nil && driver.Allocatable.Count != nil {
				klog.InfoS("CSINode Allocatable value is set", "nodeName", nodeName, "count", *driver.Allocatable.Count)
				return nil
			}
			return fmt.Errorf("isAllocatableSet: allocatable value not set for driver on node %s", nodeName)
		}
	}

	return fmt.Errorf("isAllocatableSet: driver not found on node %s", nodeName)
}

// collectMountOptions returns array of mount options from
// VolumeCapability_MountVolume and special mount options for
// given filesystem.
func collectMountOptions(fsType string, mntFlags []string) []string {
	var options []string
	for _, opt := range mntFlags {
		if !hasMountOption(options, opt) {
			options = append(options, opt)
		}
	}

	// By default, xfs does not allow mounting of two volumes with the same filesystem uuid.
	// Force ignore this uuid to be able to mount volume + its clone / restored snapshot on the same node.
	if fsType == FSTypeXfs {
		if !hasMountOption(options, "nouuid") {
			options = append(options, "nouuid")
		}
	}
	return options
}
