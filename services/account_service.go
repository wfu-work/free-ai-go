package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/free-ai-go/utils"
	"github.com/wfu-work/nav-common-go-lib/global"
	commonUtils "github.com/wfu-work/nav-common-go-lib/utils"
	"github.com/wfu-work/proxy-api-lib/catalog"
	"github.com/wfu-work/proxy-api-lib/chatgpt"
	"github.com/wfu-work/proxy-api-lib/codexauth"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

type AccountService struct{}

var AccountServiceApp = AccountService{}
var accountTokenRefreshGroup singleflight.Group
var accountModelSyncGroup singleflight.Group

const (
	tokenRefreshSkew = 2 * time.Minute
)

type ImportAccountInput struct {
	AccountFile  json.RawMessage `json:"accountFile"`
	VendorCode   string          `json:"vendorCode"`
	Name         string          `json:"name"`
	AccountGroup string          `json:"accountGroup"`
	Priority     int             `json:"priority"`
	Weight       int             `json:"weight"`
	Remark       string          `json:"remark"`
}

// AccountPoolInput 是所有官方账号添加方式共用的账号池配置。
type AccountPoolInput struct {
	VendorCode   string `json:"vendorCode"`
	Name         string `json:"name"`
	AccountGroup string `json:"accountGroup"`
	Priority     int    `json:"priority"`
	Weight       int    `json:"weight"`
	Remark       string `json:"remark"`
}

// ManualAccountInput 用于手动录入已取得的 OpenAI OAuth 凭据。
// Access Token 可省略，此时服务端会使用 Refresh Token 换取一组有效令牌。
type ManualAccountInput struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	IDToken      string `json:"idToken"`
	AccountID    string `json:"accountId"`
	AccountPoolInput
}

type UpdateAccountInput struct {
	Name         string `json:"name"`
	VendorCode   string `json:"vendorCode"`
	AccountGroup string `json:"accountGroup"`
	Priority     int    `json:"priority"`
	Weight       int    `json:"weight"`
	Remark       string `json:"remark"`
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
	Guid string `json:"guid"`
}

type RefreshUsageResult struct {
	AccountGuid string                 `json:"accountGuid"`
	UsageType   string                 `json:"usageType"`
	Quotas      []domains.AccountQuota `json:"quotas"`
	PlanType    string                 `json:"planType"`
	Raw         any                    `json:"raw,omitempty"`
}

type AccountListItem struct {
	domains.Account
	Quotas              []domains.AccountQuota `json:"quotas"`
	AvailableModelCount int64                  `json:"availableModelCount"`
}

type UsageRefreshSweepResult struct {
	Checked int `json:"checked"`
	Updated int `json:"updated"`
	Failed  int `json:"failed"`
}

// ModelSyncInput 指定同步单个账号或某一官方产品；全部为空时同步所有启用账号。
type ModelSyncInput struct {
	AccountGuid string `json:"accountGuid"`
	VendorCode  string `json:"vendorCode"`
	ProductCode string `json:"productCode"`
}

// ModelSyncSweepResult 汇总一个或多个账号的模型目录同步结果。
type ModelSyncSweepResult struct {
	Checked int              `json:"checked"`
	Updated int              `json:"updated"`
	Failed  int              `json:"failed"`
	Results []ModelSyncStats `json:"results"`
	Errors  []string         `json:"errors,omitempty"`
}

// Import 创建或更新一个规范 OAuth 账号文件。相同 ChatGPT Account ID 会更新原账号，而不是重复入池。
func (s AccountService) Import(input ImportAccountInput) (domains.Account, error) {
	raw, err := normalizeAccountFileJSON(input.AccountFile)
	if err != nil {
		return domains.Account{}, err
	}
	file, err := codexauth.ParseAccountFile(raw)
	if err != nil {
		return domains.Account{}, fmt.Errorf("invalid OAuth account file: %w", err)
	}
	return s.upsertOfficialAccount(file, AccountPoolInput{
		VendorCode: input.VendorCode, Name: input.Name, AccountGroup: input.AccountGroup,
		Priority: input.Priority, Weight: input.Weight, Remark: input.Remark,
	}, "account.import")
}

// AddManual 使用手动填写的 OAuth Token 创建或更新官方账号。
func (s AccountService) AddManual(ctx context.Context, input ManualAccountInput) (domains.Account, error) {
	pool, err := normalizeOpenAICodexPool(input.AccountPoolInput)
	if err != nil {
		return domains.Account{}, err
	}
	accessToken := strings.TrimSpace(input.AccessToken)
	refreshToken := strings.TrimSpace(input.RefreshToken)
	if accessToken == "" && refreshToken == "" {
		return domains.Account{}, errors.New("accessToken or refreshToken is required")
	}
	tokens := codexauth.TokenSet{
		AccessToken: accessToken, RefreshToken: refreshToken, IDToken: strings.TrimSpace(input.IDToken),
	}
	if accessToken == "" {
		refreshed, err := refreshManualTokens(ctx, refreshToken)
		if err != nil {
			return domains.Account{}, fmt.Errorf("refresh OAuth token: %w", err)
		}
		tokens = *refreshed
		tokens.RefreshToken = tokens.EffectiveRefreshToken(refreshToken)
	}
	file, err := codexauth.NewAccountFile(tokens, strings.TrimSpace(input.AccountID))
	if err != nil {
		return domains.Account{}, fmt.Errorf("invalid OAuth credentials: %w", err)
	}
	if file.NeedsRefresh(time.Now(), tokenRefreshSkew) {
		if refreshToken == "" {
			return domains.Account{}, errors.New("accessToken is expired or about to expire and refreshToken is required")
		}
		refreshed, refreshErr := refreshManualTokens(ctx, refreshToken)
		if refreshErr != nil {
			return domains.Account{}, fmt.Errorf("refresh OAuth token: %w", refreshErr)
		}
		if err := file.ApplyTokenSet(*refreshed); err != nil {
			return domains.Account{}, err
		}
	}
	return s.upsertOfficialAccount(file, pool, "account.manual")
}

