package services

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/nav-common-go-lib/global"
	"github.com/wfu-work/proxy-api-lib/catalog"
	proxyopenai "github.com/wfu-work/proxy-api-lib/openai"
	"gorm.io/gorm"
)

const officialPricingSyncTimeout = 30 * time.Second

// ModelPricingService 管理官方公开模型定价的同步和持久化。
type ModelPricingService struct{}

// ModelPricingServiceApp 是模型定价服务的全局实例。
var ModelPricingServiceApp = ModelPricingService{}

// ModelPricingSyncResult 汇总一次官方定价同步结果。
type ModelPricingSyncResult struct {
	Checked       int    `json:"checked"`
	Created       int    `json:"created"`
	Updated       int    `json:"updated"`
	Preserved     int    `json:"preserved"`
	Deactivated   int64  `json:"deactivated"`
	SourceKind    string `json:"sourceKind"`
	SourceVersion string `json:"sourceVersion,omitempty"`
	SourceURL     string `json:"sourceUrl"`
	SyncedAt      int64  `json:"syncedAt"`
	Warning       string `json:"warning,omitempty"`
}

// ModelCostEstimate 是一次请求按官方 API 参考价计算出的成本。
// CostMicrousd 使用微美元整数，写库和汇总时不会产生浮点累计误差。
type ModelCostEstimate struct {
	CostMicrousd int64
	Matched      bool
	SourceKind   string
}

// EstimateCost 根据模型、服务等级和真实 Token 用量估算 API 参考成本。
//
// cachedInputTokens 已经包含在 inputTokens 中，因此普通输入只计算两者的差值。
func (s ModelPricingService) EstimateCost(vendor, model, serviceTier string, inputTokens, cachedInputTokens, outputTokens int64) ModelCostEstimate {
	vendor = strings.TrimSpace(vendor)
	if vendor == "" {
		vendor = catalog.VendorOpenAI
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return ModelCostEstimate{}
	}
	tier := normalizePricingServiceTier(serviceTier)
	contextTier := catalog.PricingContextShort
	if inputTokens > 272000 {
		contextTier = catalog.PricingContextLong
	}
	price, err := s.findActivePrice(vendor, model, tier, contextTier)
	if err != nil && contextTier == catalog.PricingContextLong {
		price, err = s.findActivePrice(vendor, model, tier, catalog.PricingContextShort)
	}
	if err != nil && tier != catalog.PricingTierStandard {
		price, err = s.findActivePrice(vendor, model, catalog.PricingTierStandard, contextTier)
	}
	if err != nil && contextTier == catalog.PricingContextLong {
		price, err = s.findActivePrice(vendor, model, catalog.PricingTierStandard, catalog.PricingContextShort)
	}
	if err != nil {
		return ModelCostEstimate{}
	}
	if inputTokens < 0 {
		inputTokens = 0
	}
	if cachedInputTokens < 0 {
		cachedInputTokens = 0
	}
	if cachedInputTokens > inputTokens {
		cachedInputTokens = inputTokens
	}
	if outputTokens < 0 {
		outputTokens = 0
	}
	regularInputTokens := inputTokens - cachedInputTokens
	cost := pricingComponentCost(regularInputTokens, price.InputMicrousdPer1M)
	if price.CachedInputMicrousdPer1M != nil {
		cost += pricingComponentCost(cachedInputTokens, price.CachedInputMicrousdPer1M)
	} else {
		cost += pricingComponentCost(cachedInputTokens, price.InputMicrousdPer1M)
	}
	cost += pricingComponentCost(outputTokens, price.OutputMicrousdPer1M)
	return ModelCostEstimate{CostMicrousd: cost, Matched: true, SourceKind: price.SourceKind}
}

