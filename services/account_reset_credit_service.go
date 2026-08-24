package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/nav-common-go-lib/global"
	"github.com/wfu-work/proxy-api-lib/chatgpt"
	"github.com/wfu-work/proxy-api-lib/codexauth"
	"gorm.io/gorm"
)

type AccountResetCreditService struct{}

var AccountResetCreditServiceApp = AccountResetCreditService{}
var accountResetCreditLocks sync.Map

// AccountResetCreditSummary 是管理端可安全展示的重置券汇总。
type AccountResetCreditSummary struct {
	AvailableCount           int   `json:"availableCount"`
	ApplicableAvailableCount *int  `json:"applicableAvailableCount,omitempty"`
	ExpiresAt                int64 `json:"expiresAt,omitempty"`
	SyncedAt                 int64 `json:"syncedAt,omitempty"`
}

// AccountResetCredit 是一张官方额度重置券；官方时间戳统一转换为毫秒。
type AccountResetCredit struct {
	ID          string `json:"id"`
	ResetType   string `json:"resetType"`
	Status      string `json:"status"`
	GrantedAt   int64  `json:"grantedAt,omitempty"`
	ExpiresAt   int64  `json:"expiresAt,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

type AccountResetCreditsResult struct {
	AccountGuid              string               `json:"accountGuid"`
	AvailableCount           int                  `json:"availableCount"`
	ApplicableAvailableCount *int                 `json:"applicableAvailableCount,omitempty"`
	Credits                  []AccountResetCredit `json:"credits"`
	DetailsAvailable         bool                 `json:"detailsAvailable"`
	ExpiresAt                int64                `json:"expiresAt,omitempty"`
	SyncedAt                 int64                `json:"syncedAt"`
}

type ConsumeAccountResetCreditInput struct {
	IdempotencyKey string `json:"idempotencyKey"`
	CreditID       string `json:"creditId"`
}

type ConsumeAccountResetCreditResult struct {
	AccountGuid    string                     `json:"accountGuid"`
	Outcome        string                     `json:"outcome"`
	CreditID       string                     `json:"creditId,omitempty"`
	IdempotencyKey string                     `json:"idempotencyKey"`
	ResetCredits   *AccountResetCreditsResult `json:"resetCredits,omitempty"`
	Usage          *RefreshUsageResult        `json:"usage,omitempty"`
	RefreshWarning string                     `json:"refreshWarning,omitempty"`
}

func (s AccountResetCreditService) List(ctx context.Context, guid string) (AccountResetCreditsResult, error) {
	guid = strings.TrimSpace(guid)
	lock := accountResetCreditLock(guid)
	lock.Lock()
	defer lock.Unlock()
	return s.listUnlocked(ctx, guid)
}

func (s AccountResetCreditService) listUnlocked(ctx context.Context, guid string) (AccountResetCreditsResult, error) {
	account, err := AccountServiceApp.GetByGuid(guid)
	if err != nil {
		return AccountResetCreditsResult{}, err
	}
	if account.ProductCode != domains.ProductCodex || account.CredentialType != domains.CredentialOAuth {
		return AccountResetCreditsResult{}, errors.New("only Codex OAuth accounts support rate-limit reset credits")
	}
	file, err := AccountServiceApp.ActiveAccountFile(ctx, account, false)
	if err != nil {
		return AccountResetCreditsResult{}, err
	}
	result, err := s.listWithFile(ctx, account, file)
	if isChatGPTUnauthorized(err) {
		file, err = AccountServiceApp.ActiveAccountFile(ctx, account, true)
		if err == nil {
			result, err = s.listWithFile(ctx, account, file)
		}
	}
	if err != nil {
		return AccountResetCreditsResult{}, err
	}
	result = normalizeResetCreditResult(result)
	if err = s.persistSnapshot(result); err != nil {
		return AccountResetCreditsResult{}, fmt.Errorf("persist reset credit snapshot: %w", err)
	}
	return result, nil
}

func (s AccountResetCreditService) Consume(ctx context.Context, guid string, input ConsumeAccountResetCreditInput) (ConsumeAccountResetCreditResult, error) {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.CreditID = strings.TrimSpace(input.CreditID)
	if _, err := uuid.Parse(input.IdempotencyKey); err != nil {
		return ConsumeAccountResetCreditResult{}, errors.New("idempotencyKey must be a valid UUID")
	}

	guid = strings.TrimSpace(guid)
	lock := accountResetCreditLock(guid)
	lock.Lock()
	defer lock.Unlock()

	account, err := AccountServiceApp.GetByGuid(guid)
	if err != nil {
		return ConsumeAccountResetCreditResult{}, err
	}
	if account.ProductCode != domains.ProductCodex || account.CredentialType != domains.CredentialOAuth {
		return ConsumeAccountResetCreditResult{}, errors.New("only Codex OAuth accounts support rate-limit reset credits")
	}
	redemption, err := s.findOrCreateRedemption(account.Guid, input)
	if err != nil {
		return ConsumeAccountResetCreditResult{}, err
	}
	if redemption.Status == domains.ResetCreditRedemptionCompleted {
		result := ConsumeAccountResetCreditResult{
			AccountGuid: account.Guid, Outcome: redemption.Outcome,
			CreditID: redemption.CreditID, IdempotencyKey: redemption.IdempotencyKey,
		}
		if cached, found, cacheErr := s.persistedSnapshot(account.Guid); cacheErr == nil && found {
			result.ResetCredits = &cached
		} else if cacheErr != nil {
			result.RefreshWarning = appendResetCreditWarning(result.RefreshWarning, cacheErr)
		}
		if result.ResetCredits == nil {
			if credits, listErr := s.listUnlocked(ctx, account.Guid); listErr == nil {
				result.ResetCredits = &credits
			} else {
				result.RefreshWarning = appendResetCreditWarning(result.RefreshWarning, listErr)
			}
		}
		return result, nil
	}
	file, err := AccountServiceApp.ActiveAccountFile(ctx, account, false)
	if err != nil {
		return ConsumeAccountResetCreditResult{}, err
	}
	officialResult, err := s.consumeWithFile(ctx, account, file, input)
	if isChatGPTUnauthorized(err) {
		file, err = AccountServiceApp.ActiveAccountFile(ctx, account, true)
		if err == nil {
			officialResult, err = s.consumeWithFile(ctx, account, file, input)
		}
	}
	if err != nil {
		_ = global.NAV_DB.Model(&redemption).Updates(map[string]any{
			"status": domains.ResetCreditRedemptionPending, "last_error": truncateError(err),
		}).Error
		AuditServiceApp.Record("", "account.reset_credit.consume_failed", "account", account.Guid, map[string]any{
			"creditId": input.CreditID, "idempotencyKey": input.IdempotencyKey, "error": truncateError(err),
		})
		return ConsumeAccountResetCreditResult{}, err
	}

	outcome := officialResult.Outcome
	resolvedCreditID := firstNonEmpty(officialResult.CreditID, input.CreditID)
	if err := global.NAV_DB.Model(&redemption).Updates(map[string]any{
		"credit_id": resolvedCreditID, "outcome": string(outcome),
		"status": domains.ResetCreditRedemptionCompleted, "last_error": "", "completed_at": time.Now().UnixMilli(),
	}).Error; err != nil {
		return ConsumeAccountResetCreditResult{}, err
	}
	result := ConsumeAccountResetCreditResult{
		AccountGuid: account.Guid, Outcome: string(outcome),
		CreditID: resolvedCreditID, IdempotencyKey: input.IdempotencyKey,
	}
	if cached, cacheErr := s.applyConsumeOutcome(account.Guid, resolvedCreditID, outcome); cacheErr == nil {
		result.ResetCredits = cached
	} else {
		result.RefreshWarning = appendResetCreditWarning(result.RefreshWarning, cacheErr)
	}
	if outcome.IsIdempotentSuccess() {
		usage, refreshErr := AccountServiceApp.RefreshUsage(account.Guid)
		if refreshErr == nil {
			result.Usage = &usage
		} else {
			result.RefreshWarning = appendResetCreditWarning(result.RefreshWarning, refreshErr)
		}
	}
	// 消耗接口的结果比紧随其后的列表查询更可靠。官方列表可能存在短暂缓存，
	// 只有本地没有可更新的快照时才回查，避免成功扣减后数量立即回跳。
	if outcome.IsKnown() && result.ResetCredits == nil {
		credits, listErr := s.listUnlocked(ctx, account.Guid)
		if listErr == nil {
			result.ResetCredits = &credits
		} else {
			result.RefreshWarning = appendResetCreditWarning(result.RefreshWarning, listErr)
		}
	}
	AuditServiceApp.Record("", "account.reset_credit.consume", "account", account.Guid, map[string]any{
		"creditId": result.CreditID, "idempotencyKey": input.IdempotencyKey, "outcome": string(outcome),
	})
	return result, nil
}

func (s AccountResetCreditService) findOrCreateRedemption(accountGuid string, input ConsumeAccountResetCreditInput) (domains.AccountResetCreditRedemption, error) {
	var redemption domains.AccountResetCreditRedemption
	err := global.NAV_DB.Where("idempotency_key = ?", input.IdempotencyKey).First(&redemption).Error
	if err == nil {
		if redemption.AccountGuid != accountGuid {
			return domains.AccountResetCreditRedemption{}, errors.New("idempotencyKey belongs to another account")
		}
		if input.CreditID != "" && redemption.CreditID != "" && redemption.CreditID != input.CreditID {
			return domains.AccountResetCreditRedemption{}, errors.New("idempotencyKey was already used for another reset credit")
		}
		return redemption, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return domains.AccountResetCreditRedemption{}, err
	}
	redemption = domains.AccountResetCreditRedemption{
		AccountGuid: accountGuid, IdempotencyKey: input.IdempotencyKey, CreditID: input.CreditID,
		Status: domains.ResetCreditRedemptionPending, CreatedAt: time.Now().UnixMilli(),
	}
	if err := global.NAV_DB.Create(&redemption).Error; err != nil {
		if lookupErr := global.NAV_DB.Where("idempotency_key = ?", input.IdempotencyKey).First(&redemption).Error; lookupErr == nil {
			return redemption, nil
		}
		return domains.AccountResetCreditRedemption{}, err
	}
	return redemption, nil
}

func (s AccountResetCreditService) listWithFile(ctx context.Context, account domains.Account, file *codexauth.AccountFile) (AccountResetCreditsResult, error) {
	client, err := chatGPTClient(file)
	if err != nil {
		return AccountResetCreditsResult{}, err
	}
	officialCredits, err := client.Resets.List(ctx, accountRouteID(account, file))
	if err != nil {
		return AccountResetCreditsResult{}, err
	}
	credits := make([]AccountResetCredit, 0, len(officialCredits.Credits))
	for _, credit := range officialCredits.SortedCredits() {
		if credit == nil {
			continue
		}
		credits = append(credits, AccountResetCredit{
			ID: strings.TrimSpace(credit.ID), ResetType: strings.TrimSpace(credit.ResetType), Status: strings.TrimSpace(credit.Status),
			GrantedAt: credit.GrantedAtMillis(), ExpiresAt: credit.ExpiresAtMillis(),
			Title: strings.TrimSpace(credit.Title), Description: strings.TrimSpace(credit.Description),
		})
	}
	return AccountResetCreditsResult{
		AccountGuid: account.Guid, AvailableCount: officialCredits.AvailableCount,
		ApplicableAvailableCount: officialCredits.ApplicableAvailableCount,
		Credits:                  credits, DetailsAvailable: officialCredits.DetailsAvailable(), SyncedAt: time.Now().UnixMilli(),
	}, nil
}

func (s AccountResetCreditService) consumeWithFile(ctx context.Context, account domains.Account, file *codexauth.AccountFile, input ConsumeAccountResetCreditInput) (*chatgpt.ConsumeRateLimitResetCreditResult, error) {
	client, err := chatGPTClient(file)
	if err != nil {
		return nil, err
	}
	return client.Resets.Consume(ctx, accountRouteID(account, file), input.IdempotencyKey, input.CreditID)
}

func accountResetCreditLock(guid string) *sync.Mutex {
	value, _ := accountResetCreditLocks.LoadOrStore(guid, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func normalizeResetCreditResult(result AccountResetCreditsResult) AccountResetCreditsResult {
	if result.AvailableCount < 0 {
		result.AvailableCount = 0
	}
	if result.ApplicableAvailableCount != nil {
		value := max(0, *result.ApplicableAvailableCount)
		result.ApplicableAvailableCount = &value
	}
	credits := make([]AccountResetCredit, 0, len(result.Credits))
	for _, credit := range result.Credits {
		credit.ID = strings.TrimSpace(credit.ID)
		credit.ResetType = strings.TrimSpace(credit.ResetType)
		credit.Status = strings.TrimSpace(credit.Status)
		credit.Title = strings.TrimSpace(credit.Title)
		credit.Description = strings.TrimSpace(credit.Description)
		credits = append(credits, credit)
	}
	if result.DetailsAvailable {
		result.Credits = credits
	} else {
		result.Credits = nil
	}
	result.ExpiresAt = earliestResetCreditExpiry(result.Credits)
	if result.SyncedAt <= 0 {
		result.SyncedAt = time.Now().UnixMilli()
	}
	return result
}

func earliestResetCreditExpiry(credits []AccountResetCredit) int64 {
	var expiresAt int64
	for _, credit := range credits {
		if !resetCreditIsAvailable(credit) || credit.ExpiresAt <= 0 {
			continue
		}
		if expiresAt == 0 || credit.ExpiresAt < expiresAt {
			expiresAt = credit.ExpiresAt
		}
	}
	return expiresAt
}

func resetCreditIsAvailable(credit AccountResetCredit) bool {
	return credit.Status == "" || strings.EqualFold(credit.Status, "available")
}

func (s AccountResetCreditService) persistSnapshot(result AccountResetCreditsResult) error {
	result = normalizeResetCreditResult(result)
	creditsJSON, err := json.Marshal(result.Credits)
	if err != nil {
		return err
	}
	var snapshot domains.AccountResetCreditSnapshot
	err = global.NAV_DB.Where("account_guid = ?", result.AccountGuid).First(&snapshot).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return global.NAV_DB.Create(&domains.AccountResetCreditSnapshot{
			AccountGuid: result.AccountGuid, AvailableCount: result.AvailableCount,
			ApplicableAvailableCount: result.ApplicableAvailableCount,
			DetailsAvailable:         result.DetailsAvailable, CreditsJSON: string(creditsJSON),
			ExpiresAt: result.ExpiresAt, SyncedAt: result.SyncedAt,
		}).Error
	}
	if err != nil {
		return err
	}
	return global.NAV_DB.Model(&snapshot).Updates(map[string]any{
		"available_count": result.AvailableCount, "applicable_available_count": result.ApplicableAvailableCount,
		"details_available": result.DetailsAvailable, "credits_json": string(creditsJSON),
		"expires_at": result.ExpiresAt, "synced_at": result.SyncedAt,
	}).Error
}

func (s AccountResetCreditService) persistedSnapshot(accountGuid string) (AccountResetCreditsResult, bool, error) {
	var snapshot domains.AccountResetCreditSnapshot
	err := global.NAV_DB.Where("account_guid = ?", accountGuid).First(&snapshot).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AccountResetCreditsResult{}, false, nil
	}
	if err != nil {
		return AccountResetCreditsResult{}, false, err
	}
	return resetCreditResultFromSnapshot(snapshot), true, nil
}

func resetCreditSnapshotsByAccount(accountGuids []string) (map[string]AccountResetCreditsResult, error) {
	results := make(map[string]AccountResetCreditsResult, len(accountGuids))
	if len(accountGuids) == 0 {
		return results, nil
	}
	var snapshots []domains.AccountResetCreditSnapshot
	if err := global.NAV_DB.Where("account_guid IN ?", accountGuids).Find(&snapshots).Error; err != nil {
		return nil, err
	}
	for _, snapshot := range snapshots {
		results[snapshot.AccountGuid] = resetCreditResultFromSnapshot(snapshot)
	}
	return results, nil
}

func resetCreditResultFromSnapshot(snapshot domains.AccountResetCreditSnapshot) AccountResetCreditsResult {
	var credits []AccountResetCredit
	if strings.TrimSpace(snapshot.CreditsJSON) != "" {
		_ = json.Unmarshal([]byte(snapshot.CreditsJSON), &credits)
	}
	return normalizeResetCreditResult(AccountResetCreditsResult{
		AccountGuid: snapshot.AccountGuid, AvailableCount: snapshot.AvailableCount,
		ApplicableAvailableCount: snapshot.ApplicableAvailableCount,
		Credits:                  credits, DetailsAvailable: snapshot.DetailsAvailable,
		ExpiresAt: snapshot.ExpiresAt, SyncedAt: snapshot.SyncedAt,
	})
}

func (s AccountResetCreditService) applyConsumeOutcome(accountGuid, creditID string, outcome chatgpt.RateLimitResetOutcome) (*AccountResetCreditsResult, error) {
	result, found, err := s.persistedSnapshot(accountGuid)
	if err != nil {
		return nil, err
	}
	if !found {
		if outcome != chatgpt.RateLimitResetOutcomeNoCredit {
			return nil, nil
		}
		result = AccountResetCreditsResult{AccountGuid: accountGuid, DetailsAvailable: false}
	}
	switch outcome {
	case chatgpt.RateLimitResetOutcomeReset, chatgpt.RateLimitResetOutcomeAlreadyRedeemed:
		result.AvailableCount = max(0, result.AvailableCount-1)
		if result.ApplicableAvailableCount != nil {
			value := max(0, *result.ApplicableAvailableCount-1)
			result.ApplicableAvailableCount = &value
		}
		result.Credits = removeConsumedResetCredit(result.Credits, creditID)
	case chatgpt.RateLimitResetOutcomeNoCredit:
		zero := 0
		result.AvailableCount = 0
		result.ApplicableAvailableCount = &zero
		if result.DetailsAvailable {
			result.Credits = []AccountResetCredit{}
		}
	case chatgpt.RateLimitResetOutcomeNothingToReset:
		zero := 0
		result.ApplicableAvailableCount = &zero
	default:
		return &result, nil
	}
	result.SyncedAt = time.Now().UnixMilli()
	result = normalizeResetCreditResult(result)
	if err = s.persistSnapshot(result); err != nil {
		return nil, err
	}
	return &result, nil
}

func removeConsumedResetCredit(credits []AccountResetCredit, creditID string) []AccountResetCredit {
	index := -1
	creditID = strings.TrimSpace(creditID)
	for current, credit := range credits {
		if creditID != "" && credit.ID == creditID {
			index = current
			break
		}
		if index < 0 && resetCreditIsAvailable(credit) {
			index = current
		}
	}
	if index < 0 {
		return credits
	}
	return append(credits[:index:index], credits[index+1:]...)
}

func appendResetCreditWarning(current string, err error) string {
	if err == nil {
		return current
	}
	message := truncateError(err)
	if current == "" {
		return message
	}
	if strings.Contains(current, message) {
		return current
	}
	return truncateError(errors.New(current + "; " + message))
}

func resetCreditSummaryFromUsage(raw []byte) (AccountResetCreditSummary, bool) {
	usage, err := chatgpt.ParseUsageSnapshot(raw)
	if err != nil {
		return AccountResetCreditSummary{}, false
	}
	return resetCreditSummaryFromSnapshot(usage.RateLimitResetCredits)
}

func resetCreditSummaryFromSnapshot(credits *chatgpt.RateLimitResetCredits) (AccountResetCreditSummary, bool) {
	if credits == nil {
		return AccountResetCreditSummary{}, false
	}
	expiresAt := int64(0)
	if credit := credits.NextAvailableCredit(); credit != nil {
		expiresAt = credit.ExpiresAtMillis()
	}
	return AccountResetCreditSummary{
		AvailableCount:           credits.AvailableCount,
		ApplicableAvailableCount: credits.ApplicableAvailableCount,
		ExpiresAt:                expiresAt,
	}, true
}

func resetCreditSummaryFromQuotas(quotas []domains.AccountQuota) *AccountResetCreditSummary {
	var latest *domains.AccountQuota
	for index := range quotas {
		quota := &quotas[index]
		if quota.Source != "wham" || strings.TrimSpace(quota.Extra) == "" {
			continue
		}
		if latest == nil || quota.LastSyncedAt > latest.LastSyncedAt {
			latest = quota
		}
	}
	if latest == nil {
		return nil
	}
	summary, ok := resetCreditSummaryFromUsage([]byte(latest.Extra))
	if !ok {
		return nil
	}
	return &summary
}