func normalizeOpenAICodexPool(pool AccountPoolInput) (AccountPoolInput, error) {
	vendorCode, productCode, err := normalizeOfficialAccountIdentity(pool.VendorCode)
	if err != nil {
		return AccountPoolInput{}, err
	}
	if vendorCode != domains.VendorOpenAI || productCode != domains.ProductCodex {
		return AccountPoolInput{}, errors.New("current OAuth login only supports official OpenAI Codex accounts")
	}
	pool.VendorCode = vendorCode
	return pool, nil
}

func refreshManualTokens(ctx context.Context, refreshToken string) (*codexauth.TokenSet, error) {
	httpClient, err := UpstreamHTTPClient()
	if err != nil {
		return nil, err
	}
	return codexauth.NewOAuthClient(codexauth.WithHTTPClient(httpClient)).Refresh(ctx, refreshToken)
}

func (s AccountService) upsertOfficialAccount(file *codexauth.AccountFile, pool AccountPoolInput, auditAction string) (domains.Account, error) {
	if file == nil {
		return domains.Account{}, errors.New("OAuth account file is required")
	}
	if err := file.Normalize(); err != nil {
		return domains.Account{}, err
	}
	vendorCode, productCode, err := normalizeOfficialAccountIdentity(pool.VendorCode)
	if err != nil {
		return domains.Account{}, err
	}
	encoded, err := file.Marshal()
	if err != nil {
		return domains.Account{}, err
	}
	encrypted, err := encryptAccountFile(encoded)
	if err != nil {
		return domains.Account{}, err
	}
	metadata := accountMetadata(file)
	name := strings.TrimSpace(pool.Name)
	if name == "" {
		name = metadata.name
	}
	if pool.Weight <= 0 {
		pool.Weight = 1
	}
	group := normalizeAccountGroupName(pool.AccountGroup)
	var existing domains.Account
	findErr := global.NAV_DB.Where(
		"vendor_code = ? AND product_code = ? AND chat_gpt_account_id = ?",
		vendorCode, productCode, file.Tokens.AccountID,
	).First(&existing).Error
	if findErr == nil {
		updates := metadata.updates()
		updates["vendor_code"] = vendorCode
		updates["product_code"] = productCode
		updates["credential_type"] = domains.CredentialOAuth
		updates["name"] = name
		updates["encrypted_account_file"] = encrypted
		updates["credential_hint"] = accountCredentialHint(file.Tokens.AccountID)
		updates["account_group"] = group
		updates["priority"] = pool.Priority
		updates["weight"] = pool.Weight
		updates["remark"] = pool.Remark
		updates["enabled"] = true
		updates["status"] = domains.AccountStatusAvailable
		updates["last_error"] = ""
		if err := global.NAV_DB.Model(&existing).Updates(updates).Error; err != nil {
			return domains.Account{}, err
		}
		AccountGroupServiceApp.RefreshSummaries(existing.AccountGroup, group)
		AuditServiceApp.Record("", auditAction+".update", "account", existing.Guid, nil)
		return s.GetByGuid(existing.Guid)
	}
	if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return domains.Account{}, findErr
	}
	account := domains.Account{
		VendorCode: vendorCode, ProductCode: productCode, CredentialType: domains.CredentialOAuth,
		Name: name, Email: metadata.email, ChatGPTAccountID: file.Tokens.AccountID,
		WorkspaceID: file.Meta.WorkspaceID, EncryptedAccountFile: encrypted,
		CredentialHint: accountCredentialHint(file.Tokens.AccountID), PlanType: file.Meta.PlanType,
		SubscriptionPlan: file.Meta.SubscriptionPlan, SubscriptionExpiredAt: normalizeUnixMillis(file.Meta.SubscriptionExpiresAt),
		SubscriptionRenewsAt: normalizeUnixMillis(file.Meta.SubscriptionRenewsAt), SubscriptionWillRenew: file.Meta.SubscriptionWillRenew,
		AccessTokenExpiresAt: metadata.accessTokenExpiresAt, TokenStatus: metadata.tokenStatus,
		AccountGroup: group, Status: domains.AccountStatusAvailable,
		Priority: pool.Priority, Weight: pool.Weight, Enabled: true, Remark: pool.Remark,
	}
	if err := global.NAV_DB.Create(&account).Error; err != nil {
		return domains.Account{}, err
	}
	AccountGroupServiceApp.RefreshSummaries(group)
	AuditServiceApp.Record("", auditAction, "account", account.Guid, map[string]string{"accountId": account.ChatGPTAccountID})
	return account, nil
}

