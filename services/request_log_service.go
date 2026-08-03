package services

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/nav-common-go-lib/global"
	commonUtils "github.com/wfu-work/nav-common-go-lib/utils"
	"gorm.io/gorm"
)

type RequestLogService struct{}

var RequestLogServiceApp = RequestLogService{}

// UsageDimension 是用量分析按一个维度聚合后的统计结果。
type UsageDimension struct {
	Dimension    string  `json:"dimension" gorm:"column:dimension"`
	Requests     int64   `json:"requests" gorm:"column:requests"`
	Failures     int64   `json:"failures" gorm:"column:failures"`
	InputTokens  int64   `json:"inputTokens" gorm:"column:input_tokens"`
	OutputTokens int64   `json:"outputTokens" gorm:"column:output_tokens"`
	CachedTokens int64   `json:"cachedTokens" gorm:"column:cached_tokens"`
	CostMicrousd int64   `json:"costMicrousd" gorm:"column:cost_microusd"`
	CostAmount   float64 `json:"costAmount" gorm:"-"`
}

// UsageSummary 是一个时间窗口内的完整请求用量汇总。
type UsageSummary struct {
	Since           int64            `json:"since"`
	Until           int64            `json:"until"`
	TotalRequests   int64            `json:"totalRequests"`
	SuccessRequests int64            `json:"successRequests"`
	FailedRequests  int64            `json:"failedRequests"`
	AvgLatencyMs    float64          `json:"avgLatencyMs"`
	InputTokens     int64            `json:"inputTokens"`
	OutputTokens    int64            `json:"outputTokens"`
	CachedTokens    int64            `json:"cachedTokens"`
	CostMicrousd    int64            `json:"costMicrousd"`
	CostAmount      float64          `json:"costAmount"`
	Models          []UsageDimension `json:"models"`
	Accounts        []UsageDimension `json:"accounts"`
	PlatformKeys    []UsageDimension `json:"platformKeys"`
}

type RequestLogInput struct {
	RequestID         string
	Method            string
	Path              string
	PlatformKeyID     string
	PlatformKey       string
	KeyPrefix         string
	AccountGuid       string
	AccountName       string
	Model             string
	UpstreamModel     string
	ReasoningEffort   string
	ServiceTier       string
	StatusCode        int
	ErrorType         string
	Switched          bool
	SwitchCount       int
	SwitchReason      string
	LatencyMs         int64
	FirstTokenMs      int64
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
	CostMicrousd      int64
	PricingMatched    bool
	PricingSource     string
}

