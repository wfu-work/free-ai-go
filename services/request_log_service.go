package services

import (
	"time"

	"freeai/domains"

	"github.com/google/uuid"
	"github.com/wfu-work/nav-common-go-lib/global"
	commonUtils "github.com/wfu-work/nav-common-go-lib/utils"
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

func (s RequestLogService) List(params map[string]string) (list interface{}, total int64, err error) {
	limit := commonUtils.Str2Int(params["size"])
	offset := limit * (commonUtils.Str2Int(params["page"]) - 1)
	var results []domains.RequestLog
	db := global.NAV_DB.Model(new(domains.RequestLog))
	if params["platformKeyId"] != "" {
		db = db.Where("platform_key_id = ?", params["platformKeyId"])
	}
	if params["accountGuid"] != "" {
		db = db.Where("account_guid = ?", params["accountGuid"])
	}
	if params["model"] != "" {
		db = db.Where("model = ?", params["model"])
	}
	if params["provider"] != "" {
		db = db.Where("provider = ?", params["provider"])
	}
	if params["errorType"] != "" {
		db = db.Where("error_type = ?", params["errorType"])
	}
	if params["statusCode"] != "" {
		db = db.Where("status_code = ?", params["statusCode"])
	}
	if params["content"] != "" {
		like := "%" + params["content"] + "%"
		db = db.Where("request_id LIKE ? OR model LIKE ? OR upstream_model LIKE ? OR provider LIKE ? OR error_type LIKE ?", like, like, like, like, like)
	}
	if err = db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = db.Order("id desc").Limit(limit).Offset(offset).Find(&results).Error
	return results, total, err
}

func (s RequestLogService) ListAll() ([]domains.RequestLog, error) {
	var list []domains.RequestLog
	err := global.NAV_DB.Order("id desc").Limit(5000).Find(&list).Error
	return list, err
}

func (s RequestLogService) GetByGuid(guid string) (domains.RequestLog, error) {
	var log domains.RequestLog
	err := global.NAV_DB.Where("guid = ?", guid).First(&log).Error
	return log, err
}

func (s RequestLogService) Get(guid string) (domains.RequestLog, error) {
	return s.GetByGuid(guid)
}

func (s RequestLogService) DeleteByGuid(guid string) error {
	return global.NAV_DB.Where("guid = ?", guid).Delete(&domains.RequestLog{}).Error
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
