package services

import (
	"time"

	"freeai/domains"

	"github.com/google/uuid"
	"github.com/wfu-work/nav-common-go-lib/global"
	"gorm.io/gorm"
)

type RequestLogService struct{}

var RequestLogServiceApp = RequestLogService{}

type RequestLogInput struct {
	PlatformKeyID string
	AccountGuid   string
	Model         string
	UpstreamModel string
	Provider      string
	StatusCode    int
	ErrorType     string
	Switched      bool
	SwitchCount   int
	SwitchReason  string
	LatencyMs     int64
	FirstTokenMs  int64
	InputTokens   int64
	OutputTokens  int64
}

func (s RequestLogService) Record(input RequestLogInput) {
	_ = global.NAV_DB.Create(&domains.RequestLog{
		RequestID:     uuid.NewString(),
		PlatformKeyID: input.PlatformKeyID,
		AccountGuid:   input.AccountGuid,
		Model:         input.Model,
		UpstreamModel: input.UpstreamModel,
		Provider:      input.Provider,
		StatusCode:    input.StatusCode,
		ErrorType:     input.ErrorType,
		Switched:      input.Switched,
		SwitchCount:   input.SwitchCount,
		SwitchReason:  input.SwitchReason,
		LatencyMs:     input.LatencyMs,
		FirstTokenMs:  input.FirstTokenMs,
		InputTokens:   input.InputTokens,
		OutputTokens:  input.OutputTokens,
		CreatedAtUnix: time.Now().UnixMilli(),
	}).Error
}

func (s RequestLogService) List(limit int) ([]domains.RequestLog, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var list []domains.RequestLog
	err := global.NAV_DB.Order("id desc").Limit(limit).Find(&list).Error
	return list, err
}

func (s RequestLogService) Get(guid string) (domains.RequestLog, error) {
	var log domains.RequestLog
	err := global.NAV_DB.Where("guid = ?", guid).First(&log).Error
	return log, err
}

func (s RequestLogService) ClearBefore(cutoffMs int64) error {
	query := global.NAV_DB
	if cutoffMs > 0 {
		query = query.Where("created_at_unix < ?", cutoffMs)
	} else {
		query = query.Session(&gorm.Session{AllowGlobalUpdate: true})
	}
	return query.Delete(&domains.RequestLog{}).Error
}

func (s RequestLogService) CleanupExpired(retentionDays int) error {
	if retentionDays <= 0 {
		retentionDays = Config().CleanupLogRetentionDays
	}
	if retentionDays <= 0 {
		return nil
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).UnixMilli()
	return s.ClearBefore(cutoff)
}

func (s RequestLogService) Stats() (map[string]any, error) {
	var total int64
	var failures int64
	var avgLatency float64
	if err := global.NAV_DB.Model(&domains.RequestLog{}).Count(&total).Error; err != nil {
		return nil, err
	}
	if err := global.NAV_DB.Model(&domains.RequestLog{}).Where("status_code >= ? OR error_type <> ?", 400, "").Count(&failures).Error; err != nil {
		return nil, err
	}
	if err := global.NAV_DB.Model(&domains.RequestLog{}).Select("COALESCE(AVG(latency_ms), 0)").Scan(&avgLatency).Error; err != nil {
		return nil, err
	}
	success := total - failures
	if success < 0 {
		success = 0
	}
	return map[string]any{
		"total":        total,
		"success":      success,
		"failures":     failures,
		"avgLatencyMs": avgLatency,
	}, nil
}
