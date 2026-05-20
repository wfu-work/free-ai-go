package services

import (
	"errors"
	"strconv"
	"strings"

	"freeai/domains"

	"github.com/wfu-work/nav-common-go-lib/global"
	"gorm.io/gorm"
)

type SystemConfigService struct{}

var SystemConfigServiceApp = SystemConfigService{}

const (
	systemConfigTypeString = "string"
	systemConfigTypeBool   = "bool"
)

func (s SystemConfigService) Get(key string) (domains.SystemConfig, bool) {
	key = strings.TrimSpace(key)
	if key == "" || !systemConfigTableReady() {
		return domains.SystemConfig{}, false
	}
	var item domains.SystemConfig
	err := global.NAV_DB.Where("config_key = ?", key).First(&item).Error
	if err != nil {
		return domains.SystemConfig{}, false
	}
	return item, true
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
	if !systemConfigTableReady() {
		return errors.New("system config table is not ready")
	}
	var item domains.SystemConfig
	err := global.NAV_DB.Where("config_key = ?", key).First(&item).Error
	if err == nil {
		return global.NAV_DB.Model(&item).Updates(map[string]any{
			"config_value": value,
			"value_type":   valueType,
			"group":        strings.TrimSpace(group),
			"remark":       strings.TrimSpace(remark),
		}).Error
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	item = domains.SystemConfig{
		ConfigKey:   key,
		ConfigValue: value,
		ValueType:   valueType,
		Group:       strings.TrimSpace(group),
		Remark:      strings.TrimSpace(remark),
	}
	return global.NAV_DB.Create(&item).Error
}

func systemConfigTableReady() bool {
	if global.NAV_DB == nil {
		return false
	}
	return global.NAV_DB.Migrator().HasTable(&domains.SystemConfig{})
}
