package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/free-ai-go/utils"
	"github.com/wfu-work/nav-common-go-lib/global"
	commonutils "github.com/wfu-work/nav-common-go-lib/utils"
	"gorm.io/gorm"
)

type PlatformKeyService struct{}

var PlatformKeyServiceApp = PlatformKeyService{}

const platformKeySecretPrefix = "sk-"

var platformKeyLimiter = struct {
	sync.Mutex
	windows map[string]rateWindow
}{
	windows: map[string]rateWindow{},
}

var platformKeyLastUsed = struct {
	sync.Mutex
	writtenAt map[string]int64
}{
	writtenAt: map[string]int64{},
}

var platformKeyConcurrency = struct {
	sync.RWMutex
	inFlight map[string]int
}{
	inFlight: map[string]int{},
}

type rateWindow struct {
	StartedAt int64
	Count     int
}

type CreatePlatformKeyInput struct {
	Name               string `json:"name"`
	AllowedModels      string `json:"allowedModels"`
	AccountGroupFilter string `json:"accountGroupFilter"`
	TotalTokenLimit    int64  `json:"totalTokenLimit"`
	TokenLimitUnit     string `json:"tokenLimitUnit"`
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

type PlatformKeyConcurrencyOutput struct {
	Total                 int            `json:"total"`
	MaxConcurrentRequests int            `json:"maxConcurrentRequests"`
	ByKey                 map[string]int `json:"byKey"`
}

func (s PlatformKeyService) Create(input CreatePlatformKeyInput) (CreatePlatformKeyOutput, error) {
	if input.Name == "" {
		return CreatePlatformKeyOutput{}, errors.New("name is required")
	}
	key, err := generatePlatformKey()
	if err != nil {
		return CreatePlatformKeyOutput{}, err
	}
	utils.SetSecretKeyFile(Config().SecretKeyFile)
	encryptedKey, err := utils.EncryptSecret(key)
	if err != nil {
		return CreatePlatformKeyOutput{}, err
	}
	entity := domains.PlatformKey{
		Name:               input.Name,
		KeyHash:            utils.SHA256Hex(key),
		KeyPrefix:          key[:10],
		EncryptedKey:       encryptedKey,
		AllowedModels:      input.AllowedModels,
		AccountGroupFilter: normalizePlatformKeyAccountGroupFilter(input.AccountGroupFilter),
		TotalTokenLimit:    input.TotalTokenLimit,
		TokenLimitUnit:     normalizeTokenLimitUnit(input.TokenLimitUnit),
		BoundModel:         strings.TrimSpace(input.BoundModel),
		ReasoningEffort:    strings.TrimSpace(input.ReasoningEffort),
		ServiceTier:        strings.TrimSpace(input.ServiceTier),
		RateLimitPerMinute: input.RateLimitPerMinute,
		Enabled:            true,
		Remark:             input.Remark,
	}
	err = global.NAV_DB.Create(&entity).Error
	AuditServiceApp.Record("", "platform_key.create", "platform_key", entity.Guid, map[string]string{"name": entity.Name})
	return CreatePlatformKeyOutput{Key: key, Entity: entity}, err
}

func generatePlatformKey() (string, error) {
	raw, err := utils.RandomHex(24)
	if err != nil {
		return "", err
	}
	return platformKeySecretPrefix + raw, nil
}

func (s PlatformKeyService) List(params map[string]string) (list interface{}, total int64, err error) {
	limit := commonutils.Str2Int(params["size"])
	offset := commonutils.Str2Int(params["size"]) * (commonutils.Str2Int(params["page"]) - 1)
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
	s.attachUsageStats(results)
	return results, total, err
}

func (s PlatformKeyService) ListAll() (list []domains.PlatformKey, err error) {
	err = global.NAV_DB.Order("id desc").Find(&list).Error
	s.attachUsageStats(list)
	return list, err
}

func (s PlatformKeyService) GetByGuid(guid string) (domains.PlatformKey, error) {
	var key domains.PlatformKey
	err := global.NAV_DB.Where("guid = ?", guid).First(&key).Error
	if err == nil {
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
		"account_group_filter":  normalizePlatformKeyAccountGroupFilter(input.AccountGroupFilter),
		"total_token_limit":     input.TotalTokenLimit,
		"token_limit_unit":      normalizeTokenLimitUnit(input.TokenLimitUnit),
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
	var usedCostMicrousd int64
	err := global.NAV_DB.Model(&domains.PlatformKey{}).
		Select("COALESCE(SUM(used_tokens), 0)").
		Scan(&usedTokens).Error
	if err != nil {
		return PlatformKeyStatsOutput{}, err
	}
	if err := global.NAV_DB.Model(&domains.PlatformKey{}).
		Select("COALESCE(SUM(used_cost_microusd), 0)").
		Scan(&usedCostMicrousd).Error; err != nil {
		return PlatformKeyStatsOutput{}, err
	}
	return PlatformKeyStatsOutput{
		TotalTokens: usedTokens,
		TotalAmount: microusdToUSD(usedCostMicrousd),
	}, nil
}

// TrackConcurrentRequest records one authenticated proxy request until its
// response finishes. The returned release function is safe to call repeatedly.
func (s PlatformKeyService) TrackConcurrentRequest(guid string) func() {
	guid = strings.TrimSpace(guid)
	if guid == "" {
		return func() {}
	}
	platformKeyConcurrency.Lock()
	platformKeyConcurrency.inFlight[guid]++
	platformKeyConcurrency.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			platformKeyConcurrency.Lock()
			if platformKeyConcurrency.inFlight[guid] <= 1 {
				delete(platformKeyConcurrency.inFlight, guid)
			} else {
				platformKeyConcurrency.inFlight[guid]--
			}
			platformKeyConcurrency.Unlock()
		})
	}
}

