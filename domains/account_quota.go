package domains

import common "github.com/wfu-work/nav-common-go-lib/domains"

type AccountQuota struct {
	common.BaseDataEntity
	AccountGuid     string  `json:"accountGuid" gorm:"size:50;index;comment:账号guid"`
	WindowType      string  `json:"windowType" gorm:"size:40;index;comment:窗口类型"`
	UsedPercent     float64 `json:"usedPercent" gorm:"comment:已用百分比"`
	RemainingTokens int64   `json:"remainingTokens" gorm:"comment:剩余Token"`
	TotalTokens     int64   `json:"totalTokens" gorm:"comment:总Token"`
	ResetAt         int64   `json:"resetAt" gorm:"index;comment:重置时间"`
	NextRefreshAt   int64   `json:"nextRefreshAt" gorm:"index;comment:下次刷新时间"`
	Status          string  `json:"status" gorm:"size:40;index;comment:状态"`
}

func (AccountQuota) TableName() string { return "fmg_account_quota" }