// SyncOfficialAccountAsync 在账号入池后异步同步额度、订阅和官方模型目录。
func (s AccountService) SyncOfficialAccountAsync(guid string) {
	go func() {
		if _, err := s.RefreshUsage(guid); err != nil {
			global.NAV_LOG.Warn("sync added account usage failed", zap.String("accountGuid", guid), zap.Error(err))
		}
		if _, err := s.FetchModels(FetchAccountModelsInput{Guid: guid}); err != nil {
			global.NAV_LOG.Warn("sync added account models failed", zap.String("accountGuid", guid), zap.Error(err))
		}
	}()
}

func (s AccountService) Update(guid string, input UpdateAccountInput) (domains.Account, error) {
	account, err := s.GetByGuid(guid)
	if err != nil {
		return domains.Account{}, err
	}
	requestedVendor := strings.TrimSpace(input.VendorCode)
	if requestedVendor == "" {
		requestedVendor = account.VendorCode
	}
	vendorCode, productCode, err := normalizeOfficialAccountIdentity(requestedVendor)
	if err != nil {
		return domains.Account{}, err
	}
	if input.Weight <= 0 {
		input.Weight = 1
	}
	input.AccountGroup = normalizeAccountGroupName(input.AccountGroup)
	if strings.TrimSpace(input.Name) == "" {
		input.Name = account.Name
	}
	if err := global.NAV_DB.Model(&account).Updates(map[string]any{
		"name":          input.Name,
		"vendor_code":   vendorCode,
		"product_code":  productCode,
		"account_group": input.AccountGroup, "priority": input.Priority,
		"weight": input.Weight, "remark": input.Remark,
	}).Error; err != nil {
		return domains.Account{}, err
	}
	AccountGroupServiceApp.RefreshSummaries(account.AccountGroup, input.AccountGroup)
	AuditServiceApp.Record("", "account.update", "account", guid, nil)
	return s.GetByGuid(guid)
}

func (s AccountService) Export(guid string) ([]byte, domains.Account, error) {
	account, err := s.GetByGuid(guid)
	if err != nil {
		return nil, domains.Account{}, err
	}
	file, err := s.LoadAccountFile(account)
	if err != nil {
		return nil, domains.Account{}, err
	}
	file.Meta.Label = firstNonEmpty(account.Email, account.Name, file.Meta.Label)
	file.Meta.ChatGPTAccountID = account.ChatGPTAccountID
	file.Meta.WorkspaceID = account.WorkspaceID
	file.Meta.PlanType = account.PlanType
	file.Meta.SubscriptionPlan = account.SubscriptionPlan
	file.Meta.SubscriptionExpiresAt = account.SubscriptionExpiredAt
	file.Meta.SubscriptionRenewsAt = account.SubscriptionRenewsAt
	file.Meta.SubscriptionWillRenew = account.SubscriptionWillRenew
	file.Meta.ExportedAt = time.Now().UnixMilli()
	encoded, err := file.Marshal()
	if err == nil {
		AuditServiceApp.Record("", "account.export", "account", guid, nil)
	}
	return encoded, account, err
}

func (s AccountService) GetByGuid(guid string) (domains.Account, error) {
	var account domains.Account
	err := global.NAV_DB.Where("guid = ?", guid).First(&account).Error
	return account, err
}

func (s AccountService) List(params map[string]string) (list interface{}, total int64, err error) {
	limit := commonUtils.Str2Int(params["size"])
	offset := limit * (commonUtils.Str2Int(params["page"]) - 1)
	var results []domains.Account
	db := global.NAV_DB.Model(new(domains.Account))
	if params["enabled"] != "" {
		db = db.Where("enabled = ?", params["enabled"])
	}
	if params["accountGroup"] != "" {
		db = db.Where("account_group = ?", params["accountGroup"])
	}
	if params["status"] != "" {
		db = db.Where("status = ?", params["status"])
	}
	if vendorCode := strings.TrimSpace(params["vendorCode"]); vendorCode != "" {
		db = db.Where("vendor_code = ?", strings.ToLower(vendorCode))
	}
	if params["content"] != "" {
		like := "%" + params["content"] + "%"
		db = db.Where("name LIKE ? OR email LIKE ? OR chat_gpt_account_id LIKE ? OR remark LIKE ?", like, like, like, like)
	}
	if err = db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err = db.Order("priority asc, id desc").Limit(limit).Offset(offset).Find(&results).Error; err != nil {
		return nil, 0, err
	}
	return attachAccountQuotas(results), total, nil
}

