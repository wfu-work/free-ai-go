package domains

import common "github.com/wfu-work/nav-common-go-lib/domains"

type PlatformKey struct {
	common.BaseDataEntity
	Name               string `json:"name" gorm:"size:100;comment:密钥名称"`
	KeyHash            string `json:"-" gorm:"size:128;uniqueIndex;comment:密钥哈希"`
	KeyPrefix          string `json:"keyPrefix" gorm:"size:20;index;comment:密钥前缀"`
	AllowedModels      string `json:"allowedModels" gorm:"comment:允许模型JSON"`
	RateLimitPerMinute int    `json:"rateLimitPerMinute" gorm:"comment:每分钟限制"`
	Enabled            bool   `json:"enabled" gorm:"index;comment:是否启用"`
	LastUsedAt         int64  `json:"lastUsedAt" gorm:"index;comment:最后使用时间"`
	Remark             string `json:"remark" gorm:"comment:备注"`
}

func (PlatformKey) TableName() string { return "fmg_platform_key" }