// findActivePrice 查找一条当前有效的官方定价。
func (s ModelPricingService) findActivePrice(vendor, model, serviceTier, contextTier string) (domains.ModelPricing, error) {
	var price domains.ModelPricing
	err := global.NAV_DB.Where(
		"vendor_code = ? AND remote_model_id = ? AND scope = ? AND service_tier = ? AND context_tier = ? AND active = ?",
		vendor, model, catalog.PricingScopeAPIReference, serviceTier, contextTier, true,
	).Order("last_synced_at desc").First(&price).Error
	return price, err
}

// normalizePricingServiceTier 把网关请求中的服务等级映射到定价表等级。
func normalizePricingServiceTier(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case catalog.PricingTierBatch:
		return catalog.PricingTierBatch
	case catalog.PricingTierFlex:
		return catalog.PricingTierFlex
	case catalog.PricingTierPriority:
		return catalog.PricingTierPriority
	default:
		return catalog.PricingTierStandard
	}
}

// pricingComponentCost 将某类 Token 按每百万 Token 的微美元单价换算并四舍五入。
func pricingComponentCost(tokens int64, microusdPerMillion *int64) int64 {
	if tokens <= 0 || microusdPerMillion == nil || *microusdPerMillion <= 0 {
		return 0
	}
	return (tokens**microusdPerMillion + 500000) / 1000000
}

// SyncOfficial 从 OpenAI 官方定价文档同步 API 参考价。
//
// 该方法复用系统的官方上游代理配置；实时文档不可用时，底层库会返回带警告的官方快照。
func (s ModelPricingService) SyncOfficial(ctx context.Context) (ModelPricingSyncResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	httpClient, err := UpstreamHTTPClient()
	if err != nil {
		return ModelPricingSyncResult{}, err
	}
	syncContext, cancel := context.WithTimeout(ctx, officialPricingSyncTimeout)
	defer cancel()

	snapshot, err := proxyopenai.NewOfficialPricingSource(httpClient).Fetch(syncContext)
	if err != nil {
		return ModelPricingSyncResult{}, err
	}
	result, err := s.syncSnapshot(snapshot)
	if err != nil {
		return ModelPricingSyncResult{}, err
	}
	_, _ = s.BackfillRequestCosts(10000)
	_ = PlatformKeyServiceApp.ReconcileUsageFromLogs()
	return result, nil
}

