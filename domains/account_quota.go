package domains

import common "github.com/wfu-work/nav-common-go-lib/domains"

type AccountQuota struct {
	common.BaseDataEntity
	AccountGuid        string   `json:"accountGuid" gorm:"size:50;index;comment:账号guid"`
	WindowType         string   `json:"windowType" gorm:"size:120;index;comment:额度窗口"`
	UsedPercent        *float64 `json:"usedPercent" gorm:"comment:已用百分比"`
	LimitWindowSeconds int64    `json:"limitWindowSeconds" gorm:"comment:窗口秒数"`
	ResetAt            int64    `json:"resetAt" gorm:"index;comment:重置时间"`
	Allowed            *bool    `json:"allowed" gorm:"comment:是否允许请求"`
	LimitReached       *bool    `json:"limitReached" gorm:"comment:是否达到上限"`
	Source             string   `json:"source" gorm:"size:40;index;comment:额度来源"`
	NextRefreshAt      int64    `json:"nextRefreshAt" gorm:"index;comment:下次刷新时间"`
	LastSyncedAt       int64    `json:"lastSyncedAt" gorm:"index;comment:最后同步时间"`
	Status             string   `json:"status" gorm:"size:40;index;comment:状态"`
	Extra              string   `json:"extra" gorm:"comment:扩展信息JSON"`
}

func (AccountQuota) TableName() string { return "fmg_account_quota" }
