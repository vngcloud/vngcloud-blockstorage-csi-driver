package driver

import (
	"context"
	"encoding/json"
	"fmt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	lcsi "github.com/container-storage-interface/spec/lib/go/csi"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/driver/internal"
)

var (
	// taintRemovalInitialDelay is the initial delay for node taint removal
	taintRemovalInitialDelay = 1 * time.Second

	// taintRemovalBackoff is the exponential backoff configuration for node taint removal
	taintRemovalBackoff = wait.Backoff{
		Duration: 500 * time.Millisecond,
		Factor:   2,
		Steps:    10, // Max delay = 0.5 * 2^9 = ~4 minutes
	}
)

const (
	defaultFsType = FSTypeExt4
)

var (
	ValidFSTypes = map[string]struct{}{
		FSTypeExt2: {},
		FSTypeExt3: {},
		FSTypeExt4: {},
		FSTypeXfs:  {},
		FSTypeNtfs: {},
	}
)

// Struct for JSON patch operations
type JSONPatch struct {
	OP    string      `json:"op,omitempty"`
	Path  string      `json:"path,omitempty"`
	Value interface{} `json:"value"`
}

// nodeService represents the node service of CSI driver
type nodeService struct {
	metadata         cloud.MetadataService
	mounter          Mounter
	deviceIdentifier DeviceIdentifier
	inFlight         *internal.InFlight
	driverOptions    *DriverOptions
}

// newNodeService creates a new node service
// it panics if failed to create the service
func newNodeService(driverOptions *DriverOptions) nodeService {
	klog.V(5).InfoS("[Debug] Retrieving node info from metadata service")
	metadata, err := cloud.NewMetadataService(cloud.DefaultVServerMetadataClient)
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
		removeTaintInBackground(cloud.DefaultKubernetesAPIClient, removeNotReadyTaint)
	})

	return nodeService{
		metadata:         metadata,
		mounter:          nodeMounter,
		deviceIdentifier: newNodeDeviceIdentifier(),
		inFlight:         internal.NewInFlight(),
		driverOptions:    driverOptions,
	}
}

