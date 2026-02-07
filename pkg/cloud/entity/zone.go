package entity

import lsdkEntity "github.com/vngcloud/vngcloud-go-sdk/v2/vngcloud/entity"

type Zone struct {
	*lsdkEntity.Zone
}

type ListZones struct {
	*lsdkEntity.ListZones
}

func (s *ListZones) Len() int {
	return len(s.Items)
}

func (s *ListZones) IsEmpty() bool {
	return s.Len() < 1
}
