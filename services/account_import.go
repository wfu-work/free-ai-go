package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/proxy-api-lib/codexauth"
)

const (
	maxAccountImportBytes   = 16 << 20
	maxSub2APIAccounts      = 100
	sub2APIImportFormat     = "sub2api"
	nativeImportFormat      = "freeai"
	accountImportStatusOK   = "imported"
	accountImportStatusFail = "failed"
)

// ImportAccountsResult 是批量账号文件导入的脱敏结果。不会包含任何 OAuth 令牌。
type ImportAccountsResult struct {
	Format   string                    `json:"format"`
	Total    int                       `json:"total"`
	Imported int                       `json:"imported"`
	Failed   int                       `json:"failed"`
	Items    []ImportAccountItemResult `json:"items"`
}

type ImportAccountItemResult struct {
	Index   int              `json:"index"`
	Name    string           `json:"name,omitempty"`
	Status  string           `json:"status"`
	Account *domains.Account `json:"account,omitempty"`
	Error   string           `json:"error,omitempty"`
}

type accountImportCandidate struct {
	Index    int
	Name     string
	Priority int
	File     *codexauth.AccountFile
	Err      error
}

// ImportFile 导入 FreeAI 原生账号文件或 sub2api-data v1 导出文件。
// sub2api 批量文件按账号独立处理，单条失败不会阻止其他有效账号入池。
func (s AccountService) ImportFile(input ImportAccountInput) (ImportAccountsResult, error) {
	if len(input.AccountFile) > maxAccountImportBytes {
		return ImportAccountsResult{}, fmt.Errorf("account file is too large (maximum %d MiB)", maxAccountImportBytes>>20)
	}
	format, candidates, err := parseImportCandidates(input.AccountFile)
	if err != nil {
		return ImportAccountsResult{}, fmt.Errorf("invalid account import file: %w", err)
	}
	basePool, err := normalizeOpenAICodexPool(AccountPoolInput{VendorCode: input.VendorCode})
	if err != nil {
		return ImportAccountsResult{}, err
	}
	result := ImportAccountsResult{
		Format: format,
		Total:  len(candidates),
		Items:  make([]ImportAccountItemResult, 0, len(candidates)),
	}
	for _, candidate := range candidates {
		item := ImportAccountItemResult{Index: candidate.Index, Name: candidate.Name}
		if candidate.Err != nil {
			item.Status = accountImportStatusFail
			item.Error = candidate.Err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		pool := AccountPoolInput{
			VendorCode: basePool.VendorCode, Name: input.Name, AccountGroup: input.AccountGroup,
			Priority: input.Priority, Weight: input.Weight, Remark: input.Remark,
		}
		if format == sub2APIImportFormat && pool.Priority == 0 && candidate.Priority > 0 {
			pool.Priority = candidate.Priority
		}
		if strings.TrimSpace(pool.Name) == "" || len(candidates) > 1 {
			pool.Name = candidate.Name
		}
		account, importErr := s.upsertOfficialAccount(candidate.File, pool, "account.import_file")
		if importErr != nil {
			item.Status = accountImportStatusFail
			item.Error = importErr.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		item.Status = accountImportStatusOK
		item.Account = &account
		if item.Name == "" {
			item.Name = account.Name
		}
		result.Imported++
		result.Items = append(result.Items, item)
		s.SyncOfficialAccountAsync(account.Guid)
	}
	return result, nil
}

type sub2APIExport struct {
	Type       string           `json:"type"`
	Version    int              `json:"version"`
	ExportedAt string           `json:"exported_at"`
	Accounts   []sub2APIAccount `json:"accounts"`
}

type sub2APIAccount struct {
	Name        string             `json:"name"`
	Type        string             `json:"type"`
	Platform    string             `json:"platform"`
	Priority    int                `json:"priority"`
	Extra       sub2APIExtra       `json:"extra"`
	Credentials sub2APICredentials `json:"credentials"`
}

type sub2APIExtra struct {
	Email       string `json:"email"`
	WorkspaceID string `json:"workspace_id"`
}

type sub2APICredentials struct {
	Name             string       `json:"name"`
	Type             string       `json:"type"`
	Email            string       `json:"email"`
	Extra            sub2APIExtra `json:"extra"`
	IDToken          string       `json:"id_token"`
	ClientID         string       `json:"client_id"`
	PlanType         string       `json:"plan_type"`
	AccountID        string       `json:"account_id"`
	AccessToken      string       `json:"access_token"`
	RefreshToken     string       `json:"refresh_token"`
	ChatGPTPlanType  string       `json:"chatgpt_plan_type"`
	ChatGPTAccountID string       `json:"chatgpt_account_id"`
}

// parseImportCandidates 识别 FreeAI 原生 OAuth 文件和 sub2api-data v1 文件。
// 解析过程中只在内存中保留令牌，调用方负责后续加密持久化。
func parseImportCandidates(raw json.RawMessage) (string, []accountImportCandidate, error) {
	normalized, err := normalizeAccountFileJSON(raw)
	if err != nil {
		return "", nil, err
	}
	var envelope struct {
		Type    string `json:"type"`
		Version int    `json:"version"`
	}
	if err := json.Unmarshal(normalized, &envelope); err != nil {
		return "", nil, err
	}
	if strings.EqualFold(strings.TrimSpace(envelope.Type), "sub2api-data") {
		return parseSub2APICandidates(normalized)
	}
	file, err := codexauth.ParseAccountFile(normalized)
	if err != nil {
		return "", nil, err
	}
	return nativeImportFormat, []accountImportCandidate{{Index: 0, File: file}}, nil
}

func parseSub2APICandidates(raw []byte) (string, []accountImportCandidate, error) {
	var source sub2APIExport
	if err := json.Unmarshal(raw, &source); err != nil {
		return "", nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(source.Type), "sub2api-data") {
		return "", nil, errors.New("unsupported account file format")
	}
	if source.Version != 1 {
		return "", nil, fmt.Errorf("unsupported sub2api file version: %d", source.Version)
	}
	if len(source.Accounts) == 0 {
		return "", nil, errors.New("sub2api account file does not contain accounts")
	}
	if len(source.Accounts) > maxSub2APIAccounts {
		return "", nil, fmt.Errorf("sub2api account file contains too many accounts (maximum %d)", maxSub2APIAccounts)
	}

	candidates := make([]accountImportCandidate, 0, len(source.Accounts))
	seenAccountIDs := make(map[string]struct{}, len(source.Accounts))
	for index, account := range source.Accounts {
		candidate := accountImportCandidate{Index: index, Name: sub2APIAccountName(account), Priority: account.Priority}
		file, err := convertSub2APIAccount(source, account)
		if err == nil {
			accountID := file.Tokens.AccountID
			if _, exists := seenAccountIDs[accountID]; exists {
				err = errors.New("duplicate account identifier in sub2api file")
				file = nil
			} else {
				seenAccountIDs[accountID] = struct{}{}
			}
		}
		candidate.File = file
		candidate.Err = err
		candidates = append(candidates, candidate)
	}
	return sub2APIImportFormat, candidates, nil
}

func convertSub2APIAccount(source sub2APIExport, account sub2APIAccount) (*codexauth.AccountFile, error) {
	if !strings.EqualFold(strings.TrimSpace(account.Type), "oauth") {
		return nil, errors.New("account type must be oauth")
	}
	if !strings.EqualFold(strings.TrimSpace(account.Platform), "openai") {
		return nil, errors.New("account platform must be openai")
	}
	credentials := account.Credentials
	if !strings.EqualFold(strings.TrimSpace(credentials.Type), "") &&
		!strings.EqualFold(strings.TrimSpace(credentials.Type), "oauth") {
		return nil, errors.New("credential type must be oauth")
	}
	if strings.TrimSpace(credentials.AccessToken) == "" {
		return nil, errors.New("credentials.access_token is required")
	}
	if clientID := strings.TrimSpace(credentials.ClientID); clientID != "" && clientID != codexauth.DefaultClientID {
		return nil, errors.New("credentials.client_id is not supported")
	}
	if primary, fallback := strings.TrimSpace(credentials.ChatGPTAccountID), strings.TrimSpace(credentials.AccountID); primary != "" && fallback != "" && primary != fallback {
		return nil, errors.New("credentials account identifiers do not match")
	}
	accountID := firstNonEmpty(credentials.ChatGPTAccountID, credentials.AccountID)
	workspaceID := firstNonEmpty(account.Extra.WorkspaceID, credentials.Extra.WorkspaceID, credentials.AccountID)
	label := firstNonEmpty(credentials.Email, account.Extra.Email, credentials.Extra.Email)
	planType := firstNonEmpty(credentials.ChatGPTPlanType, credentials.PlanType)
	exportedAt := parseSub2APIExportedAt(source.ExportedAt)

	file := &codexauth.AccountFile{
		Tokens: codexauth.AccountTokens{
			AccessToken:  strings.TrimSpace(credentials.AccessToken),
			IDToken:      strings.TrimSpace(credentials.IDToken),
			RefreshToken: strings.TrimSpace(credentials.RefreshToken),
			AccountID:    strings.TrimSpace(accountID),
		},
		Meta: codexauth.AccountMeta{
			Label:            strings.TrimSpace(label),
			Issuer:           codexauth.DefaultIssuer,
			Status:           "active",
			WorkspaceID:      strings.TrimSpace(workspaceID),
			ChatGPTAccountID: strings.TrimSpace(accountID),
			ExportedAt:       exportedAt,
			PlanType:         strings.TrimSpace(planType),
		},
	}
	if err := file.Normalize(); err != nil {
		return nil, fmt.Errorf("invalid OAuth credentials: %w", err)
	}
	if claims, err := codexauth.ParseUnverifiedClaims(file.Tokens.AccessToken); err == nil {
		if claimedID := strings.TrimSpace(claims.ResolvedAccountID()); claimedID != "" && claimedID != file.Tokens.AccountID {
			return nil, errors.New("credentials account identifier does not match access token")
		}
	}
	return file, nil
}

func sub2APIAccountName(account sub2APIAccount) string {
	return firstNonEmpty(account.Name, account.Credentials.Name, account.Credentials.Email, account.Extra.Email)
}

func parseSub2APIExportedAt(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return 0
	}
	return parsed.UnixMilli()
}
