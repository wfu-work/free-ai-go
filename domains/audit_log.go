package domains

import common "github.com/wfu-work/nav-common-go-lib/domains"

type AuditLog struct {
	common.BaseDataEntity
	Actor      string `json:"actor" gorm:"size:80;index;comment:操作者"`
	Action     string `json:"action" gorm:"size:100;index;comment:动作"`
	TargetType string `json:"targetType" gorm:"size:40;index;comment:目标类型"`
	TargetGuid string `json:"targetGuid" gorm:"size:50;index;comment:目标guid"`
	Detail     string `json:"detail" gorm:"comment:详情"`
	CreatedAt  int64  `json:"createdAt" gorm:"index;comment:创建时间"`
}

func (AuditLog) TableName() string { return "fmg_audit_log" }
