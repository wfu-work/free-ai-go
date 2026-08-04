package services

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/nav-common-go-lib/global"
	commonUtils "github.com/wfu-work/nav-common-go-lib/utils"
	"github.com/wfu-work/proxy-api-lib/catalog"
	"gorm.io/gorm"
)

// ModelService 管理官方模型目录、账号可用性和对外暴露策略。
type ModelService struct{}

var ModelServiceApp = ModelService{}

// ModelCatalogItem 是管理端使用的模型目录聚合视图。
type ModelCatalogItem struct {
	Guid                   string           `json:"guid"`
	CreateTime             int64            `json:"createTime"`
	UpdateTime             int64            `json:"updateTime"`
	VendorCode             string           `json:"vendorCode"`
	ProductCode            string           `json:"productCode"`
	UpstreamProtocol       string           `json:"upstreamProtocol"`
	RemoteModelID          string           `json:"remoteModelId"`
	DisplayName            string           `json:"displayName"`
	Description            string           `json:"description"`
	OwnedBy                string           `json:"ownedBy"`
	CapabilitiesJSON       string           `json:"capabilitiesJson"`
	ReasoningEfforts       []string         `json:"reasoningEfforts"`
	DefaultReasoningEffort string           `json:"defaultReasoningEffort"`
	Source                 string           `json:"source"`
	RemoteCreatedAt        int64            `json:"remoteCreatedAt"`
	FirstSeenAt            int64            `json:"firstSeenAt"`
	LastSeenAt             int64            `json:"lastSeenAt"`
	Deprecated             bool             `json:"deprecated"`
	ExposureGuid           string           `json:"exposureGuid"`
	PublicModel            string           `json:"publicModel"`
	Aliases                string           `json:"aliases"`
	AccountGroup           string           `json:"accountGroup"`
	TimeoutSec             int              `json:"timeoutSec"`
	Enabled                bool             `json:"enabled"`
	Visible                bool             `json:"visible"`
	AvailableAccountCount  int64            `json:"availableAccountCount"`
	Pricing                []ModelPriceItem `json:"pricing"`
	PricingUpdatedAt       int64            `json:"pricingUpdatedAt"`
	PricingSourceURL       string           `json:"pricingSourceUrl"`
	PricingSourceKind      string           `json:"pricingSourceKind"`
	PricingSourceVersion   string           `json:"pricingSourceVersion,omitempty"`
}

// ModelPriceItem 是模型目录返回给管理端的单条官方 API 参考价。
type ModelPriceItem struct {
	Scope                    string `json:"scope"`
	ServiceTier              string `json:"serviceTier"`
	ContextTier              string `json:"contextTier"`
	Currency                 string `json:"currency"`
	Unit                     string `json:"unit"`
	InputMicrousdPer1M       *int64 `json:"inputMicrousdPer1M,omitempty"`
	CachedInputMicrousdPer1M *int64 `json:"cachedInputMicrousdPer1M,omitempty"`
	CacheWriteMicrousdPer1M  *int64 `json:"cacheWriteMicrousdPer1M,omitempty"`
	OutputMicrousdPer1M      *int64 `json:"outputMicrousdPer1M,omitempty"`
	SourceURL                string `json:"sourceUrl"`
	SourceKind               string `json:"sourceKind"`
	SourceVersion            string `json:"sourceVersion,omitempty"`
	LastSyncedAt             int64  `json:"lastSyncedAt"`
}

// ModelPolicyInput 是用户可以修改的对外模型策略，不允许修改官方远端模型身份。
type ModelPolicyInput struct {
	PublicModel  string `json:"publicModel"`
	Aliases      string `json:"aliases"`
	AccountGroup string `json:"accountGroup"`
	TimeoutSec   int    `json:"timeoutSec"`
	Enabled      *bool  `json:"enabled"`
	Visible      *bool  `json:"visible"`
}

