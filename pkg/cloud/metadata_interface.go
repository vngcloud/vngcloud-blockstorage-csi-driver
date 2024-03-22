package cloud

type MetadataService interface {
	GetInstanceID() string
	GetProjectID() string
}

type IVServerMetadata interface {
	GetMetadata() *VServerMetadata
}