func normalizeOfficialAccountIdentity(vendorCode string) (string, string, error) {
	switch strings.ToLower(strings.TrimSpace(vendorCode)) {
	case "", domains.VendorOpenAI:
		return domains.VendorOpenAI, domains.ProductCodex, nil
	case domains.VendorGoogle:
		return domains.VendorGoogle, domains.ProductGemini, nil
	case domains.VendorAnthropic:
		return domains.VendorAnthropic, domains.ProductClaudeCode, nil
	default:
		return "", "", fmt.Errorf("unsupported official account type: %s", vendorCode)
	}
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
	_ = global.NAV_DB.Where("account_guid IN ?", guids).Order("window_type asc").Find(&quotas).Error
	byAccount := map[string][]domains.AccountQuota{}
	for _, quota := range quotas {
		byAccount[quota.AccountGuid] = append(byAccount[quota.AccountGuid], quota)
	}
	type modelCountRow struct {
		AccountGuid string
		Count       int64
	}
	var modelCounts []modelCountRow
	_ = global.NAV_DB.Model(&domains.AccountModelAvailability{}).
		Select("account_guid, COUNT(*) AS count").
		Where("account_guid IN ? AND available = ?", guids, true).
		Group("account_guid").Scan(&modelCounts).Error
	countByAccount := map[string]int64{}
	for _, count := range modelCounts {
		countByAccount[count.AccountGuid] = count.Count
	}
	for _, account := range accounts {
		items = append(items, AccountListItem{
			Account: account, Quotas: byAccount[account.Guid], AvailableModelCount: countByAccount[account.Guid],
		})
	}
	return items
}

func (s AccountService) DeleteByGuid(guid string) error {
	account, _ := s.GetByGuid(guid)
	err := global.NAV_DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("account_guid = ?", guid).Delete(&domains.AccountQuota{}).Error; err != nil {
			return err
		}
		if err := tx.Where("account_guid = ?", guid).Delete(&domains.AccountModelAvailability{}).Error; err != nil {
			return err
		}
		return tx.Where("guid = ?", guid).Delete(&domains.Account{}).Error
	})
	if err == nil {
		AccountGroupServiceApp.RefreshSummaries(account.AccountGroup)
	}
	AuditServiceApp.Record("", "account.delete", "account", guid, nil)
	return err
}

func (s AccountService) FetchModels(input FetchAccountModelsInput) ([]string, error) {
	result, err := s.syncAccountModels(strings.TrimSpace(input.Guid))
	return result.Models, err
}

func (s AccountService) syncAccountModels(accountGuid string) (ModelSyncStats, error) {
	value, err, _ := accountModelSyncGroup.Do(accountGuid, func() (any, error) {
		return s.syncAccountModelsOnce(accountGuid)
	})
	if err != nil {
		return ModelSyncStats{}, err
	}
	return value.(ModelSyncStats), nil
}

func (s AccountService) syncAccountModelsOnce(accountGuid string) (ModelSyncStats, error) {
	account, err := s.GetByGuid(accountGuid)
	if err != nil {
		return ModelSyncStats{}, err
	}
	ctx, cancel := contextWithOptionalTimeout(context.Background(), Config().RequestTimeout())
	defer cancel()
	file, err := s.ActiveAccountFile(ctx, account, false)
	if err != nil {
		return ModelSyncStats{}, err
	}
	identity, models, err := s.fetchModelsWithFile(ctx, account, file)
	if isChatGPTUnauthorized(err) {
		if file, refreshErr := s.ActiveAccountFile(ctx, account, true); refreshErr == nil {
			identity, models, err = s.fetchModelsWithFile(ctx, account, file)
		}
	}
	if err != nil {
		ModelServiceApp.RecordAccountSyncFailure(account.Guid, err)
		return ModelSyncStats{}, err
	}
	result, err := ModelServiceApp.SyncRemoteModels(account, identity, models)
	if err != nil {
		ModelServiceApp.RecordAccountSyncFailure(account.Guid, err)
		return ModelSyncStats{}, err
	}
	return result, nil
}

func (s AccountService) fetchModelsWithFile(ctx context.Context, account domains.Account, file *codexauth.AccountFile) (catalog.SourceIdentity, []catalog.RemoteModel, error) {
	client, err := chatGPTClient(file)
	if err != nil {
		return catalog.SourceIdentity{}, nil, err
	}
	// 客户端兼容版本由 proxy-api-lib 统一维护，业务层不绑定 Codex 私有协议版本。
	source := client.Codex.CatalogSource(accountRouteID(account, file), "")
	models, err := source.ListModels(ctx)
	return source.Identity(), models, err
}

// SyncModels 同步单个账号或全部启用官方账号的模型目录。
func (s AccountService) SyncModels(input ModelSyncInput) (ModelSyncSweepResult, error) {
	var accounts []domains.Account
	query := global.NAV_DB.Where("enabled = ? AND encrypted_account_file <> ?", true, "")
	if guid := strings.TrimSpace(input.AccountGuid); guid != "" {
		query = global.NAV_DB.Where("guid = ?", guid)
	}
	if vendor := strings.TrimSpace(input.VendorCode); vendor != "" {
		query = query.Where("vendor_code = ?", vendor)
	}
	if product := strings.TrimSpace(input.ProductCode); product != "" {
		query = query.Where("product_code = ?", product)
	}
	if err := query.Order("priority asc, id asc").Find(&accounts).Error; err != nil {
		return ModelSyncSweepResult{}, err
	}
	result := ModelSyncSweepResult{Results: make([]ModelSyncStats, 0, len(accounts))}
	for _, account := range accounts {
		result.Checked++
		stats, err := s.syncAccountModels(account.Guid)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, account.Guid+": "+err.Error())
			continue
		}
		result.Updated++
		result.Results = append(result.Results, stats)
	}
	return result, nil
}

