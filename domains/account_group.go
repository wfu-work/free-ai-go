package domains

import common "github.com/wfu-work/nav-common-go-lib/domains"

type AccountGroup struct {
	common.BaseDataEntity
	Name        string `json:"name" gorm:"size:80;uniqueIndex;comment:分组名称"`
	Description string `json:"description" gorm:"size:500;comment:分组说明"`
	Sort        int    `json:"sort" gorm:"index;comment:排序"`
	Enabled     bool   `json:"enabled" gorm:"index;comment:是否启用"`
	Remark      string `json:"remark" gorm:"comment:备注"`
}

func (AccountGroup) TableName() string { return "fmg_account_group" }
