package domains

import common "github.com/wfu-work/nav-common-go-lib/domains"

type PlatformKey struct {
	common.BaseDataEntity
	Name               string  `json:"name" gorm:"size:100;comment:密钥名称"`
	KeyHash            string  `json:"-" gorm:"size:128;uniqueIndex;comment:密钥哈希"`
	KeyPrefix          string  `json:"keyPrefix" gorm:"size:20;index;comment:密钥前缀"`
	EncryptedKey       string  `json:"-" gorm:"comment:加密完整密钥"`
	Key                string  `json:"key,omitempty" gorm:"-"`
	AllowedModels      string  `json:"allowedModels" gorm:"comment:允许模型JSON"`
	RoutingStrategy    string  `json:"routingStrategy" gorm:"size:40;index;comment:轮转策略"`
	AccountGroupFilter string  `json:"accountGroupFilter" gorm:"size:80;index;comment:账号组筛选"`
	TotalTokenLimit    int64   `json:"totalTokenLimit" gorm:"comment:总Token额度限制"`
	TokenLimitUnit     string  `json:"tokenLimitUnit" gorm:"size:10;comment:Token额度单位"`
	ProtocolType       string  `json:"protocolType" gorm:"size:40;index;comment:协议类型"`
	BoundModel         string  `json:"boundModel" gorm:"size:100;index;comment:绑定模型"`
	ReasoningEffort    string  `json:"reasoningEffort" gorm:"size:40;comment:推理等级"`
	ServiceTier        string  `json:"serviceTier" gorm:"size:40;comment:服务等级"`
	RateLimitPerMinute int     `json:"rateLimitPerMinute" gorm:"comment:每分钟限制"`
	Enabled            bool    `json:"enabled" gorm:"index;comment:是否启用"`
	LastUsedAt         int64   `json:"lastUsedAt" gorm:"index;comment:最后使用时间"`
	Remark             string  `json:"remark" gorm:"comment:备注"`
	UsedTokens         int64   `json:"usedTokens" gorm:"-"`
	UsedAmount         float64 `json:"usedAmount" gorm:"-"`
}

func (PlatformKey) TableName() string { return "fmg_platform_key" }
