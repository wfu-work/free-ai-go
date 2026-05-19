package domains

import common "github.com/wfu-work/nav-common-go-lib/domains"

type AccountQuota struct {
	common.BaseDataEntity
	AccountGuid     string  `json:"accountGuid" gorm:"size:50;index;comment:账号guid"`
	WindowType      string  `json:"windowType" gorm:"size:40;index;comment:窗口类型"`
	UsedPercent     float64 `json:"usedPercent" gorm:"comment:已用百分比"`
	RemainingTokens int64   `json:"remainingTokens" gorm:"comment:剩余Token"`
	TotalTokens     int64   `json:"totalTokens" gorm:"comment:总Token"`
	Unit            string  `json:"unit" gorm:"size:20;comment:额度单位"`
	UsedAmount      float64 `json:"usedAmount" gorm:"comment:已用额度"`
	RemainingAmount float64 `json:"remainingAmount" gorm:"comment:剩余额度"`
	TotalAmount     float64 `json:"totalAmount" gorm:"comment:总额度"`
	ResetAt         int64   `json:"resetAt" gorm:"index;comment:重置时间"`
	NextRefreshAt   int64   `json:"nextRefreshAt" gorm:"index;comment:下次刷新时间"`
	LastSyncedAt    int64   `json:"lastSyncedAt" gorm:"index;comment:最后同步时间"`
	Status          string  `json:"status" gorm:"size:40;index;comment:状态"`
	Extra           string  `json:"extra" gorm:"comment:扩展信息JSON"`
}

func (AccountQuota) TableName() string { return "fmg_account_quota" }
