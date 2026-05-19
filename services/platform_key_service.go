package services

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"freeai/domains"
	fmgutils "freeai/utils"

	"github.com/wfu-work/nav-common-go-lib/global"
	"github.com/wfu-work/nav-common-go-lib/utils"
	"gorm.io/gorm"
)

type PlatformKeyService struct{}

var PlatformKeyServiceApp = PlatformKeyService{}

var platformKeyLimiter = struct {
	sync.Mutex
	windows map[string]rateWindow
}{
	windows: map[string]rateWindow{},
}

type rateWindow struct {
	StartedAt int64
	Count     int
}

type CreatePlatformKeyInput struct {
	Name               string `json:"name"`
	AllowedModels      string `json:"allowedModels"`
	RoutingStrategy    string `json:"routingStrategy"`
	AccountGroupFilter string `json:"accountGroupFilter"`
	TotalTokenLimit    int64  `json:"totalTokenLimit"`
	TokenLimitUnit     string `json:"tokenLimitUnit"`
	ProtocolType       string `json:"protocolType"`
	BoundModel         string `json:"boundModel"`
	ReasoningEffort    string `json:"reasoningEffort"`
	ServiceTier        string `json:"serviceTier"`
	RateLimitPerMinute int    `json:"rateLimitPerMinute"`
	Remark             string `json:"remark"`
}

type CreatePlatformKeyOutput struct {
	Key    string              `json:"key"`
	Entity domains.PlatformKey `json:"entity"`
}

type PlatformKeyStatsOutput struct {
	TotalTokens int64   `json:"totalTokens"`
	TotalAmount float64 `json:"totalAmount"`
}

type platformKeyUsageAgg struct {
	PlatformKeyID string
	UsedTokens    int64
}

const platformKeyEstimatedCostPerMillionTokens = 0.6556

func (s PlatformKeyService) Create(input CreatePlatformKeyInput) (CreatePlatformKeyOutput, error) {
	if input.Name == "" {
		return CreatePlatformKeyOutput{}, errors.New("name is required")
	}
	raw, err := fmgutils.RandomHex(24)
	if err != nil {
		return CreatePlatformKeyOutput{}, err
	}
	key := "fmg_" + raw
	fmgutils.SetSecretKeyFile(Config().SecretKeyFile)
	encryptedKey, err := fmgutils.EncryptSecret(key)
	if err != nil {
		return CreatePlatformKeyOutput{}, err
	}
	entity := domains.PlatformKey{
		Name:               input.Name,
		KeyHash:            fmgutils.SHA256Hex(key),
		KeyPrefix:          key[:10],
		EncryptedKey:       encryptedKey,
		AllowedModels:      input.AllowedModels,
		RoutingStrategy:    normalizePlatformKeyRoutingStrategy(input.RoutingStrategy),
		AccountGroupFilter: normalizeAccountGroupName(input.AccountGroupFilter),
		TotalTokenLimit:    input.TotalTokenLimit,
		TokenLimitUnit:     normalizeTokenLimitUnit(input.TokenLimitUnit),
		ProtocolType:       normalizeProtocolType(input.ProtocolType),
		BoundModel:         strings.TrimSpace(input.BoundModel),
		ReasoningEffort:    strings.TrimSpace(input.ReasoningEffort),
		ServiceTier:        strings.TrimSpace(input.ServiceTier),
		RateLimitPerMinute: input.RateLimitPerMinute,
		Enabled:            true,
		Remark:             input.Remark,
	}
	err = global.NAV_DB.Create(&entity).Error
	entity.Key = key
	AuditServiceApp.Record("", "platform_key.create", "platform_key", entity.Guid, map[string]string{"name": entity.Name})
	return CreatePlatformKeyOutput{Key: key, Entity: entity}, err
}

