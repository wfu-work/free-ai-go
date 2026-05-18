package domains

import common "github.com/wfu-work/nav-common-go-lib/domains"

type ModelMapping struct {
	common.BaseDataEntity
	PublicModel   string `json:"publicModel" gorm:"size:100;uniqueIndex;comment:对外模型"`
	Aliases       string `json:"aliases" gorm:"comment:模型别名JSON或逗号分隔"`
	UpstreamModel string `json:"upstreamModel" gorm:"size:100;comment:上游模型"`
	Provider      string `json:"provider" gorm:"size:40;index;comment:平台"`
	AccountGroup  string `json:"accountGroup" gorm:"size:80;index;comment:账号分组"`
	Stream        bool   `json:"stream" gorm:"comment:是否支持流式"`
	TimeoutSec    int    `json:"timeoutSec" gorm:"comment:超时秒数"`
	Enabled       bool   `json:"enabled" gorm:"index;comment:是否启用"`
}

func (ModelMapping) TableName() string { return "fmg_model_mapping" }