// ConcurrencyStats returns the current process's authenticated in-flight
// requests grouped by platform key.
func (s PlatformKeyService) ConcurrencyStats() PlatformKeyConcurrencyOutput {
	byKey := make(map[string]int)
	total := 0
	platformKeyConcurrency.RLock()
	for guid, count := range platformKeyConcurrency.inFlight {
		byKey[guid] = count
		total += count
	}
	platformKeyConcurrency.RUnlock()

	maxConcurrent := Config().MaxConcurrentRequests
	if maxConcurrent <= 0 {
		maxConcurrent = 128
	}
	return PlatformKeyConcurrencyOutput{
		Total:                 total,
		MaxConcurrentRequests: maxConcurrent,
		ByKey:                 byKey,
	}
}

func (s PlatformKeyService) Verify(header string) (domains.PlatformKey, error) {
	token := strings.TrimSpace(header)
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		token = strings.TrimSpace(token[7:])
	}
	if token == "" {
		return domains.PlatformKey{}, errors.New(domains.ErrorPlatformKeyInvalid)
	}
	hash := utils.SHA256Hex(token)
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
	s.touchLastUsed(key.Guid)
	return key, nil
}

func (s PlatformKeyService) VerifyForModel(header, model string) (domains.PlatformKey, error) {
	key, err := s.Verify(header)
	if err != nil {
		return domains.PlatformKey{}, err
	}
	if model != "" {
		routedModel, findErr := ModelServiceApp.Find(model)
		if findErr == nil {
			if !s.ModelExposureAllowed(key, routedModel) {
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

func (s PlatformKeyService) ModelExposureAllowed(key domains.PlatformKey, model RoutedModel) bool {
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
	if global.NAV_REDIS != nil {
		redisKey := fmt.Sprintf("freeai:platform-key:rate:%s:%d", key.Guid, windowStart)
		const script = `local current = redis.call('INCR', KEYS[1]); if current == 1 then redis.call('EXPIRE', KEYS[1], ARGV[1]); end; return current`
		count, err := global.NAV_REDIS.Eval(context.Background(), script, []string{redisKey}, 90).Int64()
		if err == nil {
			return count <= int64(key.RateLimitPerMinute)
		}
	}
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

// touchLastUsed 将高频请求对 last_used_at 的写入合并为每把密钥每分钟最多一次。
func (s PlatformKeyService) touchLastUsed(guid string) {
	if guid == "" {
		return
	}
	now := time.Now().UnixMilli()
	platformKeyLastUsed.Lock()
	if now-platformKeyLastUsed.writtenAt[guid] < time.Minute.Milliseconds() {
		platformKeyLastUsed.Unlock()
		return
	}
	platformKeyLastUsed.writtenAt[guid] = now
	platformKeyLastUsed.Unlock()
	_ = global.NAV_DB.Model(&domains.PlatformKey{}).Where("guid = ?", guid).Update("last_used_at", now).Error
}

func (s PlatformKeyService) DecryptKey(key domains.PlatformKey) string {
	if strings.TrimSpace(key.EncryptedKey) == "" {
		return ""
	}
	utils.SetSecretKeyFile(Config().SecretKeyFile)
	value, err := utils.DecryptSecret(key.EncryptedKey)
	if err != nil {
		return ""
	}
	return value
}

// GetSecretByGuid 仅供受信任的服务端流程读取密钥。调用方不得记录或持久化返回的明文。
func (s PlatformKeyService) GetSecretByGuid(guid string) (string, error) {
	var key domains.PlatformKey
	if err := global.NAV_DB.Where("guid = ?", strings.TrimSpace(guid)).First(&key).Error; err != nil {
		return "", err
	}
	secret := s.DecryptKey(key)
	if secret == "" {
		return "", errors.New("platform key secret is unavailable")
	}
	return secret, nil
}

func (s PlatformKeyService) attachUsageStats(keys []domains.PlatformKey) {
	for i := range keys {
		keys[i].UsedAmount = microusdToUSD(keys[i].UsedCostMicrousd)
	}
}

func microusdToUSD(value int64) float64 {
	return float64(value) / 1000000
}

func normalizePlatformKeyAccountGroupFilter(value string) string {
	return strings.TrimSpace(value)
}

func normalizeTokenLimitUnit(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "k", "m":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}