func (s AccountService) RefreshUsage(guid string) (RefreshUsageResult, error) {
	account, err := s.GetByGuid(guid)
	if err != nil {
		return RefreshUsageResult{}, err
	}
	ctx, cancel := contextWithOptionalTimeout(context.Background(), Config().RequestTimeout())
	defer cancel()
	file, err := s.ActiveAccountFile(ctx, account, false)
	if err != nil {
		return RefreshUsageResult{}, err
	}
	usage, err := s.fetchUsageWithFile(ctx, account, file)
	if isChatGPTUnauthorized(err) {
		file, err = s.ActiveAccountFile(ctx, account, true)
		if err == nil {
			usage, err = s.fetchUsageWithFile(ctx, account, file)
		}
	}
	if err != nil {
		s.recordSyncFailure(account, err)
		return RefreshUsageResult{}, err
	}
	client, err := chatGPTClient(file)
	if err != nil {
		return RefreshUsageResult{}, err
	}
	subscription, subscriptionErr := client.Accounts.Subscription(ctx, accountRouteID(account, file))
	quotas, err := QuotaServiceApp.SyncRateLimit(account.Guid, "", "wham", usage.RateLimit, usage.Raw)
	if err != nil {
		return RefreshUsageResult{}, err
	}
	for _, additional := range usage.AdditionalRateLimits {
		if additional.RateLimit == nil {
			continue
		}
		items, syncErr := QuotaServiceApp.SyncRateLimit(account.Guid, firstNonEmpty(additional.SourceKey, additional.LimitID, additional.MeteredFeature), "wham", additional.RateLimit, additional.Raw)
		if syncErr != nil {
			return RefreshUsageResult{}, syncErr
		}
		quotas = append(quotas, items...)
	}
	updates := map[string]any{
		"last_refreshed_at": time.Now().UnixMilli(), "last_error": "", "token_status": domains.TokenStatusActive,
		"plan_type": firstNonEmpty(usage.PlanType, file.Meta.PlanType, account.PlanType),
	}
	if subscriptionErr == nil && subscription != nil {
		applySubscriptionSnapshot(updates, file, subscription)
	}
	blocked, _ := QuotaServiceApp.HasBlockingQuota(account.Guid)
	if blocked {
		updates["status"] = domains.AccountStatusExhausted
	} else if account.Enabled {
		updates["status"] = domains.AccountStatusAvailable
		updates["failure_count"] = 0
		updates["cooldown_until"] = int64(0)
	}
	if err := s.persistAccountFile(account, file, updates); err != nil {
		return RefreshUsageResult{}, err
	}
	allQuotas, _ := QuotaServiceApp.ListByAccount(account.Guid)
	raw := map[string]any{"usage": json.RawMessage(usage.Raw)}
	if subscriptionErr == nil && subscription != nil {
		raw["subscription"] = json.RawMessage(subscription.Raw)
	} else if subscriptionErr != nil {
		raw["subscriptionWarning"] = subscriptionErr.Error()
	}
	return RefreshUsageResult{AccountGuid: account.Guid, UsageType: "wham", Quotas: allQuotas, PlanType: updates["plan_type"].(string), Raw: raw}, nil
}

func (s AccountService) fetchUsageWithFile(ctx context.Context, account domains.Account, file *codexauth.AccountFile) (*chatgpt.UsageSnapshot, error) {
	client, err := chatGPTClient(file)
	if err != nil {
		return nil, err
	}
	return client.Usage.Get(ctx, accountRouteID(account, file))
}

func (s AccountService) Probe(guid string, input AccountTestInput) (map[string]any, error) {
	account, err := s.GetByGuid(guid)
	if err != nil {
		return nil, err
	}
	model := strings.TrimSpace(input.Model)
	if model == "" {
		model, _ = ModelServiceApp.FirstAvailableModelForAccount(account.Guid)
	}
	if model == "" {
		models, fetchErr := s.FetchModels(FetchAccountModelsInput{Guid: guid})
		if fetchErr != nil || len(models) == 0 {
			return nil, firstError(fetchErr, errors.New("no Codex model is available for probing"))
		}
		model = models[0]
	}
	prompt := firstNonEmpty(input.Prompt, "Reply with OK.")
	body, _ := json.Marshal(map[string]any{
		"model":        model,
		"instructions": "Reply briefly.",
		"input": []map[string]any{{
			"type": "message", "role": "user",
			"content": []map[string]any{{"type": "input_text", "text": prompt}},
		}},
		"store": false,
	})
	ctx, cancel := contextWithOptionalTimeout(context.Background(), Config().RequestTimeout())
	defer cancel()
	started := time.Now()
	result, err := ProxyAPIClientApp.Do(ctx, account, ProxyRequest{Endpoint: "/v1/responses", Model: model, Body: body})
	if err != nil {
		return nil, err
	}
	if _, sampleErr := QuotaServiceApp.SampleHeaders(account.Guid, "active_probe", result.Header); sampleErr != nil {
		return nil, sampleErr
	}
	ok := result.StatusCode >= 200 && result.StatusCode < 300 && result.ErrorType == ""
	if ok {
		_ = s.MarkUsed(account.Guid)
	} else {
		QuotaServiceApp.ApplyError(account.Guid, result.ErrorType)
	}
	quotas, _ := QuotaServiceApp.ListByAccount(account.Guid)
	return map[string]any{
		"ok": ok, "model": model, "statusCode": result.StatusCode,
		"errorType": result.ErrorType, "errorSummary": result.ErrorSummary,
		"latencyMs": time.Since(started).Milliseconds(), "quotas": quotas,
	}, nil
}

