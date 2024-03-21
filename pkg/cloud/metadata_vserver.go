package cloud

type VServerMetadataClient func() (VServerMetadata, error)

var DefaultVServerMetadataClient = func() (VServerMetadata, error) {
	sess := session.Must(session.NewSession(&aws.Config{}))
	svc := ec2metadata.New(sess)
	return svc, nil
}
