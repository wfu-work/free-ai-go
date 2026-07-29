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
	proxyapi "github.com/wfu-work/proxy-api-lib"
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
	if input.Secret == "" {
		return domains.Account{}, errors.New("secret is required")
	}
	normalizeAccountProviderConfig(&input)
	if err := validateOfficialAccountProvider(input.Provider, input.APIBaseURL); err != nil {
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
	if err := validateOfficialAccountProvider(input.Provider, input.APIBaseURL); err != nil {
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
	if !strings.EqualFold(strings.TrimSpace(account.Provider), "openai") {
		return nil, fmt.Errorf("account provider %q is no longer supported", account.Provider)
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
		Name: "openai",
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
	if provider := strings.ToLower(strings.TrimSpace(input.Provider)); provider != "" && provider != "openai" {
		return nil, fmt.Errorf("unsupported provider %q: only official OpenAI accounts are enabled", input.Provider)
	}
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
		if !strings.EqualFold(strings.TrimSpace(account.Provider), "openai") {
			return nil, fmt.Errorf("account provider %q is no longer supported", account.Provider)
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
	ctx, cancel := context.WithTimeout(context.Background(), Config().RequestTimeout())
	defer cancel()
	client, err := newProxyClient(ProxyCredential{Type: authType, Value: secret})
	if err != nil {
		return nil, err
	}
	resp, err := client.Models.List(ctx)
	if err != nil {
		return nil, err
	}
	models := make([]string, 0, len(resp.Data))
	for _, model := range resp.Data {
		if id := strings.TrimSpace(model.ID); id != "" {
			models = append(models, id)
		}
	}
	if len(models) == 0 {
		return nil, errors.New("no models found in upstream response")
	}
	sort.Strings(models)
	return models, nil
}

func (s AccountService) ParseLoginCallback(input LoginCallbackParseInput) (LoginCallbackParseResult, error) {
	provider := strings.ToLower(strings.TrimSpace(input.Provider))
	if provider == "" {
		provider = "openai"
	}
	if provider != "openai" {
		return LoginCallbackParseResult{}, fmt.Errorf("unsupported provider %q: only official OpenAI accounts are enabled", input.Provider)
	}
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
		"provider":             provider,
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
		Provider:       provider,
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
	return RefreshUsageResult{
		AccountGuid: account.Guid,
		Provider:    "openai",
	}, errors.New("official OpenAI balance query is not supported; usage is tracked locally")
}

func (s AccountService) RefreshDueUsageAccounts() (UsageRefreshSweepResult, error) {
	return UsageRefreshSweepResult{}, nil
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

func validateOfficialAccountProvider(provider, apiBaseURL string) error {
	if strings.ToLower(strings.TrimSpace(provider)) != "openai" {
		return fmt.Errorf("unsupported provider %q: only official OpenAI accounts are enabled", provider)
	}
	if strings.TrimRight(strings.TrimSpace(apiBaseURL), "/") != proxyapi.DefaultBaseURL {
		return fmt.Errorf("unsupported OpenAI base URL %q: only %s is enabled", apiBaseURL, proxyapi.DefaultBaseURL)
	}
	return nil
}

func normalizeAccountProviderConfig(input *CreateAccountInput) {
	provider := strings.ToLower(strings.TrimSpace(input.Provider))
	if provider == "" {
		provider = "openai"
	}
	input.Provider = provider
	input.APIBaseURL = proxyapi.DefaultBaseURL
	input.SupplierName = "OpenAI"
	input.OfficialURL = "https://openai.com"
}

func normalizeAccountUsageConfig(input *CreateAccountInput) {
	input.UsageQueryType = ""
	input.UsageAPIURL = ""
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
