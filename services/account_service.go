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

	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/free-ai-go/utils"
	"github.com/wfu-work/nav-common-go-lib/global"
	commonUtils "github.com/wfu-work/nav-common-go-lib/utils"
	"github.com/wfu-work/proxy-api-lib/compat/aiok"
	proxycodexzh "github.com/wfu-work/proxy-api-lib/compat/codexzh"
	proxyfreemodel "github.com/wfu-work/proxy-api-lib/compat/freemodel"
	proxytokeni "github.com/wfu-work/proxy-api-lib/compat/tokeni"
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
	Guid       string `json:"guid"`
	Provider   string `json:"provider"`
	APIBaseURL string `json:"apiBaseUrl"`
	AuthType   string `json:"authType"`
	Secret     string `json:"secret"`
}

const (
	openAIOAuthClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	openAIOAuthRedirectURI  = "http://localhost:1455/auth/callback"
	openAIOAuthTokenURL     = "https://auth.openai.com/oauth/token"
	openAIOAuthDefaultScope = "openid profile email offline_access api.connectors.read api.connectors.invoke"
	openAIOAuthAPIKeyToken  = "openai-api-key"
)

type LoginCallbackParseInput struct {
	Provider     string `json:"provider"`
	CallbackURL  string `json:"callbackUrl"`
	CodeVerifier string `json:"codeVerifier"`
	RedirectURI  string `json:"redirectUri"`
}

type LoginCallbackParseResult struct {
	Provider       string            `json:"provider"`
	AuthType       string            `json:"authType"`
	Secret         string            `json:"secret"`
	SecretHint     string            `json:"secretHint"`
	AccessToken    string            `json:"accessToken,omitempty"`
	APIKeyToken    string            `json:"apiKeyToken,omitempty"`
	Code           string            `json:"code,omitempty"`
	State          string            `json:"state,omitempty"`
	CodeVerifier   string            `json:"codeVerifier,omitempty"`
	RefreshToken   string            `json:"refreshToken,omitempty"`
	IDToken        string            `json:"idToken,omitempty"`
	TokenType      string            `json:"tokenType,omitempty"`
	ExpiresIn      string            `json:"expiresIn,omitempty"`
	Scope          string            `json:"scope,omitempty"`
	ExchangeError  string            `json:"exchangeError,omitempty"`
	APIKeyError    string            `json:"apiKeyError,omitempty"`
	HasAccessToken bool              `json:"hasAccessToken"`
	HasAPIKeyToken bool              `json:"hasApiKeyToken"`
	Params         map[string]string `json:"params"`
}

type openAIOAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    any    `json:"expires_in"`
	Scope        string `json:"scope"`
}

type openAIOAuthAPIKeyTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   any    `json:"expires_in"`
	Scope       string `json:"scope"`
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

type TokeniUsageStats struct {
	Balance float64 `json:"balance"`
}

type RefreshUsageResult struct {
	AccountGuid string                 `json:"accountGuid"`
	Provider    string                 `json:"provider"`
	UsageType   string                 `json:"usageType"`
	Quotas      []domains.AccountQuota `json:"quotas"`
	Raw         any                    `json:"raw"`
}

type AccountListItem struct {
	domains.Account
	Quotas []domains.AccountQuota `json:"quotas"`
}

type UsageRefreshSweepResult struct {
	Checked int `json:"checked"`
	Updated int `json:"updated"`
	Failed  int `json:"failed"`
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
	normalizeAccountProviderConfig(&input)
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
	input.AccountGroup = normalizeAccountGroupName(input.AccountGroup)
	utils.SetSecretKeyFile(Config().SecretKeyFile)
	encrypted, err := utils.EncryptSecret(input.Secret)
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
		SecretHint:            utils.SecretHint(input.Secret),
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
	if err == nil {
		AccountGroupServiceApp.RefreshSummaries(account.AccountGroup)
	}
	AuditServiceApp.Record("", "account.create", "account", account.Guid, map[string]string{"name": account.Name})
	return account, err
}

func (s AccountService) Update(guid string, input CreateAccountInput) (domains.Account, error) {
	var account domains.Account
	if err := global.NAV_DB.Where("guid = ?", guid).First(&account).Error; err != nil {
		return domains.Account{}, err
	}
	normalizeAccountProviderConfig(&input)
	if err := validateCustomProvider(input.Provider, input.APIBaseURL, input.SupplierName, input.OfficialURL); err != nil {
		return domains.Account{}, err
	}
	normalizeAccountUsageConfig(&input)
	input.AccountGroup = normalizeAccountGroupName(input.AccountGroup)
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
		utils.SetSecretKeyFile(Config().SecretKeyFile)
		encrypted, err := utils.EncryptSecret(input.Secret)
		if err != nil {
			return domains.Account{}, err
		}
		updates["encrypted_secret"] = encrypted
		updates["secret_hint"] = utils.SecretHint(input.Secret)
	}
	if input.Weight <= 0 {
		updates["weight"] = 1
	}
	if err := global.NAV_DB.Model(&account).Updates(updates).Error; err != nil {
		return domains.Account{}, err
	}
	AccountGroupServiceApp.RefreshSummaries(account.AccountGroup, input.AccountGroup)
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
	if err = db.Order("priority asc, id desc").Limit(limit).Offset(offset).Find(&results).Error; err != nil {
		return nil, 0, err
	}
	items := attachAccountQuotas(results)
	return items, total, nil
}

