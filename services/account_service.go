package services

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"freeai/domains"
	fmgutils "freeai/utils"

	"github.com/wfu-work/nav-common-go-lib/global"
	"gorm.io/gorm"
)

type AccountService struct{}

var AccountServiceApp = AccountService{}

type CreateAccountInput struct {
	Name                  string `json:"name"`
	Email                 string `json:"email"`
	Provider              string `json:"provider"`
	AccountType           string `json:"accountType"`
	AuthType              string `json:"authType"`
	Secret                string `json:"secret"`
	SupportedModels       string `json:"supportedModels"`
	AccountGroup          string `json:"accountGroup"`
	Priority              int    `json:"priority"`
	Weight                int    `json:"weight"`
	SubscriptionExpiredAt int64  `json:"subscriptionExpiredAt"`
	Remark                string `json:"remark"`
}

type ReorderAccountInput struct {
	Items []ReorderAccountItem `json:"items"`
}

type ReorderAccountItem struct {
	Guid     string `json:"guid"`
	Priority int    `json:"priority"`
	Weight   int    `json:"weight"`
}

func (s AccountService) Create(input CreateAccountInput) (domains.Account, error) {
	if input.Name == "" {
		return domains.Account{}, errors.New("name is required")
	}
	if input.Provider == "" {
		return domains.Account{}, errors.New("provider is required")
	}
	if input.Secret == "" {
		return domains.Account{}, errors.New("secret is required")
	}
	if input.AuthType == "" {
		input.AuthType = domains.AuthTypeBearerToken
	}
	if input.Weight <= 0 {
		input.Weight = 1
	}
	fmgutils.SetSecretKeyFile(Config().SecretKeyFile)
	encrypted, err := fmgutils.EncryptSecret(input.Secret)
	if err != nil {
		return domains.Account{}, err
	}
	account := domains.Account{
		Name:                  input.Name,
		Email:                 input.Email,
		Provider:              input.Provider,
		AccountType:           input.AccountType,
		AuthType:              input.AuthType,
		EncryptedSecret:       encrypted,
		SecretHint:            fmgutils.SecretHint(input.Secret),
		SupportedModels:       input.SupportedModels,
		AccountGroup:          input.AccountGroup,
		Status:                domains.AccountStatusAvailable,
		Priority:              input.Priority,
		Weight:                input.Weight,
		Enabled:               true,
		SubscriptionExpiredAt: input.SubscriptionExpiredAt,
		Remark:                input.Remark,
	}
	err = global.NAV_DB.Create(&account).Error
	AuditServiceApp.Record("", "account.create", "account", account.Guid, map[string]string{"name": account.Name})
	return account, err
}

func (s AccountService) Update(guid string, input CreateAccountInput) (domains.Account, error) {
	var account domains.Account
	if err := global.NAV_DB.Where("guid = ?", guid).First(&account).Error; err != nil {
		return domains.Account{}, err
	}
	updates := map[string]any{
		"name":                    input.Name,
		"email":                   input.Email,
		"provider":                input.Provider,
		"account_type":            input.AccountType,
		"auth_type":               input.AuthType,
		"supported_models":        input.SupportedModels,
		"account_group":           input.AccountGroup,
		"priority":                input.Priority,
		"weight":                  input.Weight,
		"subscription_expired_at": input.SubscriptionExpiredAt,
		"remark":                  input.Remark,
	}
	if input.Secret != "" {
		fmgutils.SetSecretKeyFile(Config().SecretKeyFile)
		encrypted, err := fmgutils.EncryptSecret(input.Secret)
		if err != nil {
			return domains.Account{}, err
		}
		updates["encrypted_secret"] = encrypted
		updates["secret_hint"] = fmgutils.SecretHint(input.Secret)
	}
	if input.Weight <= 0 {
		updates["weight"] = 1
	}
	if err := global.NAV_DB.Model(&account).Updates(updates).Error; err != nil {
		return domains.Account{}, err
	}
	AuditServiceApp.Record("", "account.update", "account", guid, nil)
	return s.Get(guid)
}

func (s AccountService) Get(guid string) (domains.Account, error) {
	var account domains.Account
	err := global.NAV_DB.Where("guid = ?", guid).First(&account).Error
	return account, err
}

func (s AccountService) List(limit int) ([]domains.Account, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var list []domains.Account
	err := global.NAV_DB.Order("priority asc, id desc").Limit(limit).Find(&list).Error
	return list, err
}

func (s AccountService) Delete(guid string) error {
	err := global.NAV_DB.Where("guid = ?", guid).Delete(&domains.Account{}).Error
	AuditServiceApp.Record("", "account.delete", "account", guid, nil)
	return err
}

func (s AccountService) Refresh(guid string) (domains.Account, error) {
	now := time.Now().UnixMilli()
	updates := map[string]any{
		"last_refreshed_at": now,
	}
	var account domains.Account
	if err := global.NAV_DB.Where("guid = ?", guid).First(&account).Error; err != nil {
		return domains.Account{}, err
	}
	if account.Enabled && (account.Status == "" || account.Status == domains.AccountStatusUnknown || account.Status == domains.AccountStatusLimited || account.Status == domains.AccountStatusCooldown) {
		updates["status"] = domains.AccountStatusAvailable
		updates["cooldown_until"] = int64(0)
	}
	if err := global.NAV_DB.Model(&account).Updates(updates).Error; err != nil {
		return domains.Account{}, err
	}
	AuditServiceApp.Record("", "account.refresh", "account", guid, nil)
	return s.Get(guid)
}

