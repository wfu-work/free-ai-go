package services

import (
	"context"
	"errors"
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
	AvailableCount           int  `json:"availableCount"`
	ApplicableAvailableCount *int `json:"applicableAvailableCount,omitempty"`
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
	SyncedAt                 int64                `json:"syncedAt"`
}

type ConsumeAccountResetCreditInput struct {
	IdempotencyKey string `json:"idempotencyKey"`
	CreditID       string `json:"creditId"`
}

type ConsumeAccountResetCreditResult struct {
	AccountGuid    string                    `json:"accountGuid"`
	Outcome        string                    `json:"outcome"`
	CreditID       string                    `json:"creditId,omitempty"`
	IdempotencyKey string                    `json:"idempotencyKey"`
	ResetCredits   AccountResetCreditsResult `json:"resetCredits"`
	Usage          *RefreshUsageResult       `json:"usage,omitempty"`
	RefreshWarning string                    `json:"refreshWarning,omitempty"`
}

func (s AccountResetCreditService) List(ctx context.Context, guid string) (AccountResetCreditsResult, error) {
	account, err := AccountServiceApp.GetByGuid(strings.TrimSpace(guid))
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
	return result, err
}

func (s AccountResetCreditService) Consume(ctx context.Context, guid string, input ConsumeAccountResetCreditInput) (ConsumeAccountResetCreditResult, error) {
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.CreditID = strings.TrimSpace(input.CreditID)
	if _, err := uuid.Parse(input.IdempotencyKey); err != nil {
		return ConsumeAccountResetCreditResult{}, errors.New("idempotencyKey must be a valid UUID")
	}

	lockValue, _ := accountResetCreditLocks.LoadOrStore(guid, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	account, err := AccountServiceApp.GetByGuid(strings.TrimSpace(guid))
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
		if credits, listErr := s.List(ctx, account.Guid); listErr == nil {
			result.ResetCredits = credits
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
	if outcome.IsIdempotentSuccess() {
		usage, refreshErr := AccountServiceApp.RefreshUsage(account.Guid)
		if refreshErr == nil {
			result.Usage = &usage
		} else {
			result.RefreshWarning = truncateError(refreshErr)
		}
	}
	if outcome.IsKnown() {
		credits, listErr := s.List(ctx, account.Guid)
		if listErr == nil {
			result.ResetCredits = credits
		} else if result.RefreshWarning == "" {
			result.RefreshWarning = truncateError(listErr)
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
	return AccountResetCreditSummary{
		AvailableCount:           credits.AvailableCount,
		ApplicableAvailableCount: credits.ApplicableAvailableCount,
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