func (s PlatformKeyService) List(params map[string]string) (list interface{}, total int64, err error) {
	limit := utils.Str2Int(params["size"])
	offset := utils.Str2Int(params["size"]) * (utils.Str2Int(params["page"]) - 1)
	var results []domains.PlatformKey
	db := global.NAV_DB
	if params["enabled"] != "" {
		db = db.Where("enabled = ?", params["enabled"])
	}
	if params["content"] != "" {
		db = db.Where("name LIKE ? OR key_prefix LIKE ? OR remark LIKE ?", "%"+params["content"]+"%", "%"+params["content"]+"%", "%"+params["content"]+"%")
	}
	db = db.Model(new(domains.PlatformKey))
	err = db.Count(&total).Error
	if err != nil {
		return
	}
	order := "id desc"
	err = db.Order(order).Limit(limit).Offset(offset).Find(&results).Error
	s.attachPlainKeys(results)
	s.attachUsageStats(results)
	return results, total, err
}

func (s PlatformKeyService) ListAll() (list []domains.PlatformKey, err error) {
	err = global.NAV_DB.Order("id desc").Find(&list).Error
	s.attachPlainKeys(list)
	s.attachUsageStats(list)
	return list, err
}

func (s PlatformKeyService) GetByGuid(guid string) (domains.PlatformKey, error) {
	var key domains.PlatformKey
	err := global.NAV_DB.Where("guid = ?", guid).First(&key).Error
	if err == nil {
		key.Key = s.DecryptKey(key)
		keys := []domains.PlatformKey{key}
		s.attachUsageStats(keys)
		key = keys[0]
	}
	return key, err
}

func (s PlatformKeyService) Get(guid string) (domains.PlatformKey, error) {
	return s.GetByGuid(guid)
}

func (s PlatformKeyService) Update(guid string, input CreatePlatformKeyInput) (domains.PlatformKey, error) {
	var key domains.PlatformKey
	if err := global.NAV_DB.Where("guid = ?", guid).First(&key).Error; err != nil {
		return domains.PlatformKey{}, err
	}
	if input.Name == "" {
		return domains.PlatformKey{}, errors.New("name is required")
	}
	if err := global.NAV_DB.Model(&key).Updates(map[string]any{
		"name":                  input.Name,
		"allowed_models":        input.AllowedModels,
		"routing_strategy":      normalizePlatformKeyRoutingStrategy(input.RoutingStrategy),
		"account_group_filter":  normalizeAccountGroupName(input.AccountGroupFilter),
		"total_token_limit":     input.TotalTokenLimit,
		"token_limit_unit":      normalizeTokenLimitUnit(input.TokenLimitUnit),
		"protocol_type":         normalizeProtocolType(input.ProtocolType),
		"bound_model":           strings.TrimSpace(input.BoundModel),
		"reasoning_effort":      strings.TrimSpace(input.ReasoningEffort),
		"service_tier":          strings.TrimSpace(input.ServiceTier),
		"rate_limit_per_minute": input.RateLimitPerMinute,
		"remark":                input.Remark,
	}).Error; err != nil {
		return domains.PlatformKey{}, err
	}
	AuditServiceApp.Record("", "platform_key.update", "platform_key", guid, map[string]string{"name": input.Name})
	return s.Get(guid)
}

func (s PlatformKeyService) DeleteByGuid(guid string) error {
	err := global.NAV_DB.Where("guid = ?", guid).Delete(&domains.PlatformKey{}).Error
	AuditServiceApp.Record("", "platform_key.delete", "platform_key", guid, nil)
	return err
}

func (s PlatformKeyService) Delete(guid string) error {
	return s.DeleteByGuid(guid)
}

func (s PlatformKeyService) SetEnabled(guid string, enabled bool) error {
	err := global.NAV_DB.Model(&domains.PlatformKey{}).Where("guid = ?", guid).Update("enabled", enabled).Error
	AuditServiceApp.Record("", "platform_key.enabled", "platform_key", guid, map[string]bool{"enabled": enabled})
	return err
}

