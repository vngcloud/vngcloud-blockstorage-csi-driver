package entity

import lsdkEntity "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/entity"

type Snapshot struct {
	*lsdkEntity.Snapshot
}

type ListSnapshots struct {
	*lsdkEntity.ListSnapshots
}

func (s *ListSnapshots) Len() int {
	return len(s.Items)
}
