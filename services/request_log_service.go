package services

import (
	"fmt"
	"strings"
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

// UsageTimelinePoint 是用量趋势中的一个时间桶。
type UsageTimelinePoint struct {
	BucketStart  int64   `json:"bucketStart"`
	Requests     int64   `json:"requests"`
	Failures     int64   `json:"failures"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	CachedTokens int64   `json:"cachedTokens"`
	CostMicrousd int64   `json:"costMicrousd"`
	CostAmount   float64 `json:"costAmount"`
}

// ModelUsageTimelinePoint 是一个模型在指定时间桶内的调用统计。
type ModelUsageTimelinePoint struct {
	BucketStart int64 `json:"bucketStart"`
	Requests    int64 `json:"requests"`
	Failures    int64 `json:"failures"`
}

// ModelUsageTimelineSeries 是一个模型的连续调用趋势。
type ModelUsageTimelineSeries struct {
	Model         string                    `json:"model"`
	TotalRequests int64                     `json:"totalRequests"`
	Points        []ModelUsageTimelinePoint `json:"points"`
}

// UsageSummary 是一个时间窗口内的完整请求用量汇总。
type UsageSummary struct {
	Since           int64                      `json:"since"`
	Until           int64                      `json:"until"`
	TotalRequests   int64                      `json:"totalRequests"`
	SuccessRequests int64                      `json:"successRequests"`
	FailedRequests  int64                      `json:"failedRequests"`
	AvgLatencyMs    float64                    `json:"avgLatencyMs"`
	InputTokens     int64                      `json:"inputTokens"`
	OutputTokens    int64                      `json:"outputTokens"`
	CachedTokens    int64                      `json:"cachedTokens"`
	CostMicrousd    int64                      `json:"costMicrousd"`
	CostAmount      float64                    `json:"costAmount"`
	Models          []UsageDimension           `json:"models"`
	Accounts        []UsageDimension           `json:"accounts"`
	PlatformKeys    []UsageDimension           `json:"platformKeys"`
	Timeline        []UsageTimelinePoint       `json:"timeline"`
	ModelTimeline   []ModelUsageTimelineSeries `json:"modelTimeline"`
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
	ErrorSummary      string
	DiagnosticType    string
	DiagnosticSummary string
	Switched          bool
	SwitchCount       int
	SwitchReason      string
	LatencyMs         int64
	PreparationMs     int64
	DNSMs             int64
	ConnectMs         int64
	TLSHandshakeMs    int64
	UpstreamHeaderMs  int64
	FirstEventMs      int64
	FirstTokenMs      int64
	ConnectionReused  bool
	ConnectionTraced  bool
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
		ErrorSummary:      normalizeErrorSummary(input.ErrorSummary),
		DiagnosticType:    input.DiagnosticType,
		DiagnosticSummary: normalizeErrorSummary(input.DiagnosticSummary),
		Switched:          input.Switched,
		SwitchCount:       input.SwitchCount,
		SwitchReason:      input.SwitchReason,
		LatencyMs:         input.LatencyMs,
		PreparationMs:     input.PreparationMs,
		DNSMs:             input.DNSMs,
		ConnectMs:         input.ConnectMs,
		TLSHandshakeMs:    input.TLSHandshakeMs,
		UpstreamHeaderMs:  input.UpstreamHeaderMs,
		FirstEventMs:      input.FirstEventMs,
		FirstTokenMs:      input.FirstTokenMs,
		ConnectionReused:  input.ConnectionReused,
		ConnectionTraced:  input.ConnectionTraced,
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
		db = db.Where("request_id LIKE ? OR method LIKE ? OR path LIKE ? OR model LIKE ? OR upstream_model LIKE ? OR error_type LIKE ? OR error_summary LIKE ? OR diagnostic_type LIKE ? OR diagnostic_summary LIKE ? OR account_name LIKE ? OR platform_key LIKE ? OR key_prefix LIKE ?", like, like, like, like, like, like, like, like, like, like, like, like)
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
	// 每个聚合步骤都必须使用独立查询。GORM 的 Select、Group、Order 会保存在
	// 当前 Statement 中；复用同一个查询会把维度统计的 requests 排序带入趋势查询。
	newUsageQuery := func() *gorm.DB {
		return global.NAV_DB.Model(&domains.RequestLog{}).
			Where("created_at_unix >= ? AND created_at_unix <= ?", since, until)
	}
	var totals usageTotalsRow
	if err := newUsageQuery().Select(`
		COUNT(*) AS total_requests,
		COALESCE(SUM(CASE WHEN status_code >= 400 OR error_type <> '' THEN 1 ELSE 0 END), 0) AS failed_requests,
		COALESCE(AVG(latency_ms), 0) AS avg_latency_ms,
		COALESCE(SUM(input_tokens), 0) AS input_tokens,
		COALESCE(SUM(output_tokens), 0) AS output_tokens,
		COALESCE(SUM(cached_input_tokens), 0) AS cached_tokens,
		COALESCE(SUM(cost_microusd), 0) AS cost_microusd`).Scan(&totals).Error; err != nil {
		return UsageSummary{}, err
	}
	models, err := queryUsageDimensions(newUsageQuery(), "model")
	if err != nil {
		return UsageSummary{}, err
	}
	accounts, err := queryUsageDimensions(newUsageQuery(), "account_name")
	if err != nil {
		return UsageSummary{}, err
	}
	platformKeys, err := queryUsageDimensions(newUsageQuery(), "key_prefix")
	if err != nil {
		return UsageSummary{}, err
	}
	timeline, modelTimeline, err := queryUsageTimeline(newUsageQuery(), since, until, models)
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
		Timeline: timeline, ModelTimeline: modelTimeline,
	}, nil
}

type usageTimelineRow struct {
	CreatedAtUnix int64  `gorm:"column:created_at_unix"`
	Model         string `gorm:"column:model"`
	Failed        int64  `gorm:"column:failed"`
	InputTokens   int64  `gorm:"column:input_tokens"`
	OutputTokens  int64  `gorm:"column:output_tokens"`
	CachedTokens  int64  `gorm:"column:cached_tokens"`
	CostMicrousd  int64  `gorm:"column:cost_microusd"`
}

// queryUsageTimeline 按时间窗口生成连续趋势点。最多返回 90 个点，避免长周期数据拖慢管理端渲染。
func queryUsageTimeline(db *gorm.DB, since, until int64, models []UsageDimension) ([]UsageTimelinePoint, []ModelUsageTimelineSeries, error) {
	const maxBuckets = 90
	dayMs := (24 * time.Hour).Milliseconds()
	duration := until - since
	if duration <= 0 {
		duration = dayMs
	}

	bucketCount := int((duration + dayMs - 1) / dayMs)
	if bucketCount < 1 {
		bucketCount = 1
	}
	bucketSize := dayMs
	if bucketCount > maxBuckets {
		bucketCount = maxBuckets
		bucketSize = (duration + int64(bucketCount) - 1) / int64(bucketCount)
	}

	points := make([]UsageTimelinePoint, bucketCount)
	for i := range points {
		points[i].BucketStart = since + int64(i)*bucketSize
	}
	modelSeries, modelIndexes, otherModelIndex := newModelUsageTimeline(models, points)

	rows, err := db.Select(`
		created_at_unix,
		model,
		CASE WHEN status_code >= 400 OR error_type <> '' THEN 1 ELSE 0 END AS failed,
		input_tokens,
		output_tokens,
		cached_input_tokens AS cached_tokens,
		cost_microusd`).Rows()
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var row usageTimelineRow
		if err := db.ScanRows(rows, &row); err != nil {
			return nil, nil, err
		}
		index := int((row.CreatedAtUnix - since) / bucketSize)
		if index < 0 {
			continue
		}
		if index >= len(points) {
			index = len(points) - 1
		}

		point := &points[index]
		point.Requests++
		point.Failures += row.Failed
		point.InputTokens += row.InputTokens
		point.OutputTokens += row.OutputTokens
		point.CachedTokens += row.CachedTokens
		point.CostMicrousd += row.CostMicrousd

		model := strings.TrimSpace(row.Model)
		if model == "" {
			model = "未标识"
		}
		seriesIndex, exists := modelIndexes[model]
		if !exists && otherModelIndex >= 0 {
			seriesIndex, exists = otherModelIndex, true
		}
		if exists {
			series := &modelSeries[seriesIndex]
			series.TotalRequests++
			series.Points[index].Requests++
			series.Points[index].Failures += row.Failed
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	for i := range points {
		points[i].CostAmount = microusdToUSD(points[i].CostMicrousd)
	}
	if otherModelIndex >= 0 && modelSeries[otherModelIndex].TotalRequests == 0 {
		modelSeries = modelSeries[:otherModelIndex]
	}
	return points, modelSeries, nil
}

// newModelUsageTimeline 初始化调用量最高的 5 个模型，并为剩余模型预留“其他”系列。
func newModelUsageTimeline(models []UsageDimension, points []UsageTimelinePoint) ([]ModelUsageTimelineSeries, map[string]int, int) {
	const maxModels = 5
	count := len(models)
	if count > maxModels {
		count = maxModels
	}
	series := make([]ModelUsageTimelineSeries, 0, count+1)
	indexes := make(map[string]int, count+1)
	newSeries := func(model string) ModelUsageTimelineSeries {
		modelPoints := make([]ModelUsageTimelinePoint, len(points))
		for i := range modelPoints {
			modelPoints[i].BucketStart = points[i].BucketStart
		}
		return ModelUsageTimelineSeries{Model: model, Points: modelPoints}
	}
	appendSeries := func(model string) {
		indexes[model] = len(series)
		series = append(series, newSeries(model))
	}
	for i := 0; i < count; i++ {
		appendSeries(models[i].Dimension)
	}
	otherIndex := -1
	if len(models) > maxModels {
		otherIndex = len(series)
		series = append(series, newSeries("其他"))
	}
	return series, indexes, otherIndex
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