func (s AccountService) RefreshDueUsageAccounts() (UsageRefreshSweepResult, error) {
	var accounts []domains.Account
	if err := global.NAV_DB.Where("enabled = ?", true).Order("last_refreshed_at asc").Find(&accounts).Error; err != nil {
		return UsageRefreshSweepResult{}, err
	}
	now := time.Now()
	interval := time.Duration(Config().QuotaRefreshSeconds) * time.Second
	if interval <= 0 {
		interval = 3 * time.Minute
	}
	result := UsageRefreshSweepResult{}
	for _, account := range accounts {
		jitter := deterministicJitter(account.Guid, interval/5)
		if account.LastRefreshedAt > 0 && time.UnixMilli(account.LastRefreshedAt).Add(interval+jitter).After(now) {
			continue
		}
		result.Checked++
		if _, err := s.RefreshUsage(account.Guid); err != nil {
			result.Failed++
			continue
		}
		result.Updated++
	}
	return result, nil
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
		return nil
	})
}

func (s AccountService) SetEnabled(guid string, enabled bool) error {
	account, _ := s.GetByGuid(guid)
	status := domains.AccountStatusDisabled
	if enabled {
		status = domains.AccountStatusAvailable
	}
	err := global.NAV_DB.Model(&domains.Account{}).Where("guid = ?", guid).Updates(map[string]any{"enabled": enabled, "status": status}).Error
	if err == nil {
		AccountGroupServiceApp.RefreshSummaries(account.AccountGroup)
	}
	return err
}

func (s AccountService) MarkUsed(guid string) error {
	status := domains.AccountStatusAvailable
	if blocked, err := QuotaServiceApp.HasBlockingQuota(guid); err == nil && blocked {
		status = domains.AccountStatusExhausted
	}
	return global.NAV_DB.Model(&domains.Account{}).Where("guid = ?", guid).Updates(map[string]any{
		"last_used_at": time.Now().UnixMilli(), "failure_count": 0, "status": status,
	}).Error
}

// AccountFailurePolicy 控制路由请求失败后是否需要保留最后一个可用账号。
type AccountFailurePolicy struct {
	PreserveLastAvailable bool
}

func (s AccountService) MarkFailure(guid, errorType string) error {
	return s.MarkFailureWithPolicy(guid, errorType, AccountFailurePolicy{})
}

// MarkFailureWithPolicy 记录账号失败。最后一个可用账号只豁免可恢复错误触发的自动冷却。
func (s AccountService) MarkFailureWithPolicy(guid, errorType string, policy AccountFailurePolicy) error {
	account, err := s.GetByGuid(guid)
	if err != nil {
		return err
	}
	status, cooldownUntil := accountFailureState(
		account,
		errorType,
		policy,
		time.Now(),
		time.Duration(Config().CooldownSeconds)*time.Second,
	)
	return global.NAV_DB.Model(&account).Updates(map[string]any{
		"failure_count": account.FailureCount + 1, "status": status, "cooldown_until": cooldownUntil,
	}).Error
}

func accountFailureState(account domains.Account, errorType string, policy AccountFailurePolicy, now time.Time, cooldown time.Duration) (string, int64) {
	status := account.Status
	cooldownUntil := account.CooldownUntil
	switch errorType {
	case domains.ErrorAuthFailed:
		status = domains.AccountStatusInvalid
	case domains.ErrorRateLimited:
		status = domains.AccountStatusLimited
		cooldownUntil = now.Add(cooldown).UnixMilli()
	case domains.ErrorQuotaExhausted:
		status = domains.AccountStatusExhausted
	case domains.ErrorUpstreamHTTP5xx, domains.ErrorUpstream5xx, domains.ErrorUpstreamFailed,
		domains.ErrorStreamIncomplete, domains.ErrorNetwork, domains.ErrorUpstreamTimeout:
		if account.FailureCount+1 >= 3 {
			if policy.PreserveLastAvailable {
				cooldownUntil = 0
			} else {
				status = domains.AccountStatusCooldown
				cooldownUntil = now.Add(cooldown).UnixMilli()
			}
		}
	}
	return status, cooldownUntil
}

func (s AccountService) MarkExpiredSubscriptions() error {
	now := time.Now().UnixMilli()
	return global.NAV_DB.Model(&domains.Account{}).
		Where("enabled = ? AND subscription_will_renew = ? AND subscription_expired_at > 0 AND subscription_expired_at <= ?", true, false, now).
		Update("status", domains.AccountStatusExpired).Error
}

