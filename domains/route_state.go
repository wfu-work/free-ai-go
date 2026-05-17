package domains

import common "github.com/wfu-work/nav-common-go-lib/domains"

type RouteState struct {
	common.BaseDataEntity
	RouteKey        string `json:"routeKey" gorm:"size:160;uniqueIndex;comment:路由键"`
	LastAccountGuid string `json:"lastAccountGuid" gorm:"size:50;index;comment:最近账号"`
	Cursor          int    `json:"cursor" gorm:"comment:轮询游标"`
	UpdatedAtUnix   int64  `json:"updatedAtUnix" gorm:"index;comment:更新时间"`
}

func (RouteState) TableName() string { return "fmg_route_state" }
