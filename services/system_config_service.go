package services

import (
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/nav-common-go-lib/global"
	"gorm.io/gorm"
)

type SystemConfigService struct{}

var SystemConfigServiceApp = SystemConfigService{}

var systemConfigCache = struct {
	sync.RWMutex
	db        *gorm.DB
	expiresAt time.Time
	values    map[string]domains.SystemConfig
}{values: map[string]domains.SystemConfig{}}

const (
	systemConfigTypeString = "string"
	systemConfigTypeBool   = "bool"
)

func (s SystemConfigService) Get(key string) (domains.SystemConfig, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return domains.SystemConfig{}, false
	}
	item, ok := s.snapshot()[key]
	return item, ok
}

// snapshot 返回短时缓存的完整运行配置，避免单个代理请求为每个配置项分别查询数据库。
func (s SystemConfigService) snapshot() map[string]domains.SystemConfig {
	values, _ := s.snapshotWithError()
	return values
}

// snapshotWithError retains the last complete snapshot when a refresh fails.
// Upstream requests must distinguish an unreadable config from an empty config.
func (s SystemConfigService) snapshotWithError() (map[string]domains.SystemConfig, error) {
	db := global.NAV_DB
	if db == nil {
		return nil, nil
	}
	now := time.Now()
	systemConfigCache.RLock()
	if systemConfigCache.db == db && now.Before(systemConfigCache.expiresAt) {
		values := systemConfigCache.values
		systemConfigCache.RUnlock()
		return values, nil
	}
	systemConfigCache.RUnlock()

	systemConfigCache.Lock()
	defer systemConfigCache.Unlock()
	if systemConfigCache.db == db && now.Before(systemConfigCache.expiresAt) {
		return systemConfigCache.values, nil
	}
	lastSnapshot := func(err error) (map[string]domains.SystemConfig, error) {
		if systemConfigCache.db == db {
			return systemConfigCache.values, err
		}
		return nil, err
	}
	if !db.Migrator().HasTable(&domains.SystemConfig{}) {
		return lastSnapshot(errors.New("system config table is unavailable"))
	}
	var items []domains.SystemConfig
	if err := db.Find(&items).Error; err != nil {
		return lastSnapshot(errors.New("system config could not be read"))
	}
	values := make(map[string]domains.SystemConfig, len(items))
	for _, item := range items {
		values[item.ConfigKey] = item
	}
	if err := protectProxySnapshot(db, values); err != nil {
		return lastSnapshot(err)
	}
	systemConfigCache.db = db
	systemConfigCache.values = values
	systemConfigCache.expiresAt = now.Add(2 * time.Second)
	return values, nil
}

func (s SystemConfigService) GetString(key, fallback string) string {
	item, ok := s.Get(key)
	if !ok {
		return fallback
	}
	return item.ConfigValue
}

func (s SystemConfigService) GetBool(key string, fallback bool) bool {
	item, ok := s.Get(key)
	if !ok {
		return fallback
	}
	value, err := strconv.ParseBool(strings.TrimSpace(item.ConfigValue))
	if err != nil {
		return fallback
	}
	return value
}

func (s SystemConfigService) GetInt(key string, fallback int) int {
	item, ok := s.Get(key)
	if !ok {
		return fallback
	}
	value, err := strconv.Atoi(strings.TrimSpace(item.ConfigValue))
	if err != nil {
		return fallback
	}
	return value
}

func (s SystemConfigService) GetInt64(key string, fallback int64) int64 {
	item, ok := s.Get(key)
	if !ok {
		return fallback
	}
	value, err := strconv.ParseInt(strings.TrimSpace(item.ConfigValue), 10, 64)
	if err != nil {
		return fallback
	}
	return value
}

func (s SystemConfigService) SetString(group, key, value, remark string) error {
	return s.set(group, key, value, systemConfigTypeString, remark)
}

func (s SystemConfigService) SetBool(group, key string, value bool, remark string) error {
	return s.set(group, key, strconv.FormatBool(value), systemConfigTypeBool, remark)
}

func (s SystemConfigService) SetInt(group, key string, value int, remark string) error {
	return s.set(group, key, strconv.Itoa(value), "int", remark)
}

func (s SystemConfigService) SetInt64(group, key string, value int64, remark string) error {
	return s.set(group, key, strconv.FormatInt(value, 10), "int64", remark)
}

func (s SystemConfigService) set(group, key, value, valueType, remark string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	return s.SetValues([]domains.SystemConfig{{
		ConfigKey: key, ConfigValue: value, ValueType: valueType,
		Group: strings.TrimSpace(group), Remark: strings.TrimSpace(remark),
	}})
}

// SetValues commits related settings together; readers never see half a proxy policy.
func (s SystemConfigService) SetValues(values []domains.SystemConfig) error {
	if !systemConfigTableReady() {
		return errors.New("system config table is not ready")
	}
	protected := append([]domains.SystemConfig(nil), values...)
	for index, value := range protected {
		if value.ConfigKey != systemConfigUpstreamProxyURL || strings.HasPrefix(value.ConfigValue, proxySecretPrefix) {
			continue
		}
		encrypted, err := encryptProxyURL(strings.TrimSpace(value.ConfigValue), Config().SecretKeyFile)
		if err != nil {
			return err
		}
		protected[index].ConfigValue = encrypted
		if strings.HasPrefix(encrypted, proxySecretPrefix) {
			protected[index].ValueType = "secret"
		}
	}
	err := global.NAV_DB.Transaction(func(tx *gorm.DB) error {
		for _, value := range protected {
			if err := setSystemConfigValue(tx, value); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		invalidateSystemConfigCache()
	}
	return err
}

func setSystemConfigValue(db *gorm.DB, value domains.SystemConfig) error {
	var item domains.SystemConfig
	err := db.Unscoped().Where("config_key = ?", value.ConfigKey).First(&item).Error
	if err == nil {
		return db.Unscoped().Model(&item).Updates(map[string]any{
			"config_value": value.ConfigValue,
			"value_type":   value.ValueType,
			"group":        value.Group,
			"remark":       value.Remark,
			"deleted_time": nil,
		}).Error
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return db.Create(&value).Error
}

func invalidateSystemConfigCache() {
	systemConfigCache.Lock()
	systemConfigCache.expiresAt = time.Time{}
	systemConfigCache.Unlock()
}

func systemConfigTableReady() bool {
	if global.NAV_DB == nil {
		return false
	}
	return global.NAV_DB.Migrator().HasTable(&domains.SystemConfig{})
}