// removeTaintInBackground is a goroutine that retries removeNotReadyTaint with exponential backoff
func removeTaintInBackground(k8sClient cloud.KubernetesAPIClient, removalFunc func(cloud.KubernetesAPIClient) error) {
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

// removeNotReadyTaint removes the taint ebs.csi.aws.com/agent-not-ready from the local node
// This taint can be optionally applied by users to prevent startup race conditions such as
// https://github.com/kubernetes/kubernetes/issues/95911
func removeNotReadyTaint(k8sClient cloud.KubernetesAPIClient) error {
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

	node, err := clientset.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
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

	_, err = clientset.CoreV1().Nodes().Patch(context.Background(), nodeName, k8stypes.JSONPatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return err
	}
	klog.InfoS("Removed taint(s) from local node", "node", nodeName)
	return nil
}

func (s *nodeService) NodeStageVolume(ctx context.Context, req *lcsi.NodeStageVolumeRequest) (*lcsi.NodeStageVolumeResponse, error) {
	klog.V(4).InfoS("NodeStageVolume: called", "args", *req)

	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	target := req.GetStagingTargetPath()
	if len(target) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Staging target not provided")
	}

	volCap := req.GetVolumeCapability()
	if volCap == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capability not provided")
	}

	if !isValidVolumeCapabilities([]*lcsi.VolumeCapability{volCap}) {
		return nil, status.Error(codes.InvalidArgument, "Volume capability not supported")
	}
	volumeContext := req.GetVolumeContext()
	if isValid := isValidVolumeContext(volumeContext); !isValid {
		return nil, status.Error(codes.InvalidArgument, "Volume Attribute is not valid")
	}

	// If the access type is block, do nothing for stage
	switch volCap.GetAccessType().(type) {
	case *lcsi.VolumeCapability_Block:
		return &lcsi.NodeStageVolumeResponse{}, nil
	}

	mountVolume := volCap.GetMount()
	if mountVolume == nil {
		return nil, status.Error(codes.InvalidArgument, "NodeStageVolume: mount is nil within volume capability")
	}

	fsType := mountVolume.GetFsType()
	if len(fsType) == 0 {
		fsType = defaultFsType
	}

	_, ok := ValidFSTypes[strings.ToLower(fsType)]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "NodeStageVolume: invalid fstype %s", fsType)
	}

	context := req.GetVolumeContext()

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
		return nil, status.Errorf(codes.Aborted, volumeOperationAlreadyExists, volumeID)
	}
	defer func() {
		klog.V(4).InfoS("NodeStageVolume: volume operation finished", "volumeID", volumeID)
		s.inFlight.Delete(volumeID)
	}()

	devicePath, ok := req.GetPublishContext()[DevicePathKey]
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "Device path not provided")
	}

	//source, err := s.findDevicePath(devicePath, volumeID, "")
	source, err := s.getDevicePath(devicePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to find device path %s. %v", devicePath, err)
	}

	klog.V(4).InfoS("NodeStageVolume: find device path", "devicePath", devicePath, "source", source)
	exists, err := s.mounter.PathExists(target)
	if err != nil {
		msg := fmt.Sprintf("failed to check if target %q exists: %v", target, err)
		return nil, status.Error(codes.Internal, msg)
	}
	// When exists is true it means target path was created but device isn't mounted.
	// We don't want to do anything in that case and let the operation proceed.
	// Otherwise we need to create the target directory.
	if !exists {
		// If target path does not exist we need to create the directory where volume will be staged
		klog.V(4).InfoS("NodeStageVolume: creating target dir", "target", target)
		if err = s.mounter.MakeDir(target); err != nil {
			msg := fmt.Sprintf("could not create target dir %q: %v", target, err)
			return nil, status.Error(codes.Internal, msg)
		}
	}

	// Check if a device is mounted in target directory
	device, _, err := s.mounter.GetDeviceNameFromMount(target)
	if err != nil {
		msg := fmt.Sprintf("failed to check if volume is already mounted: %v", err)
		return nil, status.Error(codes.Internal, msg)
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
		msg := fmt.Sprintf("could not format %q and mount it at %q: %v", source, target, err)
		return nil, status.Error(codes.Internal, msg)
	}

	needResize, err := s.mounter.NeedResize(source, target)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not determine if volume %q (%q) need to be resized:  %v", req.GetVolumeId(), source, err)
	}

	if needResize {
		r, err := s.mounter.NewResizeFs()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Error attempting to create new ResizeFs:  %v", err)
		}
		klog.V(2).InfoS("Volume needs resizing", "source", source)
		if _, err := r.Resize(source, target); err != nil {
			return nil, status.Errorf(codes.Internal, "Could not resize volume %q (%q):  %v", volumeID, source, err)
		}
	}
	klog.V(4).InfoS("NodeStageVolume: successfully staged volume", "source", source, "volumeID", volumeID, "target", target, "fstype", fsType)
	return &lcsi.NodeStageVolumeResponse{}, nil
}

func (s *nodeService) NodeUnstageVolume(ctx context.Context, req *lcsi.NodeUnstageVolumeRequest) (*lcsi.NodeUnstageVolumeResponse, error) {
	return &lcsi.NodeUnstageVolumeResponse{}, nil
}