func (s PlatformKeyService) Stats() (PlatformKeyStatsOutput, error) {
	var usedTokens int64
	err := global.NAV_DB.Model(&domains.RequestLog{}).
		Select("COALESCE(SUM(input_tokens + output_tokens), 0)").
		Scan(&usedTokens).Error
	if err != nil {
		return PlatformKeyStatsOutput{}, err
	}
	return PlatformKeyStatsOutput{
		TotalTokens: usedTokens,
		TotalAmount: estimatePlatformKeyAmount(usedTokens),
	}, nil
}

func (s PlatformKeyService) Verify(header string) (domains.PlatformKey, error) {
	token := strings.TrimSpace(header)
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		token = strings.TrimSpace(token[7:])
	}
	if token == "" {
		return domains.PlatformKey{}, errors.New(domains.ErrorPlatformKeyInvalid)
	}
	hash := fmgutils.SHA256Hex(token)
	var key domains.PlatformKey
	err := global.NAV_DB.Where("key_hash = ? AND enabled = ?", hash, true).First(&key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domains.PlatformKey{}, errors.New(domains.ErrorPlatformKeyInvalid)
	}
	if err != nil {
		return domains.PlatformKey{}, err
	}
	if !s.allowRequest(key) {
		return domains.PlatformKey{}, errors.New(domains.ErrorPlatformKeyLimited)
	}
	if !s.allowTotalTokens(key) {
		return domains.PlatformKey{}, errors.New(domains.ErrorPlatformKeyLimited)
	}
	_ = global.NAV_DB.Model(&domains.PlatformKey{}).Where("guid = ?", key.Guid).Update("last_used_at", time.Now().UnixMilli()).Error
	return key, nil
}

func (s PlatformKeyService) VerifyForModel(header, model string) (domains.PlatformKey, error) {
	key, err := s.Verify(header)
	if err != nil {
		return domains.PlatformKey{}, err
	}
	if model != "" {
		mapping, findErr := ModelServiceApp.Find(model)
		if findErr == nil {
			if !s.ModelMappingAllowed(key, mapping) {
				return domains.PlatformKey{}, errors.New(domains.ErrorModelNotSupported)
			}
			return key, nil
		}
		if !s.ModelAllowed(key, model) {
			return domains.PlatformKey{}, errors.New(domains.ErrorModelNotSupported)
		}
	}
	return key, nil
}

func (s PlatformKeyService) ModelAllowed(key domains.PlatformKey, model string) bool {
	return s.allowedByRules(key.AllowedModels, func(allowed string) bool {
		return allowed == model || allowed == "*"
	})
}

func (s PlatformKeyService) ModelMappingAllowed(key domains.PlatformKey, model domains.ModelMapping) bool {
	if key.AccountGroupFilter != "" && key.AccountGroupFilter != model.AccountGroup {
		return false
	}
	return s.allowedByRules(key.AllowedModels, func(allowed string) bool {
		switch {
		case allowed == "*":
			return true
		case allowed == model.PublicModel || allowed == model.UpstreamModel:
			return true
		case strings.TrimPrefix(allowed, "group:") != allowed:
			return strings.TrimPrefix(allowed, "group:") == model.AccountGroup
		case strings.TrimPrefix(allowed, "provider:") != allowed:
			return strings.TrimPrefix(allowed, "provider:") == model.Provider
		default:
			return false
		}
	})
}

func (s PlatformKeyService) EffectiveTokenLimit(key domains.PlatformKey) int64 {
	if key.TotalTokenLimit <= 0 {
		return 0
	}
	switch strings.ToLower(strings.TrimSpace(key.TokenLimitUnit)) {
	case "k":
		return key.TotalTokenLimit * 1000
	case "m":
		return key.TotalTokenLimit * 1000 * 1000
	default:
		return key.TotalTokenLimit
	}
}

