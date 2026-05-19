package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"freeai/domains"
	fmgutils "freeai/utils"

	"github.com/wfu-work/nav-common-go-lib/global"
	commonUtils "github.com/wfu-work/nav-common-go-lib/utils"
	"gorm.io/gorm"
)

type AccountService struct{}

var AccountServiceApp = AccountService{}

type CreateAccountInput struct {
	Name                  string `json:"name"`
	Email                 string `json:"email"`
	Provider              string `json:"provider"`
	APIBaseURL            string `json:"apiBaseUrl"`
	SupplierName          string `json:"supplierName"`
	OfficialURL           string `json:"officialUrl"`
	UsageQueryType        string `json:"usageQueryType"`
	UsageAPIURL           string `json:"usageApiUrl"`
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

type AccountTestInput struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type FetchAccountModelsInput struct {
	Provider   string `json:"provider"`
	APIBaseURL string `json:"apiBaseUrl"`
	AuthType   string `json:"authType"`
	Secret     string `json:"secret"`
}

type CodexZHUsageStats struct {
	DailyQuota        float64 `json:"dailyQuota"`
	WeeklyQuota       float64 `json:"weeklyQuota"`
	TodayUsed         float64 `json:"todayUsed"`
	WeekUsed          float64 `json:"weekUsed"`
	TodayCalls        int64   `json:"todayCalls"`
	TotalCalls        int64   `json:"totalCalls"`
	RPM               int64   `json:"rpm"`
	TPM               int64   `json:"tpm"`
	SubscriptionStart string  `json:"subscriptionStart"`
	SubscriptionEnd   string  `json:"subscriptionEnd"`
}

type RefreshUsageResult struct {
	AccountGuid string                 `json:"accountGuid"`
	Provider    string                 `json:"provider"`
	UsageType   string                 `json:"usageType"`
	Quotas      []domains.AccountQuota `json:"quotas"`
	Raw         CodexZHUsageStats      `json:"raw"`
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
	if err := validateCustomProvider(input.Provider, input.APIBaseURL, input.SupplierName, input.OfficialURL); err != nil {
		return domains.Account{}, err
	}
	if input.AuthType == "" {
		input.AuthType = domains.AuthTypeBearerToken
	}
	normalizeAccountUsageConfig(&input)
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
		APIBaseURL:            strings.TrimSpace(input.APIBaseURL),
		SupplierName:          strings.TrimSpace(input.SupplierName),
		OfficialURL:           strings.TrimSpace(input.OfficialURL),
		UsageQueryType:        strings.TrimSpace(input.UsageQueryType),
		UsageAPIURL:           strings.TrimSpace(input.UsageAPIURL),
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
	if err := validateCustomProvider(input.Provider, input.APIBaseURL, input.SupplierName, input.OfficialURL); err != nil {
		return domains.Account{}, err
	}
	normalizeAccountUsageConfig(&input)
	updates := map[string]any{
		"name":                    input.Name,
		"email":                   input.Email,
		"provider":                input.Provider,
		"api_base_url":            strings.TrimSpace(input.APIBaseURL),
		"supplier_name":           strings.TrimSpace(input.SupplierName),
		"official_url":            strings.TrimSpace(input.OfficialURL),
		"usage_query_type":        strings.TrimSpace(input.UsageQueryType),
		"usage_api_url":           strings.TrimSpace(input.UsageAPIURL),
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

func (s AccountService) GetByGuid(guid string) (domains.Account, error) {
	var account domains.Account
	err := global.NAV_DB.Where("guid = ?", guid).First(&account).Error
	return account, err
}

func (s AccountService) Get(guid string) (domains.Account, error) {
	return s.GetByGuid(guid)
}

func (s AccountService) List(params map[string]string) (list interface{}, total int64, err error) {
	limit := commonUtils.Str2Int(params["size"])
	offset := limit * (commonUtils.Str2Int(params["page"]) - 1)
	var results []domains.Account
	db := global.NAV_DB.Model(new(domains.Account))
	if params["enabled"] != "" {
		db = db.Where("enabled = ?", params["enabled"])
	}
	if params["provider"] != "" {
		db = db.Where("provider = ?", params["provider"])
	}
	if params["accountGroup"] != "" {
		db = db.Where("account_group = ?", params["accountGroup"])
	}
	if params["status"] != "" {
		db = db.Where("status = ?", params["status"])
	}
	if params["content"] != "" {
		like := "%" + params["content"] + "%"
		db = db.Where("name LIKE ? OR email LIKE ? OR provider LIKE ? OR supplier_name LIKE ?", like, like, like, like)
	}
	if err = db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = db.Order("priority asc, id desc").Limit(limit).Offset(offset).Find(&results).Error
	return results, total, err
}

func (s AccountService) ListAll() ([]domains.Account, error) {
	var list []domains.Account
	err := global.NAV_DB.Order("priority asc, id desc").Find(&list).Error
	return list, err
}

func (s AccountService) DeleteByGuid(guid string) error {
	err := global.NAV_DB.Where("guid = ?", guid).Delete(&domains.Account{}).Error
	AuditServiceApp.Record("", "account.delete", "account", guid, nil)
	return err
}

func (s AccountService) Delete(guid string) error {
	return s.DeleteByGuid(guid)
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
	_ = QuotaServiceApp.RefreshExpiredWindows(guid)
	return s.GetByGuid(guid)
}

func (s AccountService) Test(guid string, input AccountTestInput) (map[string]any, error) {
	account, err := s.GetByGuid(guid)
	if err != nil {
		return nil, err
	}
	secret, err := s.DecryptSecret(account)
	if err != nil {
		return nil, err
	}
	result := map[string]any{
		"ok":          secret != "",
		"provider":    account.Provider,
		"status":      account.Status,
		"secretHint":  account.SecretHint,
		"enabled":     account.Enabled,
		"modelCount":  len(parseSupportedModels(account.SupportedModels)),
		"checkedAtMs": time.Now().UnixMilli(),
	}
	if input.Model == "" {
		return result, nil
	}
	model, err := ModelServiceApp.Find(input.Model)
	if err != nil {
		if err.Error() != domains.ErrorModelNotSupported {
			return nil, err
		}
		if !supportsModel(account.SupportedModels, input.Model) {
			return nil, err
		}
		model = domains.ModelMapping{
			PublicModel:   input.Model,
			UpstreamModel: input.Model,
			Provider:      account.Provider,
			AccountGroup:  account.AccountGroup,
			Stream:        true,
			TimeoutSec:    int(Config().RequestTimeoutSeconds),
		}
	}
	if model.Provider != account.Provider {
		return nil, errors.New("model provider does not match account provider")
	}
	prompt := input.Prompt
	if prompt == "" {
		prompt = "ping"
	}
	body, err := json.Marshal(map[string]any{
		"model": model.PublicModel,
		"input": prompt,
		"store": false,
	})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), Config().RequestTimeout())
	defer cancel()
	proxyResult, err := ProxyAPIClientApp.Do(ctx, ProxyProviderConfig{
		Name:    model.Provider,
		BaseURL: accountBaseURL(account),
		WireAPI: "responses",
	}, ProxyCredential{Type: account.AuthType, Value: secret}, ProxyRequest{
		Endpoint: "/v1/responses",
		Model:    model.UpstreamModel,
		Body:     body,
	})
	if err != nil {
		return nil, err
	}
	result["upstreamStatusCode"] = proxyResult.StatusCode
	result["upstreamErrorType"] = proxyResult.ErrorType
	result["latencyMs"] = proxyResult.LatencyMs
	result["ok"] = proxyResult.StatusCode >= 200 && proxyResult.StatusCode < 300 && proxyResult.ErrorType == ""
	if proxyResult.ErrorType != "" {
		QuotaServiceApp.ApplyError(account.Guid, proxyResult.ErrorType)
	} else {
		_ = s.MarkUsed(account.Guid)
	}
	return result, nil
}

func (s AccountService) FetchModels(input FetchAccountModelsInput) ([]string, error) {
	secret := strings.TrimSpace(input.Secret)
	if secret == "" {
		return nil, errors.New("secret is required")
	}
	authType := input.AuthType
	if authType == "" {
		authType = domains.AuthTypeBearerToken
	}
	baseURL := strings.TrimSpace(input.APIBaseURL)
	if baseURL == "" {
		if strings.TrimSpace(input.Provider) == "custom" {
			return nil, errors.New("apiBaseUrl is required for custom provider")
		}
		baseURL = Config().DefaultUpstreamBaseURL
	}
	target := strings.TrimRight(baseURL, "/") + normalizeEndpoint("/v1/models", baseURL)
	ctx, cancel := context.WithTimeout(context.Background(), Config().RequestTimeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	authHeader, err := proxyCredential(ProxyCredential{Type: authType, Value: secret}).AuthorizationHeader(ctx)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", authHeader)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch models failed: upstream returned %d", resp.StatusCode)
	}
	models := parseModelListResponse(body)
	if len(models) == 0 {
		return nil, errors.New("no models found in upstream response")
	}
	sort.Strings(models)
	return models, nil
}