func (s AccountService) Test(guid string) (map[string]any, error) {
	account, err := s.Get(guid)
	if err != nil {
		return nil, err
	}
	secret, err := s.DecryptSecret(account)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":          secret != "",
		"provider":    account.Provider,
		"status":      account.Status,
		"secretHint":  account.SecretHint,
		"enabled":     account.Enabled,
		"modelCount":  len(parseSupportedModels(account.SupportedModels)),
		"checkedAtMs": time.Now().UnixMilli(),
	}, nil
}

func (s AccountService) Reorder(input ReorderAccountInput) error {
	return global.NAV_DB.Transaction(func(tx *gorm.DB) error {
		for _, item := range input.Items {
			if item.Guid == "" {
				continue
			}
			updates := map[string]any{"priority": item.Priority}
			if item.Weight > 0 {
				updates["weight"] = item.Weight
			}
			if err := tx.Model(&domains.Account{}).Where("guid = ?", item.Guid).Updates(updates).Error; err != nil {
				return err
			}
		}
		AuditServiceApp.Record("", "account.reorder", "account", "", map[string]int{"count": len(input.Items)})
		return nil
	})
}

func (s AccountService) SetEnabled(guid string, enabled bool) error {
	status := domains.AccountStatusDisabled
	if enabled {
		status = domains.AccountStatusAvailable
	}
	err := global.NAV_DB.Model(&domains.Account{}).Where("guid = ?", guid).Updates(map[string]any{
		"enabled": enabled,
		"status":  status,
	}).Error
	AuditServiceApp.Record("", "account.enabled", "account", guid, map[string]bool{"enabled": enabled})
	return err
}

func (s AccountService) MarkUsed(guid string) error {
	return global.NAV_DB.Model(&domains.Account{}).Where("guid = ?", guid).Updates(map[string]any{
		"last_used_at":  time.Now().UnixMilli(),
		"failure_count": 0,
		"status":        domains.AccountStatusAvailable,
	}).Error
}

func (s AccountService) MarkFailure(guid, errorType string) error {
	var account domains.Account
	if err := global.NAV_DB.Where("guid = ?", guid).First(&account).Error; err != nil {
		return err
	}
	status := account.Status
	cooldownUntil := account.CooldownUntil
	switch errorType {
	case domains.ErrorAuthFailed:
		status = domains.AccountStatusInvalid
	case domains.ErrorRateLimited:
		status = domains.AccountStatusLimited
		cooldownUntil = time.Now().Add(time.Duration(Config().CooldownSeconds) * time.Second).UnixMilli()
	case domains.ErrorQuotaExhausted:
		status = domains.AccountStatusExhausted
	case domains.ErrorUpstream5xx, domains.ErrorNetwork, domains.ErrorUpstreamTimeout:
		if account.FailureCount+1 >= 3 {
			status = domains.AccountStatusCooldown
			cooldownUntil = time.Now().Add(time.Duration(Config().CooldownSeconds) * time.Second).UnixMilli()
		}
	}
	return global.NAV_DB.Model(&account).Updates(map[string]any{
		"failure_count":  account.FailureCount + 1,
		"status":         status,
		"cooldown_until": cooldownUntil,
	}).Error
}

func (s AccountService) DecryptSecret(account domains.Account) (string, error) {
	fmgutils.SetSecretKeyFile(Config().SecretKeyFile)
	return fmgutils.DecryptSecret(account.EncryptedSecret)
}

func (s AccountService) FindAvailable(provider, accountGroup, model string, limit int) ([]domains.Account, error) {
	if limit <= 0 {
		limit = 100
	}
	now := time.Now().UnixMilli()
	query := global.NAV_DB.Where("enabled = ? AND provider = ? AND status NOT IN ?", true, provider, []string{
		domains.AccountStatusDisabled,
		domains.AccountStatusLimited,
		domains.AccountStatusCooldown,
		domains.AccountStatusExpired,
		domains.AccountStatusInvalid,
		domains.AccountStatusExhausted,
	})
	query = query.Where("(cooldown_until = 0 OR cooldown_until < ?)", now)
	query = query.Where("(subscription_expired_at = 0 OR subscription_expired_at > ?)", now)
	if accountGroup != "" {
		query = query.Where("account_group = ?", accountGroup)
	}
	var list []domains.Account
	err := query.Order("priority asc, last_used_at asc, id asc").Limit(limit).Find(&list).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return list, err
}

func parseSupportedModels(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*" {
		return nil
	}
	var models []string
	if err := json.Unmarshal([]byte(raw), &models); err == nil {
		return models
	}
	for _, part := range strings.Split(raw, ",") {
		if model := strings.TrimSpace(part); model != "" {
			models = append(models, model)
		}
	}
	return models
}