func (s PlatformKeyService) allowTotalTokens(key domains.PlatformKey) bool {
	limit := s.EffectiveTokenLimit(key)
	if limit <= 0 {
		return true
	}
	var used int64
	err := global.NAV_DB.Model(&domains.RequestLog{}).
		Where("platform_key_id = ?", key.Guid).
		Select("COALESCE(SUM(input_tokens + output_tokens), 0)").
		Scan(&used).Error
	if err != nil {
		return true
	}
	return used < limit
}

func (s PlatformKeyService) allowedByRules(raw string, match func(string) bool) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*" {
		return true
	}
	var models []string
	if err := json.Unmarshal([]byte(raw), &models); err == nil {
		for _, allowed := range models {
			if match(strings.TrimSpace(allowed)) {
				return true
			}
		}
		return false
	}
	for _, allowed := range strings.Split(raw, ",") {
		allowed = strings.TrimSpace(allowed)
		if match(allowed) {
			return true
		}
	}
	return false
}

func (s PlatformKeyService) allowRequest(key domains.PlatformKey) bool {
	if key.RateLimitPerMinute <= 0 {
		return true
	}
	now := time.Now().Unix()
	windowStart := now - now%60
	platformKeyLimiter.Lock()
	defer platformKeyLimiter.Unlock()
	window := platformKeyLimiter.windows[key.Guid]
	if window.StartedAt != windowStart {
		window = rateWindow{StartedAt: windowStart}
	}
	if window.Count >= key.RateLimitPerMinute {
		platformKeyLimiter.windows[key.Guid] = window
		return false
	}
	window.Count++
	platformKeyLimiter.windows[key.Guid] = window
	return true
}

func (s PlatformKeyService) DecryptKey(key domains.PlatformKey) string {
	if strings.TrimSpace(key.EncryptedKey) == "" {
		return ""
	}
	fmgutils.SetSecretKeyFile(Config().SecretKeyFile)
	value, err := fmgutils.DecryptSecret(key.EncryptedKey)
	if err != nil {
		return ""
	}
	return value
}

func (s PlatformKeyService) attachPlainKeys(keys []domains.PlatformKey) {
	for i := range keys {
		keys[i].Key = s.DecryptKey(keys[i])
	}
}

func (s PlatformKeyService) attachUsageStats(keys []domains.PlatformKey) {
	if len(keys) == 0 {
		return
	}
	guids := make([]string, 0, len(keys))
	for _, key := range keys {
		guids = append(guids, key.Guid)
	}
	var rows []platformKeyUsageAgg
	err := global.NAV_DB.Model(&domains.RequestLog{}).
		Select("platform_key_id, COALESCE(SUM(input_tokens + output_tokens), 0) AS used_tokens").
		Where("platform_key_id IN ?", guids).
		Group("platform_key_id").
		Scan(&rows).Error
	if err != nil {
		return
	}
	stats := make(map[string]int64, len(rows))
	for _, row := range rows {
		stats[row.PlatformKeyID] = row.UsedTokens
	}
	for i := range keys {
		keys[i].UsedTokens = stats[keys[i].Guid]
		keys[i].UsedAmount = estimatePlatformKeyAmount(keys[i].UsedTokens)
	}
}

func estimatePlatformKeyAmount(tokens int64) float64 {
	if tokens <= 0 {
		return 0
	}
	return float64(tokens) / 1000000 * platformKeyEstimatedCostPerMillionTokens
}

func normalizePlatformKeyRoutingStrategy(value string) string {
	switch strings.TrimSpace(value) {
	case "account_round_robin", "api_round_robin", "mixed_round_robin":
		return strings.TrimSpace(value)
	default:
		return "account_round_robin"
	}
}

func normalizeTokenLimitUnit(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "k", "m":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func normalizeProtocolType(value string) string {
	switch strings.TrimSpace(value) {
	case "openai_compatible", "claude", "gemini":
		return strings.TrimSpace(value)
	default:
		return "openai_compatible"
	}
}