func (s AccountService) RefreshUsage(guid string) (RefreshUsageResult, error) {
	account, err := s.Get(guid)
	if err != nil {
		return RefreshUsageResult{}, err
	}
	usageType := strings.TrimSpace(account.UsageQueryType)
	if usageType == "" && strings.EqualFold(account.Provider, "codexzh") {
		usageType = "codexzh"
	}
	if usageType == "" && looksLikeCodexZHAccount(account) {
		usageType = "codexzh"
	}
	switch usageType {
	case "codexzh":
		return s.refreshCodexZHUsage(account)
	case "":
		return RefreshUsageResult{}, errors.New("usage query is not configured")
	default:
		return RefreshUsageResult{}, fmt.Errorf("unsupported usage query type %s", usageType)
	}
}

func looksLikeCodexZHAccount(account domains.Account) bool {
	values := []string{account.SupplierName, account.OfficialURL, account.APIBaseURL, account.UsageAPIURL}
	for _, value := range values {
		if strings.Contains(strings.ToLower(strings.TrimSpace(value)), "codexzh") {
			return true
		}
	}
	return false
}

func (s AccountService) refreshCodexZHUsage(account domains.Account) (RefreshUsageResult, error) {
	secret, err := s.DecryptSecret(account)
	if err != nil {
		return RefreshUsageResult{}, err
	}
	stats, err := s.fetchCodexZHUsage(account, secret)
	if err != nil {
		return RefreshUsageResult{}, err
	}
	quotas, err := s.upsertCodexZHQuotas(account, stats)
	if err != nil {
		return RefreshUsageResult{}, err
	}
	if endMs := parseUsageTime(stats.SubscriptionEnd); endMs > 0 {
		_ = global.NAV_DB.Model(&account).Update("subscription_expired_at", endMs).Error
	}
	return RefreshUsageResult{
		AccountGuid: account.Guid,
		Provider:    account.Provider,
		UsageType:   "codexzh",
		Quotas:      quotas,
		Raw:         stats,
	}, nil
}

