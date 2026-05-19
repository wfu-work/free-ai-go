package domains

import common "github.com/wfu-work/nav-common-go-lib/domains"

type Account struct {
	common.BaseDataEntity
	Name                  string `json:"name" gorm:"size:100;comment:账号名称"`
	Email                 string `json:"email" gorm:"size:255;index;comment:邮箱或备注"`
	Provider              string `json:"provider" gorm:"size:40;index;comment:平台"`
	APIBaseURL            string `json:"apiBaseUrl" gorm:"size:500;comment:API请求地址"`
	SupplierName          string `json:"supplierName" gorm:"size:120;comment:供应商名称"`
	OfficialURL           string `json:"officialUrl" gorm:"size:500;comment:官网链接"`
	UsageQueryType        string `json:"usageQueryType" gorm:"size:40;index;comment:额度查询类型"`
	UsageAPIURL           string `json:"usageApiUrl" gorm:"size:500;comment:额度查询地址"`
	AccountType           string `json:"accountType" gorm:"size:40;index;comment:账号类型"`
	AuthType              string `json:"authType" gorm:"size:40;comment:认证类型"`
	EncryptedSecret       string `json:"-" gorm:"comment:加密密钥"`
	SecretHint            string `json:"secretHint" gorm:"size:120;comment:密钥提示"`
	SupportedModels       string `json:"supportedModels" gorm:"comment:支持模型JSON"`
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