func (s AccountService) FindAvailable(accountGroup, model string, limit int) ([]domains.Account, error) {
	if limit <= 0 {
		limit = 100
	}
	now := time.Now().UnixMilli()
	query := global.NAV_DB.Where("enabled = ? AND encrypted_account_file <> ? AND status NOT IN ?", true, "", []string{
		domains.AccountStatusDisabled, domains.AccountStatusLimited, domains.AccountStatusCooldown,
		domains.AccountStatusExpired, domains.AccountStatusInvalid, domains.AccountStatusExhausted,
	}).Where("(cooldown_until = 0 OR cooldown_until < ?)", now).
		Where("(subscription_expired_at = 0 OR subscription_expired_at > ? OR subscription_will_renew = ?)", now, true)
	if accountGroup != "" {
		query = query.Where("account_group = ?", accountGroup)
	}
	var list []domains.Account
	if err := query.Order("priority asc, last_used_at asc, id asc").Limit(limit).Find(&list).Error; err != nil {
		return nil, err
	}
	available := make([]domains.Account, 0, len(list))
	for _, account := range list {
		blocked, err := QuotaServiceApp.HasBlockingQuota(account.Guid)
		if err != nil {
			return nil, err
		}
		if !blocked {
			available = append(available, account)
		}
	}
	return available, nil
}

// LoadAccountFile 解密并解析账号的规范 OAuth 文件。
func (s AccountService) LoadAccountFile(account domains.Account) (*codexauth.AccountFile, error) {
	if strings.TrimSpace(account.EncryptedAccountFile) == "" {
		return nil, errors.New("account does not contain an OAuth account file")
	}
	utils.SetSecretKeyFile(Config().SecretKeyFile)
	plaintext, err := utils.DecryptSecret(account.EncryptedAccountFile)
	if err != nil {
		return nil, err
	}
	return codexauth.ParseAccountFile([]byte(plaintext))
}

// ActiveAccountFile 在必要时刷新 Access Token，并原子保存轮换后的 Refresh Token。
func (s AccountService) ActiveAccountFile(ctx context.Context, account domains.Account, force bool) (*codexauth.AccountFile, error) {
	file, err := s.LoadAccountFile(account)
	if err != nil {
		return nil, err
	}
	if !force && !file.NeedsRefresh(time.Now(), tokenRefreshSkew) {
		return file, nil
	}
	value, err, _ := accountTokenRefreshGroup.Do(account.Guid, func() (any, error) {
		current, err := s.GetByGuid(account.Guid)
		if err != nil {
			return nil, err
		}
		currentFile, err := s.LoadAccountFile(current)
		if err != nil {
			return nil, err
		}
		if !force && !currentFile.NeedsRefresh(time.Now(), tokenRefreshSkew) {
			return currentFile, nil
		}
		if strings.TrimSpace(currentFile.Tokens.RefreshToken) == "" {
			return nil, errors.New("OAuth account does not contain refresh_token")
		}
		httpClient, err := UpstreamHTTPClient()
		if err != nil {
			return nil, err
		}
		oauth := codexauth.NewOAuthClient(codexauth.WithIssuer(currentFile.Meta.Issuer), codexauth.WithHTTPClient(httpClient))
		tokens, err := oauth.Refresh(ctx, currentFile.Tokens.RefreshToken)
		if err != nil {
			s.recordTokenRefreshFailure(current, err)
			return nil, err
		}
		if err := currentFile.ApplyTokenSet(*tokens); err != nil {
			return nil, err
		}
		metadata := accountMetadata(currentFile)
		if err := s.persistAccountFile(current, currentFile, metadata.updates()); err != nil {
			return nil, err
		}
		return currentFile, nil
	})
	if err != nil {
		return nil, err
	}
	return value.(*codexauth.AccountFile), nil
}

func (s AccountService) persistAccountFile(account domains.Account, file *codexauth.AccountFile, updates map[string]any) error {
	encoded, err := file.Marshal()
	if err != nil {
		return err
	}
	encrypted, err := encryptAccountFile(encoded)
	if err != nil {
		return err
	}
	if updates == nil {
		updates = map[string]any{}
	}
	updates["encrypted_account_file"] = encrypted
	return global.NAV_DB.Model(&domains.Account{}).Where("guid = ?", account.Guid).Updates(updates).Error
}

func (s AccountService) recordTokenRefreshFailure(account domains.Account, err error) {
	_ = global.NAV_DB.Model(&account).Updates(map[string]any{
		"token_status": domains.TokenStatusRefreshFailed, "last_error": truncateError(err),
	}).Error
}

func (s AccountService) recordSyncFailure(account domains.Account, err error) {
	_ = global.NAV_DB.Model(&account).Updates(map[string]any{
		"last_refreshed_at": time.Now().UnixMilli(), "last_error": truncateError(err),
	}).Error
}

type normalizedAccountMetadata struct {
	name, email, tokenStatus string
	accessTokenExpiresAt     int64
	file                     *codexauth.AccountFile
}

func accountMetadata(file *codexauth.AccountFile) normalizedAccountMetadata {
	label := strings.TrimSpace(file.Meta.Label)
	name := label
	email := ""
	if strings.Contains(label, "@") {
		email = label
		name = strings.SplitN(label, "@", 2)[0]
	}
	if name == "" {
		name = file.Tokens.AccountID
	}
	expiresAt := int64(0)
	status := domains.TokenStatusActive
	if expiry, ok := file.AccessTokenExpiresAt(); ok {
		expiresAt = expiry.UnixMilli()
		if !expiry.After(time.Now().Add(tokenRefreshSkew)) {
			status = domains.TokenStatusRefreshNeeded
		}
	}
	return normalizedAccountMetadata{name: name, email: email, tokenStatus: status, accessTokenExpiresAt: expiresAt, file: file}
}