func (s AccountService) fetchCodexZHUsage(account domains.Account, secret string) (CodexZHUsageStats, error) {
	baseURL := strings.TrimSpace(account.UsageAPIURL)
	if baseURL == "" {
		baseURL = "https://codexzh.com/api/v1/usage/stats"
	}
	reqURL := appendQueryParam(baseURL, "key", secret)
	ctx, cancel := context.WithTimeout(context.Background(), Config().RequestTimeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return CodexZHUsageStats{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return CodexZHUsageStats{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return CodexZHUsageStats{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CodexZHUsageStats{}, fmt.Errorf("fetch usage failed: upstream returned %d", resp.StatusCode)
	}
	stats, err := parseCodexZHUsageResponse(body)
	if err != nil {
		return CodexZHUsageStats{}, err
	}
	return stats, nil
}

func (s AccountService) upsertCodexZHQuotas(account domains.Account, stats CodexZHUsageStats) ([]domains.AccountQuota, error) {
	now := time.Now().UnixMilli()
	extra, _ := json.Marshal(map[string]any{
		"todayCalls":        stats.TodayCalls,
		"totalCalls":        stats.TotalCalls,
		"rpm":               stats.RPM,
		"tpm":               stats.TPM,
		"subscriptionStart": stats.SubscriptionStart,
		"subscriptionEnd":   stats.SubscriptionEnd,
	})
	dailyTotal := codexZHQuotaToUSD(stats.DailyQuota)
	weeklyTotal := codexZHQuotaToUSD(stats.WeeklyQuota)
	inputs := []QuotaInput{
		{
			AccountGuid:     account.Guid,
			WindowType:      "daily",
			Unit:            "usd",
			UsedAmount:      stats.TodayUsed,
			RemainingAmount: dailyTotal - stats.TodayUsed,
			TotalAmount:     dailyTotal,
			ResetAt:         defaultQuotaResetAt("daily", now),
			LastSyncedAt:    now,
			Extra:           string(extra),
		},
		{
			AccountGuid:     account.Guid,
			WindowType:      "weekly",
			Unit:            "usd",
			UsedAmount:      stats.WeekUsed,
			RemainingAmount: weeklyTotal - stats.WeekUsed,
			TotalAmount:     weeklyTotal,
			ResetAt:         parseUsageTime(stats.SubscriptionEnd),
			LastSyncedAt:    now,
			Extra:           string(extra),
		},
	}
	quotas := make([]domains.AccountQuota, 0, len(inputs))
	for _, input := range inputs {
		quota, err := QuotaServiceApp.Upsert(input)
		if err != nil {
			return nil, err
		}
		quotas = append(quotas, quota)
	}
	return quotas, nil
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

func (s AccountService) MarkExpiredSubscriptions() error {
	now := time.Now().UnixMilli()
	return global.NAV_DB.Model(&domains.Account{}).
		Where("enabled = ? AND subscription_expired_at > 0 AND subscription_expired_at <= ?", true, now).
		Update("status", domains.AccountStatusExpired).Error
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

func validateCustomProvider(provider, apiBaseURL, supplierName, officialURL string) error {
	if strings.TrimSpace(provider) != "custom" {
		return nil
	}
	if strings.TrimSpace(apiBaseURL) == "" {
		return errors.New("apiBaseUrl is required for custom provider")
	}
	if strings.TrimSpace(supplierName) == "" {
		return errors.New("supplierName is required for custom provider")
	}
	if strings.TrimSpace(officialURL) == "" {
		return errors.New("officialUrl is required for custom provider")
	}
	return nil
}

func normalizeAccountUsageConfig(input *CreateAccountInput) {
	input.UsageQueryType = strings.TrimSpace(input.UsageQueryType)
	input.UsageAPIURL = strings.TrimSpace(input.UsageAPIURL)
	if input.UsageQueryType == "" && strings.EqualFold(strings.TrimSpace(input.Provider), "codexzh") {
		input.UsageQueryType = "codexzh"
	}
	if input.UsageQueryType == "codexzh" && input.UsageAPIURL == "" {
		input.UsageAPIURL = "https://codexzh.com/api/v1/usage/stats"
	}
}

func accountBaseURL(account domains.Account) string {
	if baseURL := strings.TrimSpace(account.APIBaseURL); baseURL != "" {
		return baseURL
	}
	return Config().DefaultUpstreamBaseURL
}

func parseModelListResponse(body []byte) []string {
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []string `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	seen := map[string]bool{}
	models := make([]string, 0, len(payload.Data)+len(payload.Models))
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id != "" && !seen[id] {
			seen[id] = true
			models = append(models, id)
		}
	}
	for _, item := range payload.Models {
		id := strings.TrimSpace(item)
		if id != "" && !seen[id] {
			seen[id] = true
			models = append(models, id)
		}
	}
	return models
}

func appendQueryParam(rawURL, key, value string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		separator := "?"
		if strings.Contains(rawURL, "?") {
			separator = "&"
		}
		return rawURL + separator + url.QueryEscape(key) + "=" + url.QueryEscape(value)
	}
	query := parsed.Query()
	query.Set(key, value)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func parseCodexZHUsageResponse(body []byte) (CodexZHUsageStats, error) {
	var direct CodexZHUsageStats
	if err := json.Unmarshal(body, &direct); err == nil && (direct.DailyQuota > 0 || direct.WeeklyQuota > 0 || direct.TodayUsed > 0 || direct.WeekUsed > 0) {
		return direct, nil
	}
	var wrapped struct {
		Data CodexZHUsageStats `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return CodexZHUsageStats{}, err
	}
	if wrapped.Data.DailyQuota == 0 && wrapped.Data.WeeklyQuota == 0 && wrapped.Data.TodayUsed == 0 && wrapped.Data.WeekUsed == 0 {
		return CodexZHUsageStats{}, errors.New("invalid codexzh usage response")
	}
	return wrapped.Data, nil
}

func codexZHQuotaToUSD(value float64) float64 {
	return value / 500000
}

func parseUsageTime(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02",
		"2006/01/02 15:04:05",
		"2006/01/02",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}
