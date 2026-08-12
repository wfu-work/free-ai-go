package domains

import common "github.com/wfu-work/nav-common-go-lib/domains"

// AccountResetCreditRedemption 持久化额度重置券的幂等兑换状态。
// pending 包含请求结果未知的情况；重试时必须继续使用同一个 idempotency_key。
type AccountResetCreditRedemption struct {
	common.BaseDataEntity
	AccountGuid    string `json:"accountGuid" gorm:"size:50;index;not null"`
	IdempotencyKey string `json:"idempotencyKey" gorm:"size:64;uniqueIndex;not null"`
	CreditID       string `json:"creditId" gorm:"size:160;index"`
	Outcome        string `json:"outcome" gorm:"size:40"`
	Status         string `json:"status" gorm:"size:30;index;not null"`
	LastError      string `json:"lastError,omitempty" gorm:"size:1000"`
	CreatedAt      int64  `json:"createdAt" gorm:"index"`
	CompletedAt    int64  `json:"completedAt" gorm:"index"`
}

func (AccountResetCreditRedemption) TableName() string {
	return "fmg_account_reset_credit_redemption"
}