// BackfillRequestCosts 为升级前没有成本字段的历史请求日志补算官方参考成本。
func (s ModelPricingService) BackfillRequestCosts(limit int) (int, error) {
	if limit <= 0 || limit > 50000 {
		limit = 10000
	}
	var logs []domains.RequestLog
	if err := global.NAV_DB.Where(
		"pricing_matched = ? AND upstream_model <> ? AND (input_tokens > ? OR output_tokens > ?)",
		false, "", 0, 0,
	).Order("id asc").Limit(limit).Find(&logs).Error; err != nil {
		return 0, err
	}
	updated := 0
	for _, log := range logs {
		estimate := s.EstimateCost(
			catalog.VendorOpenAI, log.UpstreamModel, log.ServiceTier,
			log.InputTokens, log.CachedInputTokens, log.OutputTokens,
		)
		if !estimate.Matched {
			continue
		}
		if err := global.NAV_DB.Model(&log).Updates(map[string]any{
			"cost_microusd": estimate.CostMicrousd, "pricing_matched": true,
			"pricing_source": estimate.SourceKind,
		}).Error; err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

// syncSnapshot 将已经过官方数据源解析的价格快照写入本地数据库。
func (s ModelPricingService) syncSnapshot(snapshot *catalog.PricingSnapshot) (ModelPricingSyncResult, error) {
	if snapshot == nil {
		return ModelPricingSyncResult{}, errors.New("official pricing snapshot is nil")
	}
	prices := normalizeModelPrices(snapshot.Prices)
	if len(prices) == 0 {
		return ModelPricingSyncResult{}, errors.New("official pricing snapshot is empty")
	}
	syncedAt := snapshot.FetchedAt
	if syncedAt <= 0 {
		syncedAt = time.Now().UnixMilli()
	}
	result := ModelPricingSyncResult{
		Checked: len(prices), SourceKind: strings.TrimSpace(snapshot.SourceKind),
		SourceVersion: strings.TrimSpace(snapshot.SourceVersion), SourceURL: strings.TrimSpace(snapshot.SourceURL),
		SyncedAt: syncedAt, Warning: strings.TrimSpace(snapshot.Warning),
	}
	err := global.NAV_DB.Transaction(func(tx *gorm.DB) error {
		return s.persistSnapshot(tx, prices, snapshot, syncedAt, &result)
	})
	if err != nil {
		return ModelPricingSyncResult{}, err
	}
	return result, nil
}

// persistSnapshot 在单个事务内更新价格，并按实时来源停用已经消失的旧价格。
func (s ModelPricingService) persistSnapshot(tx *gorm.DB, prices []catalog.ModelPrice, snapshot *catalog.PricingSnapshot, syncedAt int64, result *ModelPricingSyncResult) error {
	vendors, scopes := pricingVendorScopes(prices)
	var existing []domains.ModelPricing
	if err := tx.Where("vendor_code IN ? AND scope IN ?", vendors, scopes).Find(&existing).Error; err != nil {
		return err
	}
	existingByIdentity := make(map[string]domains.ModelPricing, len(existing))
	for _, item := range existing {
		existingByIdentity[modelPricingIdentity(item.VendorCode, item.RemoteModelID, item.Scope, item.ServiceTier, item.ContextTier)] = item
	}
	seen := make(map[string]bool, len(prices))
	for _, price := range prices {
		identity := modelPricingIdentity(price.VendorCode, price.RemoteModelID, price.Scope, price.ServiceTier, price.ContextTier)
		seen[identity] = true
		stored, found := existingByIdentity[identity]
		if !found {
			stored = domains.ModelPricing{
				VendorCode: price.VendorCode, RemoteModelID: price.RemoteModelID, Scope: price.Scope,
				ServiceTier: price.ServiceTier, ContextTier: price.ContextTier,
			}
			applyCatalogPrice(&stored, price, snapshot, syncedAt)
			if err := tx.Create(&stored).Error; err != nil {
				return err
			}
			result.Created++
			continue
		}
		// 短暂网络失败不应让较旧的内置快照覆盖已经成功同步过的实时官方价格。
		if snapshot.SourceKind == catalog.PricingSourceOfficialDocsSnapshot &&
			stored.SourceKind == catalog.PricingSourceOfficialDocsLive && stored.Active {
			result.Preserved++
			continue
		}
		updates := modelPricingUpdates(price, snapshot, syncedAt)
		if err := tx.Model(&stored).Updates(updates).Error; err != nil {
			return err
		}
		result.Updated++
	}
	if snapshot.SourceKind != catalog.PricingSourceOfficialDocsLive {
		return nil
	}
	staleGuids := make([]string, 0)
	for _, item := range existing {
		identity := modelPricingIdentity(item.VendorCode, item.RemoteModelID, item.Scope, item.ServiceTier, item.ContextTier)
		if item.Active && !seen[identity] {
			staleGuids = append(staleGuids, item.Guid)
		}
	}
	if len(staleGuids) == 0 {
		return nil
	}
	update := tx.Model(&domains.ModelPricing{}).Where("guid IN ?", staleGuids).
		Updates(map[string]any{"active": false, "update_time": time.Now().UnixMilli()})
	result.Deactivated = update.RowsAffected
	return update.Error
}

// applyCatalogPrice 把公共库价格对象复制到数据库实体。
func applyCatalogPrice(target *domains.ModelPricing, price catalog.ModelPrice, snapshot *catalog.PricingSnapshot, syncedAt int64) {
	target.Currency = price.Currency
	target.Unit = price.Unit
	target.InputMicrousdPer1M = price.InputMicrousdPer1M
	target.CachedInputMicrousdPer1M = price.CachedInputMicrousdPer1M
	target.CacheWriteMicrousdPer1M = price.CacheWriteMicrousdPer1M
	target.OutputMicrousdPer1M = price.OutputMicrousdPer1M
	target.SourceURL = strings.TrimSpace(snapshot.SourceURL)
	target.SourceKind = strings.TrimSpace(snapshot.SourceKind)
	target.SourceVersion = strings.TrimSpace(snapshot.SourceVersion)
	target.LastSyncedAt = syncedAt
	target.Active = true
}

// modelPricingUpdates 生成包含空价格字段的更新集合，确保官方删除某项价格后本地也能同步清空。
func modelPricingUpdates(price catalog.ModelPrice, snapshot *catalog.PricingSnapshot, syncedAt int64) map[string]any {
	return map[string]any{
		"currency": price.Currency, "unit": price.Unit,
		"input_microusd_per1_m":        price.InputMicrousdPer1M,
		"cached_input_microusd_per1_m": price.CachedInputMicrousdPer1M,
		"cache_write_microusd_per1_m":  price.CacheWriteMicrousdPer1M,
		"output_microusd_per1_m":       price.OutputMicrousdPer1M,
		"source_url":                   strings.TrimSpace(snapshot.SourceURL), "source_kind": strings.TrimSpace(snapshot.SourceKind),
		"source_version": strings.TrimSpace(snapshot.SourceVersion), "last_synced_at": syncedAt,
		"active": true, "update_time": time.Now().UnixMilli(),
	}
}

// normalizeModelPrices 清理并去重官方定价行。
func normalizeModelPrices(values []catalog.ModelPrice) []catalog.ModelPrice {
	byIdentity := make(map[string]catalog.ModelPrice, len(values))
	for _, value := range values {
		value.VendorCode = strings.TrimSpace(value.VendorCode)
		value.RemoteModelID = strings.TrimSpace(value.RemoteModelID)
		value.Scope = strings.TrimSpace(value.Scope)
		value.ServiceTier = strings.TrimSpace(value.ServiceTier)
		value.ContextTier = strings.TrimSpace(value.ContextTier)
		value.Currency = strings.TrimSpace(value.Currency)
		value.Unit = strings.TrimSpace(value.Unit)
		if value.VendorCode == "" || value.RemoteModelID == "" || value.Scope == "" ||
			value.ServiceTier == "" || value.ContextTier == "" || !value.HasPrice() {
			continue
		}
		identity := modelPricingIdentity(value.VendorCode, value.RemoteModelID, value.Scope, value.ServiceTier, value.ContextTier)
		byIdentity[identity] = value
	}
	result := make([]catalog.ModelPrice, 0, len(byIdentity))
	for _, value := range byIdentity {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		return modelPricingIdentity(result[i].VendorCode, result[i].RemoteModelID, result[i].Scope, result[i].ServiceTier, result[i].ContextTier) <
			modelPricingIdentity(result[j].VendorCode, result[j].RemoteModelID, result[j].Scope, result[j].ServiceTier, result[j].ContextTier)
	})
	return result
}

// pricingVendorScopes 返回查询现有价格所需的厂商和范围集合。
func pricingVendorScopes(prices []catalog.ModelPrice) ([]string, []string) {
	vendorSet, scopeSet := map[string]bool{}, map[string]bool{}
	for _, price := range prices {
		vendorSet[price.VendorCode] = true
		scopeSet[price.Scope] = true
	}
	vendors, scopes := make([]string, 0, len(vendorSet)), make([]string, 0, len(scopeSet))
	for vendor := range vendorSet {
		vendors = append(vendors, vendor)
	}
	for scope := range scopeSet {
		scopes = append(scopes, scope)
	}
	sort.Strings(vendors)
	sort.Strings(scopes)
	return vendors, scopes
}

// modelPricingIdentity 生成模型价格的稳定唯一身份。
func modelPricingIdentity(vendor, model, scope, serviceTier, contextTier string) string {
	return strings.Join([]string{vendor, model, scope, serviceTier, contextTier}, "\x00")
}
