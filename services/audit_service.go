package services

import (
	"encoding/json"
	"time"

	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/nav-common-go-lib/global"
)

type AuditService struct{}

var AuditServiceApp = AuditService{}

func (s AuditService) Record(actor, action, targetType, targetGuid string, detail any) {
	payload := ""
	if detail != nil {
		if b, err := json.Marshal(detail); err == nil {
			payload = string(b)
		}
	}
	_ = global.NAV_DB.Create(&domains.AuditLog{
		Actor:      actor,
		Action:     action,
		TargetType: targetType,
		TargetGuid: targetGuid,
		Detail:     payload,
		CreatedAt:  time.Now().UnixMilli(),
	}).Error
}
