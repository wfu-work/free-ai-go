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
	RateLimitPerMinute int    `json:"rateLimitPerMinute"`
	Remark             string `json:"remark"`
}

type CreatePlatformKeyOutput struct {
	Key    string              `json:"key"`
	Entity domains.PlatformKey `json:"entity"`
}

func (s PlatformKeyService) Create(input CreatePlatformKeyInput) (CreatePlatformKeyOutput, error) {
	if input.Name == "" {
		return CreatePlatformKeyOutput{}, errors.New("name is required")
	}
	raw, err := fmgutils.RandomHex(24)
	if err != nil {
		return CreatePlatformKeyOutput{}, err
	}
	key := "fmg_" + raw
	entity := domains.PlatformKey{
		Name:               input.Name,
		KeyHash:            fmgutils.SHA256Hex(key),
		KeyPrefix:          key[:10],
		AllowedModels:      input.AllowedModels,
		RateLimitPerMinute: input.RateLimitPerMinute,
		Enabled:            true,
		Remark:             input.Remark,
	}
	err = global.NAV_DB.Create(&entity).Error
	AuditServiceApp.Record("", "platform_key.create", "platform_key", entity.Guid, map[string]string{"name": entity.Name})
	return CreatePlatformKeyOutput{Key: key, Entity: entity}, err
}

func (s PlatformKeyService) List(limit int) ([]domains.PlatformKey, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var list []domains.PlatformKey
	err := global.NAV_DB.Order("id desc").Limit(limit).Find(&list).Error
	return list, err
}

func (s PlatformKeyService) Get(guid string) (domains.PlatformKey, error) {
	var key domains.PlatformKey
	err := global.NAV_DB.Where("guid = ?", guid).First(&key).Error
	return key, err
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
		"rate_limit_per_minute": input.RateLimitPerMinute,
		"remark":                input.Remark,
	}).Error; err != nil {
		return domains.PlatformKey{}, err
	}
	AuditServiceApp.Record("", "platform_key.update", "platform_key", guid, map[string]string{"name": input.Name})
	return s.Get(guid)
}

func (s PlatformKeyService) Delete(guid string) error {
	err := global.NAV_DB.Where("guid = ?", guid).Delete(&domains.PlatformKey{}).Error
	AuditServiceApp.Record("", "platform_key.delete", "platform_key", guid, nil)
	return err
}

func (s PlatformKeyService) SetEnabled(guid string, enabled bool) error {
	err := global.NAV_DB.Model(&domains.PlatformKey{}).Where("guid = ?", guid).Update("enabled", enabled).Error
	AuditServiceApp.Record("", "platform_key.enabled", "platform_key", guid, map[string]bool{"enabled": enabled})
	return err
}

func (s PlatformKeyService) Verify(header string) (domains.PlatformKey, error) {
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if token == "" || token == header {
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
	_ = global.NAV_DB.Model(&domains.PlatformKey{}).Where("guid = ?", key.Guid).Update("last_used_at", time.Now().UnixMilli()).Error
	return key, nil
}

func (s PlatformKeyService) VerifyForModel(header, model string) (domains.PlatformKey, error) {
	key, err := s.Verify(header)
	if err != nil {
		return domains.PlatformKey{}, err
	}
	if model != "" && !s.ModelAllowed(key, model) {
		return domains.PlatformKey{}, errors.New(domains.ErrorModelNotSupported)
	}
	return key, nil
}

func (s PlatformKeyService) ModelAllowed(key domains.PlatformKey, model string) bool {
	raw := strings.TrimSpace(key.AllowedModels)
	if raw == "" || raw == "*" {
		return true
	}
	var models []string
	if err := json.Unmarshal([]byte(raw), &models); err == nil {
		for _, allowed := range models {
			if strings.TrimSpace(allowed) == model || strings.TrimSpace(allowed) == "*" {
				return true
			}
		}
		return false
	}
	for _, allowed := range strings.Split(raw, ",") {
		allowed = strings.TrimSpace(allowed)
		if allowed == model || allowed == "*" {
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