func (s *nodeService) NodePublishVolume(ctx context.Context, req *lcsi.NodePublishVolumeRequest) (*lcsi.NodePublishVolumeResponse, error) {
	klog.V(4).InfoS("NodePublishVolume: called", "args", *req)
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	source := req.GetStagingTargetPath()
	if len(source) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Staging target not provided")
	}

	target := req.GetTargetPath()
	if len(target) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path not provided")
	}

	volCap := req.GetVolumeCapability()
	if volCap == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capability not provided")
	}

	if !isValidVolumeCapabilities([]*lcsi.VolumeCapability{volCap}) {
		return nil, status.Error(codes.InvalidArgument, "Volume capability not supported")
	}

	if ok := s.inFlight.Insert(volumeID); !ok {
		return nil, status.Errorf(codes.Aborted, volumeOperationAlreadyExists, volumeID)
	}
	defer func() {
		klog.V(4).InfoS("NodePublishVolume: volume operation finished", "volumeId", volumeID)
		s.inFlight.Delete(volumeID)
	}()

	mountOptions := []string{"bind"}
	if req.GetReadonly() {
		mountOptions = append(mountOptions, "ro")
	}

	switch mode := volCap.GetAccessType().(type) {
	case *lcsi.VolumeCapability_Block:
		if err := s.nodePublishVolumeForBlock(req, mountOptions); err != nil {
			return nil, err
		}
	case *lcsi.VolumeCapability_Mount:
		if err := s.nodePublishVolumeForFileSystem(req, mountOptions, mode); err != nil {
			return nil, err
		}
	}

	return &lcsi.NodePublishVolumeResponse{}, nil
}

func (s *nodeService) NodeUnpublishVolume(pctx context.Context, preq *lcsi.NodeUnpublishVolumeRequest) (*lcsi.NodeUnpublishVolumeResponse, error) {
	klog.V(4).InfoS("NodeUnpublishVolume: called", "args", *preq)
	volumeID := preq.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	target := preq.GetTargetPath()
	if len(target) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path not provided")
	}
	if ok := s.inFlight.Insert(volumeID); !ok {
		return nil, status.Errorf(codes.Aborted, volumeOperationAlreadyExists, volumeID)
	}
	defer func() {
		klog.V(4).InfoS("NodeUnPublishVolume: volume operation finished", "volumeId", volumeID)
		s.inFlight.Delete(volumeID)
	}()

	klog.V(4).InfoS("NodeUnpublishVolume: unmounting", "target", target)
	err := s.mounter.Unpublish(target)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not unmount %q: %v", target, err)
	}

	return &lcsi.NodeUnpublishVolumeResponse{}, nil
}

func (s *nodeService) NodeGetVolumeStats(_ context.Context, req *lcsi.NodeGetVolumeStatsRequest) (*lcsi.NodeGetVolumeStatsResponse, error) {

	return &lcsi.NodeGetVolumeStatsResponse{}, nil
}

func (s *nodeService) NodeExpandVolume(ctx context.Context, req *lcsi.NodeExpandVolumeRequest) (*lcsi.NodeExpandVolumeResponse, error) {

	return &lcsi.NodeExpandVolumeResponse{}, nil
}