func (s AccountService) ListAll() ([]domains.Account, error) {
	var list []domains.Account
	err := global.NAV_DB.Order("priority asc, id desc").Find(&list).Error
	return list, err
}

func attachAccountQuotas(accounts []domains.Account) []AccountListItem {
	items := make([]AccountListItem, 0, len(accounts))
	if len(accounts) == 0 {
		return items
	}
	guids := make([]string, 0, len(accounts))
	for _, account := range accounts {
		guids = append(guids, account.Guid)
	}
	var quotas []domains.AccountQuota
	_ = global.NAV_DB.Where("account_guid IN ?", guids).Order("window_type asc, id asc").Find(&quotas).Error
	quotaByAccount := map[string][]domains.AccountQuota{}
	for _, quota := range quotas {
		quotaByAccount[quota.AccountGuid] = append(quotaByAccount[quota.AccountGuid], quota)
	}
	for _, account := range accounts {
		items = append(items, AccountListItem{
			Account: account,
			Quotas:  quotaByAccount[account.Guid],
		})
	}
	return items
}

func (s AccountService) DeleteByGuid(guid string) error {
	var account domains.Account
	_ = global.NAV_DB.Where("guid = ?", guid).First(&account).Error
	err := global.NAV_DB.Where("guid = ?", guid).Delete(&domains.Account{}).Error
	if err == nil && account.Guid != "" {
		AccountGroupServiceApp.RefreshSummaries(account.AccountGroup)
	}
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
	if supportsUsageQuery(account) {
		if _, err := s.RefreshUsage(guid); err != nil {
			_ = global.NAV_DB.Model(&account).Update("last_refreshed_at", now).Error
			return domains.Account{}, err
		}
		AccountGroupServiceApp.RefreshSummaries(account.AccountGroup)
		AuditServiceApp.Record("", "account.refresh", "account", guid, map[string]string{"mode": "usage"})
		return s.GetByGuid(guid)
	}
	_ = QuotaServiceApp.RefreshExpiredWindows(guid)
	blocked, err := QuotaServiceApp.HasBlockingQuota(guid)
	if err != nil {
		return domains.Account{}, err
	}
	if account.Enabled && blocked {
		updates["status"] = domains.AccountStatusExhausted
		updates["cooldown_until"] = int64(0)
	} else if account.Enabled && (account.Status == "" || account.Status == domains.AccountStatusUnknown || account.Status == domains.AccountStatusLimited || account.Status == domains.AccountStatusCooldown) {
		updates["status"] = domains.AccountStatusAvailable
		updates["cooldown_until"] = int64(0)
	}
	if err := global.NAV_DB.Model(&account).Updates(updates).Error; err != nil {
		return domains.Account{}, err
	}
	AccountGroupServiceApp.RefreshSummaries(account.AccountGroup)
	AuditServiceApp.Record("", "account.refresh", "account", guid, nil)
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
		"mode":        "basic",
		"message":     "Secret 解密成功，未填写模型，未发起上游请求",
	}
	if input.Model == "" {
		if firstModel := firstSupportedModel(account.SupportedModels); firstModel != "" {
			input.Model = firstModel
			result["mode"] = "upstream"
			result["message"] = "已使用账号支持的第一个模型发起上游测试"
		} else {
			return result, nil
		}
	} else {
		result["mode"] = "upstream"
		result["message"] = "已按指定模型发起上游测试"
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
	if model.Provider != "" && model.Provider != account.Provider {
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
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), Config().RequestTimeout())
	defer cancel()
	proxyResult, err := ProxyAPIClientApp.Do(ctx, ProxyProviderConfig{
		Name:    firstNonEmpty(model.Provider, account.Provider),
		BaseURL: accountBaseURL(account),
		WireAPI: "responses",
	}, ProxyCredential{Type: account.AuthType, Value: secret}, ProxyRequest{
		Endpoint: "/v1/responses",
		Model:    model.UpstreamModel,
		Body:     body,
	})
	if err != nil {
		errorType := classifyError(err)
		result["upstreamStatusCode"] = 0
		result["upstreamErrorType"] = errorType
		result["latencyMs"] = time.Since(startedAt).Milliseconds()
		result["ok"] = false
		result["message"] = "上游测试失败"
		QuotaServiceApp.ApplyQuotaError(account.Guid, errorType)
		if updated, markErr := s.MarkTestFailure(account.Guid, errorType); markErr == nil {
			result["status"] = updated.Status
		}
		return result, nil
	}
	result["upstreamStatusCode"] = proxyResult.StatusCode
	result["upstreamErrorType"] = proxyResult.ErrorType
	result["latencyMs"] = proxyResult.LatencyMs
	result["ok"] = proxyResult.StatusCode >= 200 && proxyResult.StatusCode < 300 && proxyResult.ErrorType == ""
	result["model"] = model.PublicModel
	result["upstreamModel"] = model.UpstreamModel
	if proxyResult.ErrorType != "" {
		result["message"] = "上游返回错误"
		QuotaServiceApp.ApplyQuotaError(account.Guid, proxyResult.ErrorType)
		if updated, markErr := s.MarkTestFailure(account.Guid, proxyResult.ErrorType); markErr == nil {
			result["status"] = updated.Status
		}
	} else {
		result["message"] = "上游测试通过"
		_ = s.MarkUsed(account.Guid)
		if updated, getErr := s.GetByGuid(account.Guid); getErr == nil {
			result["status"] = updated.Status
		}
	}
	return result, nil
}

