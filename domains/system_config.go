package domains

import common "github.com/wfu-work/nav-common-go-lib/domains"

type SystemConfig struct {
	common.BaseDataEntity
	ConfigKey   string `json:"configKey" gorm:"size:120;uniqueIndex;comment:配置键"`
	ConfigValue string `json:"configValue" gorm:"comment:配置值"`
	ValueType   string `json:"valueType" gorm:"size:30;comment:值类型"`
	Group       string `json:"group" gorm:"size:60;index;comment:配置分组"`
	Remark      string `json:"remark" gorm:"comment:备注"`
}

func (SystemConfig) TableName() string { return "fmg_system_config" }
