package domains

import common "github.com/wfu-work/nav-common-go-lib/domains"

type AccountGroup struct {
	common.BaseDataEntity
	Name                  string `json:"name" gorm:"size:80;uniqueIndex;comment:分组名称"`
	Description           string `json:"description" gorm:"size:500;comment:分组说明"`
	Sort                  int    `json:"sort" gorm:"index;comment:排序"`
	Enabled               bool   `json:"enabled" gorm:"index;comment:是否启用"`
	ProviderSummary       string `json:"providerSummary" gorm:"comment:供应商摘要JSON"`
	AccountTypeSummary    string `json:"accountTypeSummary" gorm:"comment:账号类型摘要JSON"`
	ModelSummary          string `json:"modelSummary" gorm:"comment:模型摘要JSON"`
	AccountCount          int    `json:"accountCount" gorm:"comment:账号数量"`
	EnabledAccountCount   int    `json:"enabledAccountCount" gorm:"comment:启用账号数量"`
	AvailableAccountCount int    `json:"availableAccountCount" gorm:"comment:可用账号数量"`
	ModelCount            int    `json:"modelCount" gorm:"comment:模型映射数量"`
	EnabledModelCount     int    `json:"enabledModelCount" gorm:"comment:启用模型映射数量"`
	SummarySyncedAt       int64  `json:"summarySyncedAt" gorm:"index;comment:摘要同步时间"`
	Remark                string `json:"remark" gorm:"comment:备注"`
}

func (AccountGroup) TableName() string { return "fmg_account_group" }