func (s AccountService) FetchModels(input FetchAccountModelsInput) ([]string, error) {
	secret := strings.TrimSpace(input.Secret)
	var account domains.Account
	if secret == "" {
		guid := strings.TrimSpace(input.Guid)
		if guid == "" {
			return nil, errors.New("secret is required")
		}
		if err := global.NAV_DB.Where("guid = ?", guid).First(&account).Error; err != nil {
			return nil, err
		}
		decrypted, err := s.DecryptSecret(account)
		if err != nil {
			return nil, err
		}
		secret = strings.TrimSpace(decrypted)
		if secret == "" {
			return nil, errors.New("secret is required")
		}
	}
	authType := input.AuthType
	if authType == "" {
		authType = account.AuthType
		if authType == "" {
			authType = domains.AuthTypeBearerToken
		}
	}
	baseURL := strings.TrimSpace(input.APIBaseURL)
	if baseURL == "" {
		if account.Guid != "" {
			baseURL = accountBaseURL(account)
		}
		if baseURL == "" && strings.TrimSpace(input.Provider) == "custom" {
			return nil, errors.New("apiBaseUrl is required for custom provider")
		}
		if baseURL == "" {
			baseURL = providerDefaultAPIBaseURL(input.Provider)
		}
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
	client, err := UpstreamHTTPClient()
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
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

func (s AccountService) ParseLoginCallback(input LoginCallbackParseInput) (LoginCallbackParseResult, error) {
	rawURL := strings.TrimSpace(input.CallbackURL)
	if rawURL == "" {
		return LoginCallbackParseResult{}, errors.New("callbackUrl is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return LoginCallbackParseResult{}, err
	}
	params := map[string]string{}
	collectValues(params, parsed.Query())
	if parsed.Fragment != "" {
		fragmentValues, _ := url.ParseQuery(parsed.Fragment)
		collectValues(params, fragmentValues)
	}
	accessToken := firstNonEmpty(params["access_token"], params["token"], params["id_token"])
	code := params["code"]
	state := params["state"]
	if accessToken == "" && code == "" && state == "" {
		return LoginCallbackParseResult{}, errors.New("callback url does not contain access_token, code or state")
	}
	tokenType := params["token_type"]
	expiresIn := params["expires_in"]
	scope := firstNonEmpty(params["scope"], openAIOAuthDefaultScope)
	codeVerifier := strings.TrimSpace(input.CodeVerifier)
	refreshToken := ""
	idToken := ""
	apiKeyToken := ""
	exchangeError := ""
	apiKeyError := ""
	if accessToken == "" && code != "" {
		if codeVerifier == "" {
			exchangeError = "missing code_verifier"
		} else {
			tokenResp, err := exchangeOpenAIOAuthCode(code, codeVerifier, strings.TrimSpace(input.RedirectURI))
			if err != nil {
				exchangeError = err.Error()
			} else {
				accessToken = strings.TrimSpace(tokenResp.AccessToken)
				refreshToken = strings.TrimSpace(tokenResp.RefreshToken)
				idToken = strings.TrimSpace(tokenResp.IDToken)
				tokenType = firstNonEmpty(tokenResp.TokenType, tokenType)
				expiresIn = firstNonEmpty(tokenExpiresInString(tokenResp.ExpiresIn), expiresIn)
				scope = firstNonEmpty(tokenResp.Scope, scope)
			}
		}
	}
	if idToken != "" {
		apiKeyResp, err := exchangeOpenAIOAuthAPIKeyToken(idToken)
		if err != nil {
			apiKeyError = err.Error()
		} else {
			apiKeyToken = strings.TrimSpace(apiKeyResp.AccessToken)
		}
	}
	secretPayload := map[string]string{
		"provider":             strings.TrimSpace(input.Provider),
		"access_token":         accessToken,
		"api_key_access_token": apiKeyToken,
		"refresh_token":        refreshToken,
		"id_token":             idToken,
		"code":                 code,
		"state":                state,
		"code_verifier":        codeVerifier,
		"token_type":           tokenType,
		"expires_in":           expiresIn,
		"scope":                scope,
		"callback_url":         rawURL,
	}
	secretRaw, err := json.Marshal(secretPayload)
	if err != nil {
		return LoginCallbackParseResult{}, err
	}
	secret := string(secretRaw)
	hintSource := firstNonEmpty(accessToken, code, state, rawURL)
	return LoginCallbackParseResult{
		Provider:       strings.TrimSpace(input.Provider),
		AuthType:       domains.AuthTypeLoginCallback,
		Secret:         secret,
		SecretHint:     utils.SecretHint(hintSource),
		AccessToken:    accessToken,
		APIKeyToken:    apiKeyToken,
		Code:           code,
		State:          state,
		CodeVerifier:   codeVerifier,
		RefreshToken:   refreshToken,
		IDToken:        idToken,
		TokenType:      tokenType,
		ExpiresIn:      expiresIn,
		Scope:          scope,
		ExchangeError:  exchangeError,
		APIKeyError:    apiKeyError,
		HasAccessToken: accessToken != "",
		HasAPIKeyToken: apiKeyToken != "",
		Params:         params,
	}, nil
}

func exchangeOpenAIOAuthCode(code, codeVerifier, redirectURI string) (openAIOAuthTokenResponse, error) {
	redirectURI = strings.TrimSpace(redirectURI)
	if redirectURI == "" {
		redirectURI = openAIOAuthRedirectURI
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", openAIOAuthClientID)
	form.Set("code", strings.TrimSpace(code))
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", strings.TrimSpace(codeVerifier))

	ctx, cancel := context.WithTimeout(context.Background(), Config().RequestTimeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIOAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return openAIOAuthTokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client, err := UpstreamHTTPClient()
	if err != nil {
		return openAIOAuthTokenResponse{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return openAIOAuthTokenResponse{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return openAIOAuthTokenResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return openAIOAuthTokenResponse{}, fmt.Errorf("oauth token exchange failed: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tokenResp openAIOAuthTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return openAIOAuthTokenResponse{}, err
	}
	if strings.TrimSpace(tokenResp.AccessToken) == "" {
		return openAIOAuthTokenResponse{}, errors.New("oauth token exchange returned empty access_token")
	}
	return tokenResp, nil
}

func exchangeOpenAIOAuthAPIKeyToken(idToken string) (openAIOAuthAPIKeyTokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
	form.Set("client_id", openAIOAuthClientID)
	form.Set("requested_token", openAIOAuthAPIKeyToken)
	form.Set("subject_token", strings.TrimSpace(idToken))
	form.Set("subject_token_type", "urn:ietf:params:oauth:token-type:id_token")

	ctx, cancel := context.WithTimeout(context.Background(), Config().RequestTimeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIOAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return openAIOAuthAPIKeyTokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client, err := UpstreamHTTPClient()
	if err != nil {
		return openAIOAuthAPIKeyTokenResponse{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return openAIOAuthAPIKeyTokenResponse{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return openAIOAuthAPIKeyTokenResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return openAIOAuthAPIKeyTokenResponse{}, fmt.Errorf("oauth api key token exchange failed: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tokenResp openAIOAuthAPIKeyTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return openAIOAuthAPIKeyTokenResponse{}, err
	}
	if strings.TrimSpace(tokenResp.AccessToken) == "" {
		return openAIOAuthAPIKeyTokenResponse{}, errors.New("oauth api key token exchange returned empty access_token")
	}
	return tokenResp, nil
}

func tokenExpiresInString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func (s AccountService) RefreshUsage(guid string) (RefreshUsageResult, error) {
	account, err := s.Get(guid)
	if err != nil {
		return RefreshUsageResult{}, err
	}
	usageType := normalizeUsageQueryType(account.UsageQueryType)
	if usageType == "" && strings.EqualFold(account.Provider, "codexzh") {
		usageType = "codexzh"
	}
	if usageType == "" && strings.EqualFold(account.Provider, "freemodel") {
		usageType = "freemodel"
	}
	if usageType == "" && strings.EqualFold(account.Provider, "aiok") {
		usageType = "aiok"
	}
	if usageType == "" && strings.EqualFold(account.Provider, "tokeni") {
		usageType = "tokeni"
	}
	if usageType == "" && looksLikeCodexZHAccount(account) {
		usageType = "codexzh"
	}
	switch usageType {
	case "codexzh":
		return s.refreshCodexZHUsage(account)
	case "freemodel":
		return s.refreshFreeModelUsage(account)
	case "aiok":
		return s.refreshAiokUsage(account)
	case "tokeni":
		return s.refreshTokeniUsage(account)
	case "":
		return RefreshUsageResult{}, errors.New("usage query is not configured")
	default:
		return RefreshUsageResult{}, fmt.Errorf("unsupported usage query type %s", usageType)
	}
}

func (s AccountService) RefreshDueUsageAccounts() (UsageRefreshSweepResult, error) {
	now := time.Now().UnixMilli()
	var accounts []domains.Account
	if err := global.NAV_DB.Where("enabled = ?", true).Find(&accounts).Error; err != nil {
		return UsageRefreshSweepResult{}, err
	}
	result := UsageRefreshSweepResult{}
	failures := make([]string, 0)
	for _, account := range accounts {
		if !supportsUsageQuery(account) {
			continue
		}
		due, err := s.usageRefreshDue(account.Guid, now)
		if err != nil {
			result.Failed++
			failures = append(failures, fmt.Sprintf("%s: %v", account.Guid, err))
			continue
		}
		if !due {
			continue
		}
		result.Checked++
		if _, err := s.RefreshUsage(account.Guid); err != nil {
			result.Failed++
			failures = append(failures, fmt.Sprintf("%s: %v", account.Guid, err))
			continue
		}
		result.Updated++
	}
	if len(failures) > 0 {
		return result, fmt.Errorf("refresh usage failed for %d account(s): %s", len(failures), strings.Join(failures, "; "))
	}
	return result, nil
}

func (s AccountService) usageRefreshDue(accountGuid string, now int64) (bool, error) {
	var quotas []domains.AccountQuota
	if err := global.NAV_DB.Where("account_guid = ?", accountGuid).Find(&quotas).Error; err != nil {
		return false, err
	}
	if len(quotas) == 0 {
		return true, nil
	}
	for _, quota := range quotas {
		if quota.NextRefreshAt == 0 || quota.NextRefreshAt <= now {
			return true, nil
		}
	}
	return false, nil
}

func supportsUsageQuery(account domains.Account) bool {
	usageType := normalizeUsageQueryType(account.UsageQueryType)
	if usageType == "codexzh" || strings.EqualFold(account.Provider, "codexzh") {
		return true
	}
	if usageType == "freemodel" || strings.EqualFold(account.Provider, "freemodel") {
		return true
	}
	if usageType == "aiok" || strings.EqualFold(account.Provider, "aiok") {
		return true
	}
	if usageType == "tokeni" || strings.EqualFold(account.Provider, "tokeni") {
		return true
	}
	return usageType == "" && looksLikeCodexZHAccount(account)
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
		account.SubscriptionExpiredAt = endMs
	}
	if err := s.applyUsageRefreshStatus(account, quotas); err != nil {
		return RefreshUsageResult{}, err
	}
	return RefreshUsageResult{
		AccountGuid: account.Guid,
		Provider:    account.Provider,
		UsageType:   "codexzh",
		Quotas:      quotas,
		Raw:         stats,
	}, nil
}

func (s AccountService) refreshFreeModelUsage(account domains.Account) (RefreshUsageResult, error) {
	secret, err := s.DecryptSecret(account)
	if err != nil {
		return RefreshUsageResult{}, err
	}
	stats, err := s.fetchFreeModelUsage(account, secret)
	if err != nil {
		return RefreshUsageResult{}, err
	}
	quotas, err := s.upsertCodexZHQuotas(account, stats)
	if err != nil {
		return RefreshUsageResult{}, err
	}
	if endMs := parseUsageTime(stats.SubscriptionEnd); endMs > 0 {
		_ = global.NAV_DB.Model(&account).Update("subscription_expired_at", endMs).Error
		account.SubscriptionExpiredAt = endMs
	}
	if err := s.applyUsageRefreshStatus(account, quotas); err != nil {
		return RefreshUsageResult{}, err
	}
	return RefreshUsageResult{
		AccountGuid: account.Guid,
		Provider:    account.Provider,
		UsageType:   "freemodel",
		Quotas:      quotas,
		Raw:         stats,
	}, nil
}

func (s AccountService) refreshAiokUsage(account domains.Account) (RefreshUsageResult, error) {
	secret, err := s.DecryptSecret(account)
	if err != nil {
		return RefreshUsageResult{}, err
	}
	stats, err := s.fetchAiokUsage(account, secret)
	if err != nil {
		return RefreshUsageResult{}, err
	}
	quota, err := s.upsertAiokQuota(account, stats)
	if err != nil {
		return RefreshUsageResult{}, err
	}
	if err := s.applyUsageRefreshStatus(account, quota); err != nil {
		return RefreshUsageResult{}, err
	}
	return RefreshUsageResult{
		AccountGuid: account.Guid,
		Provider:    account.Provider,
		UsageType:   "aiok",
		Quotas:      quota,
		Raw:         stats,
	}, nil
}

func (s AccountService) refreshTokeniUsage(account domains.Account) (RefreshUsageResult, error) {
	secret, err := s.DecryptSecret(account)
	if err != nil {
		return RefreshUsageResult{}, err
	}
	stats, err := s.fetchTokeniUsage(account, secret)
	if err != nil {
		return RefreshUsageResult{}, err
	}
	quotas, err := s.upsertTokeniQuota(account, stats)
	if err != nil {
		return RefreshUsageResult{}, err
	}
	if err := s.applyUsageRefreshStatus(account, quotas); err != nil {
		return RefreshUsageResult{}, err
	}
	return RefreshUsageResult{
		AccountGuid: account.Guid,
		Provider:    account.Provider,
		UsageType:   "tokeni",
		Quotas:      quotas,
		Raw:         stats,
	}, nil
}

func (s AccountService) fetchTokeniUsage(account domains.Account, secret string) (TokeniUsageStats, error) {
	baseURL := strings.TrimSpace(account.UsageAPIURL)
	if baseURL == "" {
		baseURL = proxytokeni.UsageURL
	}
	httpClient, err := UpstreamHTTPClient()
	if err != nil {
		return TokeniUsageStats{}, err
	}
	client := proxytokeni.NewUsageClient(
		proxytokeni.WithUsageBaseURL(baseURL),
		proxytokeni.WithUsageHTTPClient(httpClient),
		proxytokeni.WithUsageTimeout(Config().RequestTimeout()),
	)
	stats, err := client.Fetch(context.Background(), secret)
	if err != nil {
		return TokeniUsageStats{}, err
	}
	return TokeniUsageStats{Balance: stats.Balance}, nil
}

func (s AccountService) fetchFreeModelUsage(account domains.Account, secret string) (CodexZHUsageStats, error) {
	baseURL := strings.TrimSpace(account.UsageAPIURL)
	if baseURL == "" {
		baseURL = "https://freemodel.dev/api/usage"
	}
	httpClient, err := UpstreamHTTPClient()
	if err != nil {
		return CodexZHUsageStats{}, err
	}
	client := proxycodexzh.NewUsageClient(
		proxycodexzh.WithUsageBaseURL(baseURL),
		proxycodexzh.WithUsageHTTPClient(httpClient),
		proxycodexzh.WithUsageTimeout(Config().RequestTimeout()),
	)
	stats, err := client.Fetch(context.Background(), secret)
	if err != nil {
		return CodexZHUsageStats{}, err
	}
	return CodexZHUsageStats(stats), nil
}

func (s AccountService) fetchCodexZHUsage(account domains.Account, secret string) (CodexZHUsageStats, error) {
	baseURL := strings.TrimSpace(account.UsageAPIURL)
	if baseURL == "" {
		baseURL = proxycodexzh.UsageURL
	}
	httpClient, err := UpstreamHTTPClient()
	if err != nil {
		return CodexZHUsageStats{}, err
	}
	client := proxycodexzh.NewUsageClient(
		proxycodexzh.WithUsageBaseURL(baseURL),
		proxycodexzh.WithUsageHTTPClient(httpClient),
		proxycodexzh.WithUsageTimeout(Config().RequestTimeout()),
	)
	stats, err := client.Fetch(context.Background(), secret)
	if err != nil {
		return CodexZHUsageStats{}, err
	}
	return CodexZHUsageStats(stats), nil
}

func (s AccountService) fetchAiokUsage(account domains.Account, secret string) (TokeniUsageStats, error) {
	baseURL := strings.TrimSpace(account.UsageAPIURL)
	if baseURL == "" {
		baseURL = aiok.UsageURL
	}
	httpClient, err := UpstreamHTTPClient()
	if err != nil {
		return TokeniUsageStats{}, err
	}
	client := aiok.NewUsageClient(
		aiok.WithUsageBaseURL(baseURL),
		aiok.WithUsageHTTPClient(httpClient),
		aiok.WithUsageTimeout(Config().RequestTimeout()),
	)
	stats, err := client.Fetch(context.Background(), secret)
	if err != nil {
		return TokeniUsageStats{}, err
	}
	return TokeniUsageStats{Balance: stats.Balance}, nil
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

func (s AccountService) upsertTokeniQuota(account domains.Account, stats TokeniUsageStats) ([]domains.AccountQuota, error) {
	now := time.Now().UnixMilli()
	status := domains.QuotaStatusAvailable
	if stats.Balance <= 0 {
		status = domains.QuotaStatusExhausted
	}
	extra, _ := json.Marshal(map[string]any{
		"balance": stats.Balance,
	})
	quota, err := QuotaServiceApp.Upsert(QuotaInput{
		AccountGuid:     account.Guid,
		WindowType:      "balance",
		Unit:            "usd",
		RemainingAmount: stats.Balance,
		TotalAmount:     stats.Balance,
		ResetAt:         0,
		LastSyncedAt:    now,
		Status:          status,
		Extra:           string(extra),
	})
	if err != nil {
		return nil, err
	}
	return []domains.AccountQuota{quota}, nil
}

func (s AccountService) upsertAiokQuota(account domains.Account, stats TokeniUsageStats) ([]domains.AccountQuota, error) {
	return s.upsertTokeniQuota(account, stats)
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
	var account domains.Account
	_ = global.NAV_DB.Where("guid = ?", guid).First(&account).Error
	status := domains.AccountStatusDisabled
	if enabled {
		status = domains.AccountStatusAvailable
	}
	err := global.NAV_DB.Model(&domains.Account{}).Where("guid = ?", guid).Updates(map[string]any{
		"enabled": enabled,
		"status":  status,
	}).Error
	if err == nil && account.Guid != "" {
		AccountGroupServiceApp.RefreshSummaries(account.AccountGroup)
	}
	AuditServiceApp.Record("", "account.enabled", "account", guid, map[string]bool{"enabled": enabled})
	return err
}

func (s AccountService) MarkUsed(guid string) error {
	var account domains.Account
	_ = global.NAV_DB.Where("guid = ?", guid).First(&account).Error
	status := domains.AccountStatusAvailable
	if blocked, err := QuotaServiceApp.HasBlockingQuota(guid); err == nil && blocked {
		status = domains.AccountStatusExhausted
	}
	err := global.NAV_DB.Model(&domains.Account{}).Where("guid = ?", guid).Updates(map[string]any{
		"last_used_at":  time.Now().UnixMilli(),
		"failure_count": 0,
		"status":        status,
	}).Error
	if err == nil && account.Guid != "" {
		AccountGroupServiceApp.RefreshSummaries(account.AccountGroup)
	}
	return err
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
	err := global.NAV_DB.Model(&account).Updates(map[string]any{
		"failure_count":  account.FailureCount + 1,
		"status":         status,
		"cooldown_until": cooldownUntil,
	}).Error
	if err == nil {
		AccountGroupServiceApp.RefreshSummaries(account.AccountGroup)
	}
	return err
}

func (s AccountService) MarkTestFailure(guid, errorType string) (domains.Account, error) {
	var account domains.Account
	if err := global.NAV_DB.Where("guid = ?", guid).First(&account).Error; err != nil {
		return domains.Account{}, err
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
		status = domains.AccountStatusCooldown
		cooldownUntil = time.Now().Add(time.Duration(Config().CooldownSeconds) * time.Second).UnixMilli()
	default:
		status = domains.AccountStatusUnknown
	}
	if err := global.NAV_DB.Model(&account).Updates(map[string]any{
		"failure_count":  account.FailureCount + 1,
		"status":         status,
		"cooldown_until": cooldownUntil,
	}).Error; err != nil {
		return domains.Account{}, err
	}
	AccountGroupServiceApp.RefreshSummaries(account.AccountGroup)
	return s.GetByGuid(guid)
}

func (s AccountService) MarkExpiredSubscriptions() error {
	now := time.Now().UnixMilli()
	return global.NAV_DB.Model(&domains.Account{}).
		Where("enabled = ? AND subscription_expired_at > 0 AND subscription_expired_at <= ?", true, now).
		Update("status", domains.AccountStatusExpired).Error
}

func (s AccountService) applyUsageRefreshStatus(account domains.Account, quotas []domains.AccountQuota) error {
	if account.Guid == "" {
		return nil
	}
	now := time.Now().UnixMilli()
	status := account.Status
	cooldownUntil := account.CooldownUntil
	failureCount := account.FailureCount
	switch {
	case !account.Enabled:
		status = domains.AccountStatusDisabled
	case account.SubscriptionExpiredAt > 0 && account.SubscriptionExpiredAt <= now:
		status = domains.AccountStatusExpired
	case hasBlockingQuotaSnapshot(quotas, now):
		status = domains.AccountStatusExhausted
		cooldownUntil = 0
	default:
		if status == "" || status == domains.AccountStatusUnknown || status == domains.AccountStatusLimited || status == domains.AccountStatusCooldown || status == domains.AccountStatusExhausted {
			status = domains.AccountStatusAvailable
			cooldownUntil = 0
			failureCount = 0
		}
	}
	if err := global.NAV_DB.Model(&account).Updates(map[string]any{
		"status":            status,
		"cooldown_until":    cooldownUntil,
		"failure_count":     failureCount,
		"last_refreshed_at": now,
	}).Error; err != nil {
		return err
	}
	AccountGroupServiceApp.RefreshSummaries(account.AccountGroup)
	return nil
}

func hasBlockingQuotaSnapshot(quotas []domains.AccountQuota, now int64) bool {
	for _, quota := range quotas {
		if quota.ResetAt > 0 && quota.ResetAt <= now {
			continue
		}
		if quota.Status == domains.QuotaStatusExhausted {
			return true
		}
		if quota.TotalAmount > 0 && (quota.RemainingAmount <= 0 || quota.UsedPercent >= QuotaExhaustedPercentThreshold) {
			return true
		}
		if quota.TotalTokens > 0 && (quota.RemainingTokens <= 0 || quota.UsedPercent >= QuotaExhaustedPercentThreshold) {
			return true
		}
	}
	return false
}

func (s AccountService) DecryptSecret(account domains.Account) (string, error) {
	utils.SetSecretKeyFile(Config().SecretKeyFile)
	return utils.DecryptSecret(account.EncryptedSecret)
}

func (s AccountService) FindAvailable(provider, accountGroup, model string, limit int) ([]domains.Account, error) {
	if limit <= 0 {
		limit = 100
	}
	now := time.Now().UnixMilli()
	query := global.NAV_DB.Where("enabled = ? AND status NOT IN ?", true, []string{
		domains.AccountStatusDisabled,
		domains.AccountStatusLimited,
		domains.AccountStatusCooldown,
		domains.AccountStatusExpired,
		domains.AccountStatusInvalid,
		domains.AccountStatusExhausted,
	})
	if strings.TrimSpace(provider) != "" {
		query = query.Where("provider = ?", strings.TrimSpace(provider))
	}
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
	if err != nil {
		return nil, err
	}
	available := make([]domains.Account, 0, len(list))
	for _, account := range list {
		blocked, err := QuotaServiceApp.HasBlockingQuota(account.Guid)
		if err != nil {
			return nil, err
		}
		if blocked {
			_ = global.NAV_DB.Model(&account).Updates(map[string]any{
				"status":         domains.AccountStatusExhausted,
				"cooldown_until": int64(0),
			}).Error
			continue
		}
		available = append(available, account)
	}
	return available, nil
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

func firstSupportedModel(raw string) string {
	models := parseSupportedModels(raw)
	if len(models) == 0 {
		return ""
	}
	return models[0]
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

func normalizeAccountProviderConfig(input *CreateAccountInput) {
	input.Provider = strings.TrimSpace(input.Provider)
	input.APIBaseURL = strings.TrimSpace(input.APIBaseURL)
	if input.Provider != "custom" && input.APIBaseURL == "" {
		input.APIBaseURL = providerDefaultAPIBaseURL(input.Provider)
	}
}

func providerDefaultAPIBaseURL(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "codexzh":
		return proxycodexzh.BaseURL
	case "freemodel":
		return proxyfreemodel.BaseURL
	case "aiok":
		return aiok.BaseURL
	case "tokeni":
		return proxytokeni.BaseURL
	default:
		return Config().DefaultUpstreamBaseURL
	}
}

func normalizeAccountUsageConfig(input *CreateAccountInput) {
	input.UsageQueryType = normalizeUsageQueryType(input.UsageQueryType)
	input.UsageAPIURL = strings.TrimSpace(input.UsageAPIURL)
	if input.UsageQueryType == "" && strings.EqualFold(strings.TrimSpace(input.Provider), "codexzh") {
		input.UsageQueryType = "codexzh"
	}
	if input.UsageQueryType == "" && strings.EqualFold(strings.TrimSpace(input.Provider), "freemodel") {
		input.UsageQueryType = "freemodel"
	}
	if input.UsageQueryType == "" && strings.EqualFold(strings.TrimSpace(input.Provider), "aiok") {
		input.UsageQueryType = "aiok"
	}
	if input.UsageQueryType == "" && strings.EqualFold(strings.TrimSpace(input.Provider), "tokeni") {
		input.UsageQueryType = "tokeni"
	}
	if input.UsageQueryType == "codexzh" && input.UsageAPIURL == "" {
		input.UsageAPIURL = proxycodexzh.UsageURL
	}
	if input.UsageQueryType == "freemodel" && input.UsageAPIURL == "" {
		input.UsageAPIURL = "https://freemodel.dev/api/usage"
	}
	if input.UsageQueryType == "aiok" && input.UsageAPIURL == "" {
		input.UsageAPIURL = aiok.UsageURL
	}
	if input.UsageQueryType == "tokeni" && input.UsageAPIURL == "" {
		input.UsageAPIURL = proxytokeni.UsageURL
	}
}

func normalizeUsageQueryType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func accountBaseURL(account domains.Account) string {
	if baseURL := strings.TrimSpace(account.APIBaseURL); baseURL != "" {
		return baseURL
	}
	return providerDefaultAPIBaseURL(account.Provider)
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

func collectValues(out map[string]string, values url.Values) {
	for key, item := range values {
		if len(item) > 0 && strings.TrimSpace(item[0]) != "" {
			out[key] = strings.TrimSpace(item[0])
		}
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func codexZHQuotaToUSD(value float64) float64 {
	return proxycodexzh.QuotaToUSD(value)
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