func (s RequestLogService) Record(input RequestLogInput) error {
	requestID := input.RequestID
	if requestID == "" {
		requestID = uuid.NewString()
	}
	return global.NAV_DB.Create(&domains.RequestLog{
		RequestID:         requestID,
		Method:            input.Method,
		Path:              input.Path,
		PlatformKeyID:     input.PlatformKeyID,
		PlatformKey:       input.PlatformKey,
		KeyPrefix:         input.KeyPrefix,
		AccountGuid:       input.AccountGuid,
		AccountName:       input.AccountName,
		Model:             input.Model,
		UpstreamModel:     input.UpstreamModel,
		ReasoningEffort:   input.ReasoningEffort,
		ServiceTier:       input.ServiceTier,
		StatusCode:        input.StatusCode,
		ErrorType:         input.ErrorType,
		Switched:          input.Switched,
		SwitchCount:       input.SwitchCount,
		SwitchReason:      input.SwitchReason,
		LatencyMs:         input.LatencyMs,
		FirstTokenMs:      input.FirstTokenMs,
		InputTokens:       input.InputTokens,
		CachedInputTokens: input.CachedInputTokens,
		OutputTokens:      input.OutputTokens,
		CostMicrousd:      input.CostMicrousd,
		PricingMatched:    input.PricingMatched,
		PricingSource:     input.PricingSource,
		CreatedAtUnix:     time.Now().UnixMilli(),
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
	if params["errorType"] != "" {
		db = db.Where("error_type = ?", params["errorType"])
	}
	if params["statusCode"] != "" {
		db = db.Where("status_code = ?", params["statusCode"])
	}
	if params["content"] != "" {
		like := "%" + params["content"] + "%"
		db = db.Where("request_id LIKE ? OR method LIKE ? OR path LIKE ? OR model LIKE ? OR upstream_model LIKE ? OR error_type LIKE ? OR account_name LIKE ? OR platform_key LIKE ? OR key_prefix LIKE ?", like, like, like, like, like, like, like, like, like)
	}
	if err = db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = db.Order("id desc").Limit(limit).Offset(offset).Find(&results).Error
	s.attachAmounts(results)
	return results, total, err
}

func (s RequestLogService) ListAll(limit int, since int64) ([]domains.RequestLog, error) {
	if limit <= 0 {
		limit = 5000
	}
	if limit > 50000 {
		limit = 50000
	}
	var list []domains.RequestLog
	db := global.NAV_DB.Model(new(domains.RequestLog))
	if since > 0 {
		db = db.Where("created_at_unix >= ?", since)
	}
	err := db.Order("id desc").Limit(limit).Find(&list).Error
	s.attachAmounts(list)
	return list, err
}

func (s RequestLogService) GetByGuid(guid string) (domains.RequestLog, error) {
	var log domains.RequestLog
	err := global.NAV_DB.Where("guid = ?", guid).First(&log).Error
	log.CostAmount = microusdToUSD(log.CostMicrousd)
	return log, err
}

// attachAmounts 为管理端补充便于展示的美元金额。
func (s RequestLogService) attachAmounts(logs []domains.RequestLog) {
	for i := range logs {
		logs[i].CostAmount = microusdToUSD(logs[i].CostMicrousd)
	}
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

type usageTotalsRow struct {
	TotalRequests  int64   `gorm:"column:total_requests"`
	FailedRequests int64   `gorm:"column:failed_requests"`
	AvgLatencyMs   float64 `gorm:"column:avg_latency_ms"`
	InputTokens    int64   `gorm:"column:input_tokens"`
	OutputTokens   int64   `gorm:"column:output_tokens"`
	CachedTokens   int64   `gorm:"column:cached_tokens"`
	CostMicrousd   int64   `gorm:"column:cost_microusd"`
}

// UsageSummary 按模型、账号和 API 密钥统计请求用量，避免前端拉取大量日志后重复聚合。
func (s RequestLogService) UsageSummary(since, until int64) (UsageSummary, error) {
	if until <= 0 {
		until = time.Now().UnixMilli()
	}
	if since <= 0 || since > until {
		since = until - 30*24*time.Hour.Milliseconds()
	}
	db := global.NAV_DB.Model(&domains.RequestLog{}).Where("created_at_unix >= ? AND created_at_unix <= ?", since, until)
	var totals usageTotalsRow
	if err := db.Select(`
		COUNT(*) AS total_requests,
		COALESCE(SUM(CASE WHEN status_code >= 400 OR error_type <> '' THEN 1 ELSE 0 END), 0) AS failed_requests,
		COALESCE(AVG(latency_ms), 0) AS avg_latency_ms,
		COALESCE(SUM(input_tokens), 0) AS input_tokens,
		COALESCE(SUM(output_tokens), 0) AS output_tokens,
		COALESCE(SUM(cached_input_tokens), 0) AS cached_tokens,
		COALESCE(SUM(cost_microusd), 0) AS cost_microusd`).Scan(&totals).Error; err != nil {
		return UsageSummary{}, err
	}
	models, err := queryUsageDimensions(db, "model")
	if err != nil {
		return UsageSummary{}, err
	}
	accounts, err := queryUsageDimensions(db, "account_name")
	if err != nil {
		return UsageSummary{}, err
	}
	platformKeys, err := queryUsageDimensions(db, "key_prefix")
	if err != nil {
		return UsageSummary{}, err
	}
	success := totals.TotalRequests - totals.FailedRequests
	if success < 0 {
		success = 0
	}
	return UsageSummary{
		Since: since, Until: until,
		TotalRequests: totals.TotalRequests, SuccessRequests: success, FailedRequests: totals.FailedRequests,
		AvgLatencyMs: totals.AvgLatencyMs,
		InputTokens:  totals.InputTokens, OutputTokens: totals.OutputTokens, CachedTokens: totals.CachedTokens,
		CostMicrousd: totals.CostMicrousd, CostAmount: microusdToUSD(totals.CostMicrousd),
		Models: models, Accounts: accounts, PlatformKeys: platformKeys,
	}, nil
}

func queryUsageDimensions(db *gorm.DB, column string) ([]UsageDimension, error) {
	if column != "model" && column != "account_name" && column != "key_prefix" {
		return nil, fmt.Errorf("unsupported usage dimension: %s", column)
	}
	var rows []UsageDimension
	query := fmt.Sprintf(`
		COALESCE(NULLIF(%s, ''), '未标识') AS dimension,
		COUNT(*) AS requests,
		COALESCE(SUM(CASE WHEN status_code >= 400 OR error_type <> '' THEN 1 ELSE 0 END), 0) AS failures,
		COALESCE(SUM(input_tokens), 0) AS input_tokens,
		COALESCE(SUM(output_tokens), 0) AS output_tokens,
		COALESCE(SUM(cached_input_tokens), 0) AS cached_tokens,
		COALESCE(SUM(cost_microusd), 0) AS cost_microusd`, column)
	if err := db.Select(query).Group(column).Order("requests desc").Limit(20).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].CostAmount = microusdToUSD(rows[i].CostMicrousd)
	}
	return rows, nil
}
