package services

import (
	"errors"
	"net/url"
	"strings"

	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/free-ai-go/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const proxySecretPrefix = "aesgcm:"

type UpstreamProxyCredentialsInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func decryptProxyURL(value, keyFile string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, proxySecretPrefix) {
		utils.SetSecretKeyFile(keyFile)
		plaintext, err := utils.DecryptSecret(value)
		if err != nil {
			return "", errors.New("proxy credentials cannot be decrypted; restore the original master key or replace the credentials")
		}
		value = plaintext
	}
	if value != "" {
		if err := validateProxyURL(value); err != nil {
			return "", err
		}
	}
	return value, nil
}

func encryptProxyURL(value, keyFile string) (string, error) {
	if value == "" {
		return "", nil
	}
	if err := validateProxyURL(value); err != nil {
		return "", err
	}
	parsed, _ := url.Parse(value)
	if parsed.User == nil {
		return value, nil
	}
	utils.SetSecretKeyFile(keyFile)
	encrypted, err := utils.EncryptSecret(value)
	if err != nil {
		return "", errors.New("proxy credentials could not be encrypted; check the local master key")
	}
	return encrypted, nil
}

// proxyEndpoint never returns userinfo, including usernames used for proxy routing.
func proxyEndpoint(value string) (string, bool) {
	if value == "" {
		return "", false
	}
	parsed, err := url.Parse(value)
	if err != nil || validateProxyURL(value) != nil {
		return "", false
	}
	hasCredentials := parsed.User != nil
	parsed.User = nil
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = ""
	return parsed.String(), hasCredentials
}

// resolveProxyCredentials preserves authentication only for the same endpoint.
// Changing hosts requires new credentials or an explicit request to clear them.
func resolveProxyCredentials(value string, credentials *UpstreamProxyCredentialsInput, clear bool, current GatewayConfig) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if credentials != nil {
			return "", errors.New("proxy address is required when supplying credentials")
		}
		return "", nil
	}
	if err := validateProxyURL(value); err != nil {
		return "", err
	}
	parsed, _ := url.Parse(value)
	if clear && (credentials != nil || parsed.User != nil) || credentials != nil && parsed.User != nil {
		return "", errors.New("supply proxy credentials once, or clear them explicitly")
	}
	if credentials != nil {
		if credentials.Username == "" {
			return "", errors.New("proxy username is required when replacing credentials")
		}
		parsed.User = url.UserPassword(credentials.Username, credentials.Password)
		return parsed.String(), nil
	}
	if clear || parsed.User != nil {
		return parsed.String(), nil
	}
	previous, err := decryptProxyURL(current.UpstreamProxyURL, current.SecretKeyFile)
	if err != nil {
		return "", err
	}
	previousEndpoint, hasCredentials := proxyEndpoint(previous)
	endpoint, _ := proxyEndpoint(value)
	if !hasCredentials {
		return value, nil
	}
	if endpoint != previousEndpoint {
		return "", errors.New("changing the proxy endpoint requires new credentials or explicitly clearing saved authentication")
	}
	previousURL, _ := url.Parse(previous)
	parsed.User = previousURL.User
	return parsed.String(), nil
}

// Migrate legacy DB/YAML values before publishing a snapshot. The conditional
// write cannot overwrite a concurrent config update. SQL logging is disabled
// here because the legacy comparison value may contain plaintext credentials.
func protectProxySnapshot(db *gorm.DB, values map[string]domains.SystemConfig) error {
	cfg := baseGatewayConfig()
	if item, ok := values[systemConfigSecretKeyFile]; ok {
		cfg.SecretKeyFile = item.ConfigValue
	}
	item, exists := values[systemConfigUpstreamProxyURL]
	value := cfg.UpstreamProxyURL
	if exists {
		value = strings.TrimSpace(item.ConfigValue)
	}
	if value == "" || strings.HasPrefix(value, proxySecretPrefix) {
		return nil
	}
	_, hasCredentials := proxyEndpoint(value)
	if !hasCredentials {
		return nil
	}
	encrypted, err := encryptProxyURL(value, cfg.SecretKeyFile)
	if err != nil {
		return err
	}
	secretDB := db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	if exists {
		updated := secretDB.Model(&domains.SystemConfig{}).Where("config_key = ? AND config_value = ?", systemConfigUpstreamProxyURL, item.ConfigValue).Updates(map[string]any{"config_value": encrypted, "value_type": "secret"})
		if updated.Error != nil || updated.RowsAffected != 1 {
			return errors.New("proxy credentials could not be migrated; retry after checking configuration storage")
		}
	} else {
		item = domains.SystemConfig{ConfigKey: systemConfigUpstreamProxyURL, Group: systemConfigGroupGateway, Remark: "上游代理地址"}
		item.ConfigValue, item.ValueType = encrypted, "secret"
		if err := secretDB.Create(&item).Error; err != nil {
			return errors.New("proxy credentials could not be migrated; retry after checking configuration storage")
		}
	}
	item.ConfigValue, item.ValueType = encrypted, "secret"
	values[systemConfigUpstreamProxyURL] = item
	return nil
}
