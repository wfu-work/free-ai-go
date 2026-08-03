package domains

import common "github.com/wfu-work/nav-common-go-lib/domains"

type Account struct {
	common.BaseDataEntity
	VendorCode            string `json:"vendorCode" gorm:"size:40;index;comment:官方厂商标识"`
	ProductCode           string `json:"productCode" gorm:"size:40;index;comment:官方产品标识"`
	CredentialType        string `json:"credentialType" gorm:"size:40;index;comment:官方凭据类型"`
	Name                  string `json:"name" gorm:"size:100;comment:账号名称"`
	Email                 string `json:"email" gorm:"size:255;index;comment:邮箱"`
	ChatGPTAccountID      string `json:"chatgptAccountId" gorm:"size:120;index;comment:ChatGPT账号ID"`
	WorkspaceID           string `json:"workspaceId" gorm:"size:120;index;comment:工作区ID"`
	EncryptedAccountFile  string `json:"-" gorm:"comment:加密OAuth账号文件"`
	CredentialHint        string `json:"credentialHint" gorm:"size:120;comment:账号凭据提示"`
	PlanType              string `json:"planType" gorm:"size:80;index;comment:账号套餐"`
	SubscriptionPlan      string `json:"subscriptionPlan" gorm:"size:120;comment:订阅套餐"`
	SubscriptionRenewsAt  int64  `json:"subscriptionRenewsAt" gorm:"comment:订阅续费时间"`
	SubscriptionWillRenew *bool  `json:"subscriptionWillRenew" gorm:"comment:是否自动续费"`
	AccessTokenExpiresAt  int64  `json:"accessTokenExpiresAt" gorm:"index;comment:访问令牌过期时间"`
	TokenStatus           string `json:"tokenStatus" gorm:"size:40;index;comment:令牌状态"`
	LastError             string `json:"lastError,omitempty" gorm:"comment:最近同步错误"`
	AccountGroup          string `json:"accountGroup" gorm:"size:80;index;comment:账号分组"`
	Status                string `json:"status" gorm:"size:40;index;comment:状态"`
	Priority              int    `json:"priority" gorm:"index;comment:顺序"`
	Weight                int    `json:"weight" gorm:"comment:权重"`
	Enabled               bool   `json:"enabled" gorm:"index;comment:是否启用"`
	LastUsedAt            int64  `json:"lastUsedAt" gorm:"index;comment:最后使用时间"`
	LastRefreshedAt       int64  `json:"lastRefreshedAt" gorm:"comment:最后刷新时间"`
	SubscriptionExpiredAt int64  `json:"subscriptionExpiredAt" gorm:"index;comment:订阅过期时间"`
	FailureCount          int    `json:"failureCount" gorm:"comment:连续失败次数"`
	CooldownUntil         int64  `json:"cooldownUntil" gorm:"index;comment:冷却结束时间"`
	Remark                string `json:"remark" gorm:"comment:备注"`
}

func (Account) TableName() string { return "fmg_account" }