func (s *nodeService) NodeGetCapabilities(_ context.Context, req *lcsi.NodeGetCapabilitiesRequest) (*lcsi.NodeGetCapabilitiesResponse, error) {
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

func (s *nodeService) NodeGetInfo(_ context.Context, _ *lcsi.NodeGetInfoRequest) (*lcsi.NodeGetInfoResponse, error) {
	nodeUUID := s.metadata.GetInstanceID()
	zone := s.metadata.GetAvailabilityZone()

	klog.V(5).InfoS("NodeGetInfo; called to get node info", "nodeUUID", nodeUUID, "zone", zone)

	return &lcsi.NodeGetInfoResponse{
		NodeId:            nodeUUID,
		MaxVolumesPerNode: 26,
		AccessibleTopology: &lcsi.Topology{
			Segments: map[string]string{
				ZoneTopologyKey: zone,
			},
		},
	}, nil
}

func checkAllocatable(clientset kubernetes.Interface, nodeName string) error {
	csiNode, err := clientset.StorageV1().CSINodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("isAllocatableSet: failed to get CSINode for %s: %w", nodeName, err)
	}

	for _, driver := range csiNode.Spec.Drivers {
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

// findDevicePath finds path of device and verifies its existence
// if the device is not nvme, return the path directly
// if the device is nvme, finds and returns the nvme device path eg. /dev/nvme1n1
func (s *nodeService) findDevicePath(devicePath, volumeID, partition string) (string, error) {
	strippedVolumeName := strings.Replace(volumeID, "-", "", -1)
	canonicalDevicePath := ""

	// If the given path exists, the device MAY be nvme. Further, it MAY be a
	// symlink to the nvme device path like:
	// | $ stat /dev/xvdba
	// | File: ‘/dev/xvdba’ -> ‘nvme1n1’
	// Since these are maybes, not guarantees, the search for the nvme device
	// path below must happen and must rely on volume ID
	exists, err := s.mounter.PathExists(devicePath)
	if err != nil {
		return "", fmt.Errorf("failed to check if path %q exists: %w", devicePath, err)
	}

	if exists {
		stat, lstatErr := s.deviceIdentifier.Lstat(devicePath)
		if lstatErr != nil {
			return "", fmt.Errorf("failed to lstat %q: %w", devicePath, err)
		}

		if stat.Mode()&os.ModeSymlink == os.ModeSymlink {
			canonicalDevicePath, err = s.deviceIdentifier.EvalSymlinks(devicePath)
			if err != nil {
				return "", fmt.Errorf("failed to evaluate symlink %q: %w", devicePath, err)
			}
		} else {
			canonicalDevicePath = devicePath
		}

		klog.V(5).InfoS("[Debug] The canonical device path was resolved", "devicePath", devicePath, "cacanonicalDevicePath", canonicalDevicePath)
		if err = verifyVolumeSerialMatch(canonicalDevicePath, strippedVolumeName, execRunner); err != nil {
			return "", err
		}
		return s.appendPartition(canonicalDevicePath, partition), nil
	}

	klog.V(5).InfoS("[Debug] Falling back to nvme volume ID lookup", "devicePath", devicePath)

	// AWS recommends identifying devices by volume ID
	// (https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/nvme-ebs-volumes.html),
	// so find the nvme device path using volume ID. This is the magic name on
	// which AWS presents NVME devices under /dev/disk/by-id/. For example,
	// vol-0fab1d5e3f72a5e23 creates a symlink at
	// /dev/disk/by-id/nvme-Amazon_Elastic_Block_Store_vol0fab1d5e3f72a5e23
	nvmeName := "nvme-Amazon_Elastic_Block_Store_" + strippedVolumeName

	nvmeDevicePath, err := findNvmeVolume(s.deviceIdentifier, nvmeName)

	if err == nil {
		klog.V(5).InfoS("[Debug] successfully resolved", "nvmeName", nvmeName, "nvmeDevicePath", nvmeDevicePath)
		canonicalDevicePath = nvmeDevicePath
		if err = verifyVolumeSerialMatch(canonicalDevicePath, strippedVolumeName, execRunner); err != nil {
			return "", err
		}
		return s.appendPartition(canonicalDevicePath, partition), nil
	} else {
		klog.V(5).InfoS("[Debug] error searching for nvme path", "nvmeName", nvmeName, "err", err)
	}

	if canonicalDevicePath == "" {
		return "", errNoDevicePathFound(devicePath, volumeID)
	}

	canonicalDevicePath = s.appendPartition(canonicalDevicePath, partition)
	return canonicalDevicePath, nil
}

func (s *nodeService) nodePublishVolumeForBlock(req *lcsi.NodePublishVolumeRequest, mountOptions []string) error {
	target := req.GetTargetPath()
	//volumeID := req.GetVolumeId()
	volumeContext := req.GetVolumeContext()

	devicePath, exists := req.GetPublishContext()[DevicePathKey]
	if !exists {
		return status.Error(codes.InvalidArgument, "Device path not provided")
	}
	if isValidVolumeContext := isValidVolumeContext(volumeContext); !isValidVolumeContext {
		return status.Error(codes.InvalidArgument, "Volume Attribute is invalid")
	}

	//partition := ""
	//if part, ok := req.GetVolumeContext()[VolumeAttributePartition]; ok {
	//	if part != "0" {
	//		partition = part
	//	} else {
	//		klog.InfoS("NodePublishVolume: invalid partition config, will ignore.", "partition", part)
	//	}
	//}

	//source, err := s.findDevicePath(devicePath, volumeID, partition)
	source, err := s.getDevicePath(devicePath)
	if err != nil {
		return status.Errorf(codes.Internal, "Failed to find device path %s. %v", devicePath, err)
	}

	klog.V(4).InfoS("NodePublishVolume [block]: find device path", "devicePath", devicePath, "source", source)

	globalMountPath := filepath.Dir(target)

	// create the global mount path if it is missing
	// Path in the form of /var/lib/kubelet/plugins/kubernetes.io/csi/volumeDevices/publish/{volumeName}
	exists, err = s.mounter.PathExists(globalMountPath)
	if err != nil {
		return status.Errorf(codes.Internal, "Could not check if path exists %q: %v", globalMountPath, err)
	}

	if !exists {
		if err = s.mounter.MakeDir(globalMountPath); err != nil {
			return status.Errorf(codes.Internal, "Could not create dir %q: %v", globalMountPath, err)
		}
	}

	// Create the mount point as a file since bind mount device node requires it to be a file
	klog.V(4).InfoS("NodePublishVolume [block]: making target file", "target", target)
	if err = s.mounter.MakeFile(target); err != nil {
		if removeErr := os.Remove(target); removeErr != nil {
			return status.Errorf(codes.Internal, "Could not remove mount target %q: %v", target, removeErr)
		}
		return status.Errorf(codes.Internal, "Could not create file %q: %v", target, err)
	}

	//Checking if the target file is already mounted with a device.
	mounted, err := s.isMounted(source, target)
	if err != nil {
		return status.Errorf(codes.Internal, "Could not check if %q is mounted: %v", target, err)
	}

	if !mounted {
		klog.V(4).InfoS("NodePublishVolume [block]: mounting", "source", source, "target", target)
		if err := s.mounter.Mount(source, target, "", mountOptions); err != nil {
			if removeErr := os.Remove(target); removeErr != nil {
				return status.Errorf(codes.Internal, "Could not remove mount target %q: %v", target, removeErr)
			}
			return status.Errorf(codes.Internal, "Could not mount %q at %q: %v", source, target, err)
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
		return status.Errorf(codes.Internal, err.Error())
	}

	//Checking if the target directory is already mounted with a device.
	mounted, err := s.isMounted(source, target)
	if err != nil {
		return status.Errorf(codes.Internal, "Could not check if %q is mounted: %v", target, err)
	}

	if !mounted {
		fsType := mode.Mount.GetFsType()
		if len(fsType) == 0 {
			fsType = defaultFsType
		}

		_, ok := ValidFSTypes[strings.ToLower(fsType)]
		if !ok {
			return status.Errorf(codes.InvalidArgument, "NodePublishVolume: invalid fstype %s", fsType)
		}

		mountOptions = collectMountOptions(fsType, mountOptions)
		klog.V(4).InfoS("NodePublishVolume: mounting", "source", source, "target", target, "mountOptions", mountOptions, "fsType", fsType)
		if err := s.mounter.Mount(source, target, fsType, mountOptions); err != nil {
			return status.Errorf(codes.Internal, "Could not mount %q at %q: %v", source, target, err)
		}
	}

	return nil
}

func (s *nodeService) preparePublishTarget(target string) error {
	klog.V(4).InfoS("NodePublishVolume: creating dir", "target", target)
	if err := s.mounter.MakeDir(target); err != nil {
		return fmt.Errorf("Could not create dir %q: %w", target, err)
	}
	return nil
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
				return false, status.Errorf(codes.Internal, "Unable to unmount the target %q : %v", target, mntErr)
			}
			//After successful unmount, the device is ready to be mounted.
			return false, nil
		}
		return false, status.Errorf(codes.Internal, "Could not check if %q is a mount point: %v, %v", target, err, pathErr)
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

func verifyVolumeSerialMatch(canonicalDevicePath string, strippedVolumeName string, execRunner func(string, ...string) ([]byte, error)) error {
	// In some rare cases, a race condition can lead to the /dev/disk/by-id/ symlink becoming out of date
	// See https://github.com/kubernetes-sigs/aws-ebs-csi-driver/issues/1224 for more info
	// Attempt to use lsblk to double check that the nvme device selected was the correct volume
	output, err := execRunner("lsblk", "--noheadings", "--ascii", "--nodeps", "--output", "SERIAL", canonicalDevicePath)

	if err == nil {
		// Look for an EBS volume ID in the output, compare all matches against what we expect
		// (in some rare cases there may be multiple matches due to lsblk printing partitions)
		// If no volume ID is in the output (non-Nitro instances, SBE devices, etc) silently proceed
		volumeRegex := regexp.MustCompile(`vol[a-z0-9]+`)
		for _, volume := range volumeRegex.FindAllString(string(output), -1) {
			klog.V(6).InfoS("Comparing volume serial", "canonicalDevicePath", canonicalDevicePath, "expected", strippedVolumeName, "actual", volume)
			if volume != strippedVolumeName {
				return fmt.Errorf("Refusing to mount %s because it claims to be %s but should be %s", canonicalDevicePath, volume, strippedVolumeName)
			}
		}
	} else {
		// If the command fails (for example, because lsblk is not available), silently ignore the error and proceed
		klog.V(5).ErrorS(err, "Ignoring lsblk failure", "canonicalDevicePath", canonicalDevicePath, "strippedVolumeName", strippedVolumeName)
	}

	return nil
}

// Helper to inject exec.Comamnd().CombinedOutput() for verifyVolumeSerialMatch
// Tests use a mocked version that does not actually execute any binaries
func execRunner(name string, arg ...string) ([]byte, error) {
	return exec.Command(name, arg...).CombinedOutput()
}

func (s *nodeService) appendPartition(devicePath, partition string) string {
	if partition == "" {
		return devicePath
	}

	if strings.HasPrefix(devicePath, "/dev/nvme") {
		return devicePath + nvmeDiskPartitionSuffix + partition
	}

	return devicePath + diskPartitionSuffix + partition
}

// findNvmeVolume looks for the nvme volume with the specified name
// It follows the symlink (if it exists) and returns the absolute path to the device
func findNvmeVolume(deviceIdentifier DeviceIdentifier, findName string) (device string, err error) {
	p := filepath.Join("/dev/disk/by-id/", findName)
	stat, err := deviceIdentifier.Lstat(p)
	if err != nil {
		if os.IsNotExist(err) {
			klog.V(5).InfoS("[Debug] nvme path not found", "path", p)
			return "", fmt.Errorf("nvme path %q not found", p)
		}
		return "", fmt.Errorf("error getting stat of %q: %w", p, err)
	}

	if stat.Mode()&os.ModeSymlink != os.ModeSymlink {
		klog.InfoS("nvme file found, but was not a symlink", "path", p)
		return "", fmt.Errorf("nvme file %q found, but was not a symlink", p)
	}
	// Find the target, resolving to an absolute path
	// For example, /dev/disk/by-id/nvme-Amazon_Elastic_Block_Store_vol0fab1d5e3f72a5e23 -> ../../nvme2n1
	resolved, err := deviceIdentifier.EvalSymlinks(p)
	if err != nil {
		return "", fmt.Errorf("error reading target of symlink %q: %w", p, err)
	}

	if !strings.HasPrefix(resolved, "/dev") {
		return "", fmt.Errorf("resolved symlink for %q was unexpected: %q", p, resolved)
	}

	return resolved, nil
}

func errNoDevicePathFound(devicePath, volumeID string) error {
	return fmt.Errorf("no device path for device %q volume %q found", devicePath, volumeID)
}

func (s *nodeService) getDevicePath(volumeID string) (string, error) {
	var devicePath string
	devicePath, err := s.mounter.GetDevicePathBySerialID(volumeID)
	if err != nil {
		klog.Warningf("Couldn't get device path from mount: %v", err)
	}

	return devicePath, nil
}