// RoutedModel 是请求路由真正需要的模型信息。
type RoutedModel struct {
	CatalogGuid      string `json:"catalogGuid"`
	ExposureGuid     string `json:"exposureGuid"`
	VendorCode       string `json:"vendorCode"`
	ProductCode      string `json:"productCode"`
	UpstreamProtocol string `json:"upstreamProtocol"`
	PublicModel      string `json:"publicModel"`
	Aliases          string `json:"aliases"`
	UpstreamModel    string `json:"upstreamModel"`
	AccountGroup     string `json:"accountGroup"`
	TimeoutSec       int    `json:"timeoutSec"`
	Created          int64  `json:"created"`
	OwnedBy          string `json:"ownedBy"`
	Enabled          bool   `json:"enabled"`
	Visible          bool   `json:"visible"`
}

// ModelSyncStats 汇总一次账号模型同步的持久化结果。
type ModelSyncStats struct {
	AccountGuid string   `json:"accountGuid"`
	Models      []string `json:"models"`
	Discovered  int      `json:"discovered"`
	Created     int      `json:"created"`
	Updated     int      `json:"updated"`
	Unavailable int64    `json:"unavailable"`
}

// ModelAccountItem 描述模型与账号池的可用性关系。
type ModelAccountItem struct {
	AccountGuid  string `json:"accountGuid"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	AccountGroup string `json:"accountGroup"`
	Status       string `json:"status"`
	Enabled      bool   `json:"enabled"`
	Available    bool   `json:"available"`
	FirstSeenAt  int64  `json:"firstSeenAt"`
	LastSeenAt   int64  `json:"lastSeenAt"`
	LastError    string `json:"lastError,omitempty"`
}

// List 分页查询模型目录，并附加对外策略和可用账号数量。
func (s ModelService) List(params map[string]string) (list interface{}, total int64, err error) {
	limit := commonUtils.Str2Int(params["size"])
	if limit <= 0 {
		limit = 10
	}
	page := commonUtils.Str2Int(params["page"])
	if page <= 0 {
		page = 1
	}
	offset := limit * (page - 1)
	db := global.NAV_DB.Model(&domains.ModelCatalog{}).
		Joins("LEFT JOIN fmg_model_exposure exposure ON exposure.model_catalog_guid = fmg_model_catalog.guid AND exposure.deleted_time IS NULL")
	if value := strings.TrimSpace(params["vendorCode"]); value != "" {
		db = db.Where("fmg_model_catalog.vendor_code = ?", value)
	}
	if value := strings.TrimSpace(params["productCode"]); value != "" {
		db = db.Where("fmg_model_catalog.product_code = ?", value)
	}
	if value := strings.TrimSpace(params["enabled"]); value != "" {
		db = db.Where("exposure.enabled = ?", value)
	}
	if content := strings.TrimSpace(params["content"]); content != "" {
		like := "%" + content + "%"
		db = db.Where("fmg_model_catalog.remote_model_id LIKE ? OR fmg_model_catalog.display_name LIKE ? OR fmg_model_catalog.description LIKE ? OR exposure.public_model LIKE ? OR exposure.aliases LIKE ?", like, like, like, like, like)
	}
	if err = db.Distinct("fmg_model_catalog.guid").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var catalogs []domains.ModelCatalog
	err = db.Select("fmg_model_catalog.*").Order("fmg_model_catalog.last_seen_at desc, fmg_model_catalog.id desc").Limit(limit).Offset(offset).Find(&catalogs).Error
	if err != nil {
		return nil, 0, err
	}
	items, err := s.attachCatalogState(catalogs)
	return items, total, err
}

// ListAll 返回全部模型目录，供平台密钥和集成配置选择模型。
func (s ModelService) ListAll() ([]ModelCatalogItem, error) {
	var catalogs []domains.ModelCatalog
	if err := global.NAV_DB.Order("remote_model_id asc").Find(&catalogs).Error; err != nil {
		return nil, err
	}
	return s.attachCatalogState(catalogs)
}

// GetByGuid 根据模型目录 GUID 获取聚合详情。
func (s ModelService) GetByGuid(guid string) (ModelCatalogItem, error) {
	var model domains.ModelCatalog
	if err := global.NAV_DB.Where("guid = ?", strings.TrimSpace(guid)).First(&model).Error; err != nil {
		return ModelCatalogItem{}, err
	}
	items, err := s.attachCatalogState([]domains.ModelCatalog{model})
	if err != nil {
		return ModelCatalogItem{}, err
	}
	return items[0], nil
}

// Get 是 GetByGuid 的兼容入口。
func (s ModelService) Get(guid string) (ModelCatalogItem, error) {
	return s.GetByGuid(guid)
}

// Update 更新模型对外名称、别名和路由范围，不修改官方模型元数据。
func (s ModelService) Update(guid string, input ModelPolicyInput) (ModelCatalogItem, error) {
	var model domains.ModelCatalog
	if err := global.NAV_DB.Where("guid = ?", strings.TrimSpace(guid)).First(&model).Error; err != nil {
		return ModelCatalogItem{}, err
	}
	publicModel := strings.TrimSpace(input.PublicModel)
	if publicModel == "" {
		return ModelCatalogItem{}, errors.New("publicModel is required")
	}
	aliases, err := normalizeAliasesJSON(input.Aliases)
	if err != nil {
		return ModelCatalogItem{}, err
	}
	if err := s.validateExposureNames(model.Guid, publicModel, aliases); err != nil {
		return ModelCatalogItem{}, err
	}
	var exposure domains.ModelExposure
	findErr := global.NAV_DB.Where("model_catalog_guid = ?", model.Guid).First(&exposure).Error
	if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return ModelCatalogItem{}, findErr
	}
	timeoutSec := input.TimeoutSec
	if timeoutSec < 0 {
		return ModelCatalogItem{}, errors.New("timeoutSec must be greater than or equal to 0")
	}
	enabled, visible := true, true
	if findErr == nil {
		enabled, visible = exposure.Enabled, exposure.Visible
	}
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	if input.Visible != nil {
		visible = *input.Visible
	}
	accountGroup := normalizeModelAccountGroup(input.AccountGroup)
	if errors.Is(findErr, gorm.ErrRecordNotFound) {
		exposure = domains.ModelExposure{
			ModelCatalogGuid: model.Guid, PublicModel: publicModel, Aliases: aliases,
			AccountGroup: accountGroup, TimeoutSec: timeoutSec, Enabled: enabled, Visible: visible,
		}
		err = global.NAV_DB.Create(&exposure).Error
	} else {
		oldGroup := exposure.AccountGroup
		err = global.NAV_DB.Model(&exposure).Updates(map[string]any{
			"public_model": publicModel, "aliases": aliases, "account_group": accountGroup,
			"timeout_sec": timeoutSec, "enabled": enabled, "visible": visible,
		}).Error
		if err == nil {
			AccountGroupServiceApp.RefreshSummaries(oldGroup, accountGroup)
		}
	}
	if err != nil {
		return ModelCatalogItem{}, err
	}
	AccountGroupServiceApp.RefreshSummaries(accountGroup)
	AuditServiceApp.Record("", "model.policy.update", "model_catalog", model.Guid, map[string]string{"model": publicModel})
	return s.GetByGuid(model.Guid)
}

// SetEnabled 启用或停用模型的对外路由策略。
func (s ModelService) SetEnabled(guid string, enabled bool) error {
	var exposure domains.ModelExposure
	if err := global.NAV_DB.Where("model_catalog_guid = ?", guid).First(&exposure).Error; err != nil {
		return err
	}
	if err := global.NAV_DB.Model(&exposure).Update("enabled", enabled).Error; err != nil {
		return err
	}
	AccountGroupServiceApp.RefreshSummaries(exposure.AccountGroup)
	AuditServiceApp.Record("", "model.policy.enabled", "model_catalog", guid, map[string]bool{"enabled": enabled})
	return nil
}

// ListEnabled 返回已启用且对外可见的路由模型。
func (s ModelService) ListEnabled() ([]RoutedModel, error) {
	var exposures []domains.ModelExposure
	if err := global.NAV_DB.Where("enabled = ? AND visible = ?", true, true).Order("public_model asc").Find(&exposures).Error; err != nil {
		return nil, err
	}
	models := make([]RoutedModel, 0, len(exposures))
	for _, exposure := range exposures {
		model, err := s.routedModel(exposure)
		if err != nil {
			continue
		}
		models = append(models, model)
	}
	return models, nil
}

// Find 根据对外模型名称或别名解析真正的官方远端模型。
func (s ModelService) Find(publicModel string) (RoutedModel, error) {
	publicModel = strings.TrimSpace(publicModel)
	var exposure domains.ModelExposure
	err := global.NAV_DB.Where("public_model = ? AND enabled = ?", publicModel, true).First(&exposure).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		var candidates []domains.ModelExposure
		if listErr := global.NAV_DB.Where("enabled = ? AND aliases <> ?", true, "").Find(&candidates).Error; listErr != nil {
			return RoutedModel{}, listErr
		}
		for _, candidate := range candidates {
			if modelAliasMatches(candidate.Aliases, publicModel) {
				return s.routedModel(candidate)
			}
		}
		return RoutedModel{}, errors.New(domains.ErrorModelNotSupported)
	}
	if err != nil {
		return RoutedModel{}, err
	}
	return s.routedModel(exposure)
}

// PublicNames 返回模型主名称和全部对外别名。
func (s ModelService) PublicNames(model RoutedModel) []string {
	names := []string{model.PublicModel}
	for _, alias := range parseAliases(model.Aliases) {
		if alias != "" && alias != model.PublicModel {
			names = append(names, alias)
		}
	}
	return names
}

// SyncRemoteModels 在单个事务内更新模型目录和指定账号的模型可用性。
func (s ModelService) SyncRemoteModels(account domains.Account, identity catalog.SourceIdentity, remoteModels []catalog.RemoteModel) (ModelSyncStats, error) {
	models := normalizeRemoteModels(remoteModels)
	if len(models) == 0 {
		return ModelSyncStats{}, errors.New("official model response is empty")
	}
	identity.Vendor = firstNonEmpty(strings.TrimSpace(identity.Vendor), account.VendorCode, domains.VendorOpenAI)
	identity.Product = firstNonEmpty(strings.TrimSpace(identity.Product), account.ProductCode, domains.ProductCodex)
	identity.Protocol = firstNonEmpty(strings.TrimSpace(identity.Protocol), domains.ProtocolOpenAIResponses)
	result := ModelSyncStats{AccountGuid: account.Guid, Discovered: len(models), Models: make([]string, 0, len(models))}
	err := global.NAV_DB.Transaction(func(tx *gorm.DB) error {
		var previouslyAvailable []string
		if err := tx.Model(&domains.AccountModelAvailability{}).
			Where("account_guid = ? AND available = ?", account.Guid, true).Pluck("model_catalog_guid", &previouslyAvailable).Error; err != nil {
			return err
		}
		if err := tx.Model(&domains.AccountModelAvailability{}).
			Where("account_guid = ?", account.Guid).Update("available", false).Error; err != nil {
			return err
		}
		for _, remote := range models {
			model, created, err := s.upsertRemoteCatalog(tx, identity, remote)
			if err != nil {
				return err
			}
			if created {
				result.Created++
			} else {
				result.Updated++
			}
			if err := s.ensureDefaultExposure(tx, model); err != nil {
				return err
			}
			if err := s.upsertAccountAvailability(tx, account.Guid, model.Guid, remote); err != nil {
				return err
			}
			result.Models = append(result.Models, remote.ID)
		}
		if len(previouslyAvailable) > 0 {
			if err := tx.Model(&domains.AccountModelAvailability{}).
				Where("account_guid = ? AND model_catalog_guid IN ? AND available = ?", account.Guid, previouslyAvailable, false).
				Count(&result.Unavailable).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ModelSyncStats{}, err
	}
	s.refreshCatalogDeprecation()
	sort.Strings(result.Models)
	AccountGroupServiceApp.RefreshSummaries(account.AccountGroup)
	return result, nil
}

func (s ModelService) refreshCatalogDeprecation() {
	availableCatalogs := global.NAV_DB.Model(&domains.AccountModelAvailability{}).
		Select("model_catalog_guid").Where("available = ?", true)
	_ = global.NAV_DB.Model(&domains.ModelCatalog{}).
		Where("guid IN (?)", availableCatalogs).Update("deprecated", false).Error
	_ = global.NAV_DB.Model(&domains.ModelCatalog{}).
		Where("guid NOT IN (?)", availableCatalogs).Update("deprecated", true).Error
}

// RecordAccountSyncFailure 记录模型同步失败，但保留上一次成功的模型可用性。
func (s ModelService) RecordAccountSyncFailure(accountGuid string, syncErr error) {
	if syncErr == nil {
		return
	}
	_ = global.NAV_DB.Model(&domains.AccountModelAvailability{}).
		Where("account_guid = ?", accountGuid).Update("last_error", syncErr.Error()).Error
}

// AvailableAccountGuids 返回已确认支持该模型的账号集合。
func (s ModelService) AvailableAccountGuids(modelCatalogGuid string) (map[string]bool, error) {
	var relations []domains.AccountModelAvailability
	if err := global.NAV_DB.Select("account_guid").Where("model_catalog_guid = ? AND available = ?", modelCatalogGuid, true).Find(&relations).Error; err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(relations))
	for _, relation := range relations {
		result[relation.AccountGuid] = true
	}
	return result, nil
}

// FirstAvailableModelForAccount 返回账号最近同步确认可用的第一个模型。
func (s ModelService) FirstAvailableModelForAccount(accountGuid string) (string, error) {
	var model domains.ModelCatalog
	err := global.NAV_DB.Model(&domains.ModelCatalog{}).
		Joins("JOIN fmg_account_model relation ON relation.model_catalog_guid = fmg_model_catalog.guid AND relation.deleted_time IS NULL").
		Where("relation.account_guid = ? AND relation.available = ?", accountGuid, true).
		Order("fmg_model_catalog.remote_model_id asc").First(&model).Error
	return model.RemoteModelID, err
}

// Accounts 返回模型目录关联的全部账号及最近同步状态。
func (s ModelService) Accounts(modelCatalogGuid string) ([]ModelAccountItem, error) {
	type row struct {
		AccountGuid  string
		Name         string
		Email        string
		AccountGroup string
		Status       string
		Enabled      bool
		Available    bool
		FirstSeenAt  int64
		LastSeenAt   int64
		LastError    string
	}
	var rows []row
	err := global.NAV_DB.Table("fmg_account_model AS relation").
		Select("relation.account_guid, account.name, account.email, account.account_group, account.status, account.enabled, relation.available, relation.first_seen_at, relation.last_seen_at, relation.last_error").
		Joins("JOIN fmg_account AS account ON account.guid = relation.account_guid AND account.deleted_time IS NULL").
		Where("relation.model_catalog_guid = ? AND relation.deleted_time IS NULL", modelCatalogGuid).
		Order("relation.available desc, account.priority asc, account.id asc").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	items := make([]ModelAccountItem, 0, len(rows))
	for _, item := range rows {
		items = append(items, ModelAccountItem{
			AccountGuid: item.AccountGuid, Name: item.Name, Email: item.Email, AccountGroup: item.AccountGroup,
			Status: item.Status, Enabled: item.Enabled, Available: item.Available,
			FirstSeenAt: item.FirstSeenAt, LastSeenAt: item.LastSeenAt, LastError: item.LastError,
		})
	}
	return items, nil
}

func (s ModelService) attachCatalogState(catalogs []domains.ModelCatalog) ([]ModelCatalogItem, error) {
	items := make([]ModelCatalogItem, 0, len(catalogs))
	if len(catalogs) == 0 {
		return items, nil
	}
	guids := make([]string, 0, len(catalogs))
	for _, model := range catalogs {
		guids = append(guids, model.Guid)
	}
	var exposures []domains.ModelExposure
	if err := global.NAV_DB.Where("model_catalog_guid IN ?", guids).Find(&exposures).Error; err != nil {
		return nil, err
	}
	exposureByCatalog := make(map[string]domains.ModelExposure, len(exposures))
	for _, exposure := range exposures {
		exposureByCatalog[exposure.ModelCatalogGuid] = exposure
	}
	type countRow struct {
		ModelCatalogGuid string
		Count            int64
	}
	var counts []countRow
	if err := global.NAV_DB.Table("fmg_account_model AS relation").
		Select("relation.model_catalog_guid, COUNT(DISTINCT relation.account_guid) AS count").
		Joins("JOIN fmg_account AS account ON account.guid = relation.account_guid AND account.deleted_time IS NULL").
		Where("relation.deleted_time IS NULL AND relation.available = ? AND account.enabled = ?", true, true).
		Where("relation.model_catalog_guid IN ?", guids).
		Group("relation.model_catalog_guid").Scan(&counts).Error; err != nil {
		return nil, err
	}
	countByCatalog := make(map[string]int64, len(counts))
	for _, count := range counts {
		countByCatalog[count.ModelCatalogGuid] = count.Count
	}
	pricingByModel, err := loadCatalogPricing(catalogs)
	if err != nil {
		return nil, err
	}
	for _, model := range catalogs {
		exposure := exposureByCatalog[model.Guid]
		capabilities := catalog.DecodeModelCapabilities(json.RawMessage(model.CapabilitiesJSON))
		pricing := pricingByModel[modelPricingCatalogKey(model.VendorCode, model.RemoteModelID)]
		if pricing == nil {
			pricing = make([]ModelPriceItem, 0)
		}
		pricingUpdatedAt, pricingSourceURL, pricingSourceKind, pricingSourceVersion := latestPricingSource(pricing)
		items = append(items, ModelCatalogItem{
			Guid: model.Guid, CreateTime: model.CreateTime, UpdateTime: model.UpdateTime,
			VendorCode: model.VendorCode, ProductCode: model.ProductCode, UpstreamProtocol: model.UpstreamProtocol,
			RemoteModelID: model.RemoteModelID, DisplayName: model.DisplayName, Description: model.Description,
			OwnedBy: model.OwnedBy, CapabilitiesJSON: model.CapabilitiesJSON,
			ReasoningEfforts: capabilities.ReasoningEfforts, DefaultReasoningEffort: capabilities.DefaultReasoningEffort,
			Source:          model.Source,
			RemoteCreatedAt: model.RemoteCreatedAt, FirstSeenAt: model.FirstSeenAt, LastSeenAt: model.LastSeenAt,
			Deprecated: model.Deprecated, ExposureGuid: exposure.Guid, PublicModel: exposure.PublicModel,
			Aliases: exposure.Aliases, AccountGroup: exposure.AccountGroup, TimeoutSec: exposure.TimeoutSec,
			Enabled: exposure.Enabled, Visible: exposure.Visible, AvailableAccountCount: countByCatalog[model.Guid],
			Pricing: pricing, PricingUpdatedAt: pricingUpdatedAt, PricingSourceURL: pricingSourceURL,
			PricingSourceKind: pricingSourceKind, PricingSourceVersion: pricingSourceVersion,
		})
	}
	return items, nil
}

// loadCatalogPricing 批量读取当前页模型的有效价格，避免逐模型查询数据库。
func loadCatalogPricing(catalogs []domains.ModelCatalog) (map[string][]ModelPriceItem, error) {
	vendorSet, modelSet := map[string]bool{}, map[string]bool{}
	for _, model := range catalogs {
		vendorSet[model.VendorCode] = true
		modelSet[model.RemoteModelID] = true
	}
	vendors, remoteModelIDs := make([]string, 0, len(vendorSet)), make([]string, 0, len(modelSet))
	for vendor := range vendorSet {
		vendors = append(vendors, vendor)
	}
	for remoteModelID := range modelSet {
		remoteModelIDs = append(remoteModelIDs, remoteModelID)
	}
	var stored []domains.ModelPricing
	if err := global.NAV_DB.Where("vendor_code IN ? AND remote_model_id IN ? AND active = ?", vendors, remoteModelIDs, true).
		Find(&stored).Error; err != nil {
		return nil, err
	}
	result := make(map[string][]ModelPriceItem)
	for _, price := range stored {
		key := modelPricingCatalogKey(price.VendorCode, price.RemoteModelID)
		result[key] = append(result[key], ModelPriceItem{
			Scope: price.Scope, ServiceTier: price.ServiceTier, ContextTier: price.ContextTier,
			Currency: price.Currency, Unit: price.Unit,
			InputMicrousdPer1M:       price.InputMicrousdPer1M,
			CachedInputMicrousdPer1M: price.CachedInputMicrousdPer1M,
			CacheWriteMicrousdPer1M:  price.CacheWriteMicrousdPer1M,
			OutputMicrousdPer1M:      price.OutputMicrousdPer1M,
			SourceURL:                price.SourceURL, SourceKind: price.SourceKind,
			SourceVersion: price.SourceVersion, LastSyncedAt: price.LastSyncedAt,
		})
	}
	for key := range result {
		sort.SliceStable(result[key], func(i, j int) bool {
			left, right := result[key][i], result[key][j]
			if pricingServiceTierOrder(left.ServiceTier) != pricingServiceTierOrder(right.ServiceTier) {
				return pricingServiceTierOrder(left.ServiceTier) < pricingServiceTierOrder(right.ServiceTier)
			}
			if pricingContextTierOrder(left.ContextTier) != pricingContextTierOrder(right.ContextTier) {
				return pricingContextTierOrder(left.ContextTier) < pricingContextTierOrder(right.ContextTier)
			}
			return left.Scope < right.Scope
		})
	}
	return result, nil
}

// latestPricingSource 返回模型价格中最近一次同步的来源信息。
func latestPricingSource(prices []ModelPriceItem) (int64, string, string, string) {
	var latest ModelPriceItem
	for _, price := range prices {
		if price.LastSyncedAt > latest.LastSyncedAt {
			latest = price
		}
	}
	return latest.LastSyncedAt, latest.SourceURL, latest.SourceKind, latest.SourceVersion
}

// modelPricingCatalogKey 生成模型目录与定价数据的匹配键。
func modelPricingCatalogKey(vendorCode, remoteModelID string) string {
	return strings.TrimSpace(vendorCode) + "\x00" + strings.TrimSpace(remoteModelID)
}

// pricingServiceTierOrder 返回服务层级的稳定展示顺序。
func pricingServiceTierOrder(value string) int {
	switch value {
	case catalog.PricingTierStandard:
		return 0
	case catalog.PricingTierBatch:
		return 1
	case catalog.PricingTierFlex:
		return 2
	case catalog.PricingTierPriority:
		return 3
	default:
		return 100
	}
}

// pricingContextTierOrder 返回上下文价格区间的稳定展示顺序。
func pricingContextTierOrder(value string) int {
	if value == catalog.PricingContextShort {
		return 0
	}
	if value == catalog.PricingContextLong {
		return 1
	}
	return 100
}

func (s ModelService) routedModel(exposure domains.ModelExposure) (RoutedModel, error) {
	var catalogModel domains.ModelCatalog
	if err := global.NAV_DB.Where("guid = ?", exposure.ModelCatalogGuid).First(&catalogModel).Error; err != nil {
		return RoutedModel{}, err
	}
	return RoutedModel{
		CatalogGuid: catalogModel.Guid, ExposureGuid: exposure.Guid,
		VendorCode: catalogModel.VendorCode, ProductCode: catalogModel.ProductCode,
		UpstreamProtocol: catalogModel.UpstreamProtocol, PublicModel: exposure.PublicModel,
		Aliases: exposure.Aliases, UpstreamModel: catalogModel.RemoteModelID,
		AccountGroup: exposure.AccountGroup, TimeoutSec: exposure.TimeoutSec,
		Created: modelCreatedUnix(catalogModel), OwnedBy: catalogModel.OwnedBy,
		Enabled: exposure.Enabled, Visible: exposure.Visible,
	}, nil
}

func modelCreatedUnix(model domains.ModelCatalog) int64 {
	created := model.RemoteCreatedAt
	if created == 0 {
		created = model.FirstSeenAt
	}
	if created > 10_000_000_000 {
		created /= 1000
	}
	return created
}

func (s ModelService) upsertRemoteCatalog(db *gorm.DB, identity catalog.SourceIdentity, remote catalog.RemoteModel) (domains.ModelCatalog, bool, error) {
	now := time.Now().UnixMilli()
	var model domains.ModelCatalog
	err := db.Where("vendor_code = ? AND product_code = ? AND remote_model_id = ?", identity.Vendor, identity.Product, remote.ID).First(&model).Error
	created := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !created {
		return domains.ModelCatalog{}, false, err
	}
	displayName := strings.TrimSpace(remote.DisplayName)
	if displayName == "" {
		displayName = remote.ID
	}
	ownedBy := firstNonEmpty(strings.TrimSpace(remote.OwnedBy), identity.Vendor)
	if created {
		model = domains.ModelCatalog{
			VendorCode: identity.Vendor, ProductCode: identity.Product, UpstreamProtocol: identity.Protocol,
			RemoteModelID: remote.ID, DisplayName: displayName, Description: strings.TrimSpace(remote.Description),
			OwnedBy: ownedBy, CapabilitiesJSON: string(remote.Capabilities), RawMetadataJSON: string(remote.Raw),
			Source: "official_remote", RemoteCreatedAt: remote.Created, FirstSeenAt: now, LastSeenAt: now,
		}
		return model, true, db.Create(&model).Error
	}
	err = db.Model(&model).Updates(map[string]any{
		"upstream_protocol": identity.Protocol, "display_name": displayName,
		"description": strings.TrimSpace(remote.Description), "owned_by": ownedBy,
		"capabilities_json": string(remote.Capabilities), "raw_metadata_json": string(remote.Raw),
		"remote_created_at": remote.Created, "last_seen_at": now, "deprecated": false,
	}).Error
	return model, false, err
}

func (s ModelService) ensureDefaultExposure(db *gorm.DB, model domains.ModelCatalog) error {
	var exposure domains.ModelExposure
	err := db.Where("model_catalog_guid = ?", model.Guid).First(&exposure).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var conflict int64
	if err := db.Model(&domains.ModelExposure{}).Where("public_model = ?", model.RemoteModelID).Count(&conflict).Error; err != nil {
		return err
	}
	if conflict > 0 {
		return nil
	}
	exposure = domains.ModelExposure{
		ModelCatalogGuid: model.Guid, PublicModel: model.RemoteModelID,
		TimeoutSec: 0, Enabled: true, Visible: true,
	}
	return db.Create(&exposure).Error
}

func (s ModelService) upsertAccountAvailability(db *gorm.DB, accountGuid, modelGuid string, remote catalog.RemoteModel) error {
	now := time.Now().UnixMilli()
	var relation domains.AccountModelAvailability
	err := db.Where("account_guid = ? AND model_catalog_guid = ?", accountGuid, modelGuid).First(&relation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		relation = domains.AccountModelAvailability{
			AccountGuid: accountGuid, ModelCatalogGuid: modelGuid, Available: true,
			FirstSeenAt: now, LastSeenAt: now, RawMetadataJSON: string(remote.Raw),
		}
		return db.Create(&relation).Error
	}
	if err != nil {
		return err
	}
	return db.Model(&relation).Updates(map[string]any{
		"available": true, "last_seen_at": now, "last_error": "", "raw_metadata_json": string(remote.Raw),
	}).Error
}

func (s ModelService) validateExposureNames(modelCatalogGuid, publicModel, aliasesJSON string) error {
	names := map[string]bool{publicModel: true}
	for _, alias := range parseAliases(aliasesJSON) {
		if names[alias] {
			return errors.New("model alias duplicates publicModel")
		}
		names[alias] = true
	}
	var exposures []domains.ModelExposure
	if err := global.NAV_DB.Where("model_catalog_guid <> ?", modelCatalogGuid).Find(&exposures).Error; err != nil {
		return err
	}
	for _, exposure := range exposures {
		if names[exposure.PublicModel] {
			return errors.New("public model or alias is already in use")
		}
		for _, alias := range parseAliases(exposure.Aliases) {
			if names[alias] {
				return errors.New("public model or alias is already in use")
			}
		}
	}
	return nil
}

func normalizeRemoteModels(values []catalog.RemoteModel) []catalog.RemoteModel {
	seen := map[string]bool{}
	out := make([]catalog.RemoteModel, 0, len(values))
	for _, value := range values {
		value.ID = strings.TrimSpace(value.ID)
		if value.ID == "" || seen[value.ID] {
			continue
		}
		seen[value.ID] = true
		out = append(out, value)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func modelAliasMatches(raw, model string) bool {
	for _, alias := range parseAliases(raw) {
		if alias == model {
			return true
		}
	}
	return false
}

func parseAliases(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var aliases []string
	if err := json.Unmarshal([]byte(raw), &aliases); err == nil {
		return normalizeAliases(aliases)
	}
	return normalizeAliases(strings.Split(raw, ","))
}

func normalizeAliasesJSON(raw string) (string, error) {
	aliases := parseAliases(raw)
	if len(aliases) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(aliases)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func normalizeAliases(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func normalizeModelAccountGroup(value string) string {
	return strings.TrimSpace(value)
}