func (m normalizedAccountMetadata) updates() map[string]any {
	return map[string]any{
		"email": m.email, "chat_gpt_account_id": m.file.Tokens.AccountID, "workspace_id": m.file.Meta.WorkspaceID,
		"plan_type": m.file.Meta.PlanType, "subscription_plan": m.file.Meta.SubscriptionPlan,
		"subscription_expired_at": normalizeUnixMillis(m.file.Meta.SubscriptionExpiresAt),
		"subscription_renews_at":  normalizeUnixMillis(m.file.Meta.SubscriptionRenewsAt),
		"subscription_will_renew": m.file.Meta.SubscriptionWillRenew,
		"access_token_expires_at": m.accessTokenExpiresAt, "token_status": m.tokenStatus,
	}
}

func chatGPTClient(file *codexauth.AccountFile) (*chatgpt.Client, error) {
	if file == nil || strings.TrimSpace(file.Tokens.AccessToken) == "" {
		return nil, errors.New("OAuth access token is empty")
	}
	httpClient, err := UpstreamHTTPClient()
	if err != nil {
		return nil, err
	}
	return chatgpt.NewClient(
		chatgpt.WithHTTPClient(httpClient), chatgpt.WithAccessToken(file.Tokens.AccessToken),
		chatgpt.WithOriginator(GatewayProxyConfig().Originator), chatgpt.WithUserAgent(chatgpt.DefaultUserAgent),
	), nil
}

func accountRouteID(account domains.Account, file *codexauth.AccountFile) string {
	return firstNonEmpty(account.ChatGPTAccountID, file.Tokens.AccountID, account.WorkspaceID, file.Meta.WorkspaceID)
}

func isChatGPTUnauthorized(err error) bool {
	var apiErr *chatgpt.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == 401
}

func normalizeAccountFileJSON(raw json.RawMessage) ([]byte, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, errors.New("accountFile is required")
	}
	if strings.HasPrefix(trimmed, "\"") {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, err
		}
		return []byte(text), nil
	}
	return []byte(trimmed), nil
}

func encryptAccountFile(raw []byte) (string, error) {
	utils.SetSecretKeyFile(Config().SecretKeyFile)
	return utils.EncryptSecret(string(raw))
}

func accountCredentialHint(accountID string) string {
	accountID = strings.TrimSpace(accountID)
	if len(accountID) <= 10 {
		return accountID
	}
	return accountID[:4] + "…" + accountID[len(accountID)-6:]
}

func normalizeUnixMillis(value int64) int64 {
	if value > 0 && value < 1_000_000_000_000 {
		return value * 1000
	}
	return value
}

func timePointerMillis(value *time.Time) int64 {
	if value == nil || value.IsZero() {
		return 0
	}
	return value.UnixMilli()
}

func applySubscriptionSnapshot(updates map[string]any, file *codexauth.AccountFile, snapshot *chatgpt.SubscriptionSnapshot) {
	if updates == nil || file == nil || snapshot == nil {
		return
	}
	planType := firstNonEmpty(snapshot.AccountPlanType, stringUpdate(updates, "plan_type"), file.Meta.PlanType)
	if planType != "" {
		updates["plan_type"] = planType
		file.Meta.PlanType = planType
	}
	if subscriptionPlan := strings.TrimSpace(snapshot.SubscriptionPlan); subscriptionPlan != "" {
		updates["subscription_plan"] = subscriptionPlan
		file.Meta.SubscriptionPlan = subscriptionPlan
	}
	if snapshot.ExpiresAt != nil {
		expiresAt := timePointerMillis(snapshot.ExpiresAt)
		updates["subscription_expired_at"] = expiresAt
		file.Meta.SubscriptionExpiresAt = expiresAt
	}
	if snapshot.RenewsAt != nil {
		renewsAt := timePointerMillis(snapshot.RenewsAt)
		updates["subscription_renews_at"] = renewsAt
		file.Meta.SubscriptionRenewsAt = renewsAt
	}
	if snapshot.WillRenew != nil {
		willRenew := *snapshot.WillRenew
		updates["subscription_will_renew"] = willRenew
		file.Meta.SubscriptionWillRenew = boolPointer(willRenew)
	}
	if snapshot.HasSubscription != nil && !*snapshot.HasSubscription && snapshot.ExpiresAt == nil && snapshot.RenewsAt == nil {
		updates["subscription_expired_at"] = int64(0)
		updates["subscription_renews_at"] = int64(0)
		updates["subscription_will_renew"] = false
		file.Meta.SubscriptionExpiresAt = 0
		file.Meta.SubscriptionRenewsAt = 0
		file.Meta.SubscriptionWillRenew = boolPointer(false)
	}
}

func stringUpdate(updates map[string]any, key string) string {
	value, _ := updates[key].(string)
	return strings.TrimSpace(value)
}

func boolPointer(value bool) *bool {
	return &value
}

func deterministicJitter(key string, maximum time.Duration) time.Duration {
	if maximum <= 0 {
		return 0
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(key))
	return time.Duration(hash.Sum64() % uint64(maximum))
}

func truncateError(err error) string {
	if err == nil {
		return ""
	}
	value := strings.Join(strings.Fields(err.Error()), " ")
	if len(value) > 500 {
		value = value[:500]
	}
	return value
}

func firstError(primary, fallback error) error {
	if primary != nil {
		return primary
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
