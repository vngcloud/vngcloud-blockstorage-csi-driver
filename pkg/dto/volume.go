package dto

import (
	lsdkVolV2 "github.com/vngcloud/vngcloud-go-sdk/vngcloud/services/blockstorage/v2/volume"
	"strings"
)

type CreateVolumeRequest struct {
	ClusterID          string // ClusterID is the ID of the cluster where the volume will be created.
	VolumeTypeID       string // the volume type UUID from the VngCloud portal API.
	VolumeName         string // the name of the PersistentVolume
	IsMultiAttach      bool   // whether the volume can be attached to multiple nodes
	VolumeSize         uint64 // the size of the volume in GB
	EncryptedAlgorithm *string
	PvcNameTag         string // the name of the PVC on the PVC's Annotation
	PvcNamespaceTag    string // the namespace of the PVC on the PVC's Annotation
	PvNameTag          string // the name of the PV on the PVC's Annotation

	// The scope of mount commands
	BlockSize       string
	InodeSize       string
	BytesPerInode   string
	NumberOfInodes  string
	Ext4ClusterSize string
	Ext4BigAlloc    bool
}

type encryptedAlgorithm string

const (
	AesXtsPlain64_128 = encryptedAlgorithm("aes-xts-plain64_128")
	AesXtsPlain64_256 = encryptedAlgorithm("aes-xts-plain64_256")
)

func NewCreateVolumeRequest() *CreateVolumeRequest {
	return &CreateVolumeRequest{}
}

func (s *CreateVolumeRequest) WithClusterID(pclusterID string) *CreateVolumeRequest {
	s.ClusterID = pclusterID
	return s
}

func (s *CreateVolumeRequest) WithVolumeTypeID(pvolumeTypeID string) *CreateVolumeRequest {
	s.VolumeTypeID = pvolumeTypeID
	return s
}

func (s *CreateVolumeRequest) WithVolumeName(pvolumeName string) *CreateVolumeRequest {
	s.VolumeName = pvolumeName
	return s
}

func (s *CreateVolumeRequest) WithMultiAttach(pisMultiAttach bool) *CreateVolumeRequest {
	s.IsMultiAttach = pisMultiAttach
	return s
}

func (s *CreateVolumeRequest) WithVolumeSize(pvolumeSize uint64) *CreateVolumeRequest {
	s.VolumeSize = pvolumeSize
	return s
}

func (s *CreateVolumeRequest) WithPvcNameTag(ppvcNameTag string) *CreateVolumeRequest {
	s.PvcNameTag = ppvcNameTag
	return s
}

func (s *CreateVolumeRequest) WithPvcNamespaceTag(ppvcNamespaceTag string) *CreateVolumeRequest {
	s.PvcNamespaceTag = ppvcNamespaceTag
	return s
}

func (s *CreateVolumeRequest) WithPvNameTag(ppvNameTag string) *CreateVolumeRequest {
	s.PvNameTag = ppvNameTag
	return s
}

func (s *CreateVolumeRequest) WithBlockSize(pblockSize string) *CreateVolumeRequest {
	s.BlockSize = pblockSize
	return s
}

func (s *CreateVolumeRequest) WithInodeSize(pinodeSize string) *CreateVolumeRequest {
	s.InodeSize = pinodeSize
	return s
}

func (s *CreateVolumeRequest) WithBytesPerInode(pbytesPerInode string) *CreateVolumeRequest {
	s.BytesPerInode = pbytesPerInode
	return s
}

func (s *CreateVolumeRequest) WithNumberOfInodes(pnumberOfInodes string) *CreateVolumeRequest {
	s.NumberOfInodes = pnumberOfInodes
	return s
}

func (s *CreateVolumeRequest) WithExt4BigAlloc(pext4BigAlloc bool) *CreateVolumeRequest {
	s.Ext4BigAlloc = pext4BigAlloc
	return s
}

func (s *CreateVolumeRequest) WithExt4ClusterSize(pext4ClusterSize string) *CreateVolumeRequest {
	s.Ext4ClusterSize = pext4ClusterSize
	return s
}

func (s *CreateVolumeRequest) WithEncrypted(pencrypted string) *CreateVolumeRequest {
	pencrypted = strings.ToLower(pencrypted)
	switch pencrypted {
	case string(AesXtsPlain64_128):
		s.EncryptedAlgorithm = &pencrypted
	case string(AesXtsPlain64_256):
		s.EncryptedAlgorithm = &pencrypted
	default:
		s.EncryptedAlgorithm = nil
	}

	return s
}

func (s *CreateVolumeRequest) ToSdkCreateVolumeOpts() *lsdkVolV2.CreateOpts {
	opts := new(lsdkVolV2.CreateOpts)

	// !TODO: Implement this method

	return opts
}
