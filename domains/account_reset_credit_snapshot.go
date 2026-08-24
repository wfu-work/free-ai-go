package domains

import common "github.com/wfu-work/nav-common-go-lib/domains"

// AccountResetCreditSnapshot 持久化账号最近一次查询到的官方额度重置券快照。
// CreditsJSON 只包含可安全展示的券 ID、状态和时间，不包含 OAuth 凭据。
type AccountResetCreditSnapshot struct {
	common.BaseDataEntity
	AccountGuid              string `json:"accountGuid" gorm:"size:50;uniqueIndex;not null"`
	AvailableCount           int    `json:"availableCount" gorm:"not null;default:0"`
	ApplicableAvailableCount *int   `json:"applicableAvailableCount,omitempty"`
	DetailsAvailable         bool   `json:"detailsAvailable" gorm:"not null;default:false"`
	CreditsJSON              string `json:"-" gorm:"type:text;not null"`
	ExpiresAt                int64  `json:"expiresAt" gorm:"index"`
	SyncedAt                 int64  `json:"syncedAt" gorm:"index;not null"`
}

func (AccountResetCreditSnapshot) TableName() string {
	return "fmg_account_reset_credit_snapshot"
}
