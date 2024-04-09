package cloud

type MetadataService interface {
	GetInstanceID() string
	GetProjectID() string
	GetAvailabilityZone() string
}

type IVServerMetadata interface {
	GetMetadata() *VServerMetadata
}
