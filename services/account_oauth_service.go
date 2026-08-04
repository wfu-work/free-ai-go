package services

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wfu-work/proxy-api-lib/codexauth"
)

const (
	accountOAuthCallbackAddress = "127.0.0.1:1455"
	accountOAuthCallbackURI     = "http://localhost:1455/auth/callback"
	accountOAuthSessionTTL      = 15 * time.Minute

	OAuthLoginModeBrowser = "browser"
	OAuthLoginModeDevice  = "device"

	OAuthSessionPending    = "pending"
	OAuthSessionCompleting = "completing"
	OAuthSessionSuccess    = "success"
	OAuthSessionFailed     = "failed"
	OAuthSessionCancelled  = "cancelled"
	OAuthSessionExpired    = "expired"
)

// AccountOAuthStartInput 描述一个官方账号授权登录会话。
type AccountOAuthStartInput struct {
	Mode string `json:"mode"`
	AccountPoolInput
}

// AccountOAuthCompleteInput 用于在本机回调未被自动接收时手动提交回调结果。
type AccountOAuthCompleteInput struct {
	CallbackURL string `json:"callbackUrl"`
	Code        string `json:"code"`
	State       string `json:"state"`
}

// AccountOAuthSessionResult 是可安全返回给管理端的授权会话快照。
type AccountOAuthSessionResult struct {
	ID                string `json:"id"`
	Mode              string `json:"mode"`
	Status            string `json:"status"`
	AuthorizationURL  string `json:"authorizationUrl,omitempty"`
	VerificationURL   string `json:"verificationUrl,omitempty"`
	UserCode          string `json:"userCode,omitempty"`
	IntervalSeconds   int64  `json:"intervalSeconds,omitempty"`
	ExpiresAt         int64  `json:"expiresAt"`
	AccountGuid       string `json:"accountGuid,omitempty"`
	Error             string `json:"error,omitempty"`
	CallbackListening bool   `json:"callbackListening,omitempty"`
}

type accountOAuthSession struct {
	result       AccountOAuthSessionResult
	state        string
	codeVerifier string
	pool         AccountPoolInput
	device       *codexauth.DeviceAuthorization
	ctx          context.Context
	cancel       context.CancelFunc
}

// AccountOAuthService 管理短期 OAuth 登录会话和本机浏览器回调监听器。
type AccountOAuthService struct {
	mu              sync.Mutex
	sessions        map[string]*accountOAuthSession
	states          map[string]string
	callbackMu      sync.Mutex
	callbackStarted bool
}

// AccountOAuthServiceApp 是账号官方授权登录服务的应用单例。
var AccountOAuthServiceApp = NewAccountOAuthService()

// NewAccountOAuthService 创建一个隔离的 OAuth 登录会话服务。
func NewAccountOAuthService() *AccountOAuthService {
	return &AccountOAuthService{
		sessions: make(map[string]*accountOAuthSession),
		states:   make(map[string]string),
	}
}

// Start 创建浏览器 PKCE 或设备码授权会话。
func (s *AccountOAuthService) Start(ctx context.Context, input AccountOAuthStartInput) (AccountOAuthSessionResult, error) {
	if s == nil {
		return AccountOAuthSessionResult{}, errors.New("OAuth session service is nil")
	}
	mode := strings.ToLower(strings.TrimSpace(input.Mode))
	if mode == "" {
		mode = OAuthLoginModeBrowser
	}
	if mode != OAuthLoginModeBrowser && mode != OAuthLoginModeDevice {
		return AccountOAuthSessionResult{}, errors.New("mode must be browser or device")
	}
	pool, err := normalizeOpenAICodexPool(input.AccountPoolInput)
	if err != nil {
		return AccountOAuthSessionResult{}, err
	}
	httpClient, err := UpstreamHTTPClient()
	if err != nil {
		return AccountOAuthSessionResult{}, err
	}
	oauth := codexauth.NewOAuthClient(codexauth.WithHTTPClient(httpClient))
	expiresAt := time.Now().Add(accountOAuthSessionTTL)
	sessionCtx, cancel := context.WithDeadline(context.Background(), expiresAt)
	session := &accountOAuthSession{
		result: AccountOAuthSessionResult{
			ID: uuid.NewString(), Mode: mode, Status: OAuthSessionPending, ExpiresAt: expiresAt.UnixMilli(),
		},
		pool: pool, ctx: sessionCtx, cancel: cancel,
	}

	if mode == OAuthLoginModeBrowser {
		if err := s.ensureCallbackServer(); err != nil {
			cancel()
			return AccountOAuthSessionResult{}, fmt.Errorf("start OAuth callback listener on %s: %w; close the process using port 1455 or use device login", accountOAuthCallbackAddress, err)
		}
		pkce, err := codexauth.GeneratePKCE()
		if err != nil {
			cancel()
			return AccountOAuthSessionResult{}, err
		}
		state, err := codexauth.GenerateState()
		if err != nil {
			cancel()
			return AccountOAuthSessionResult{}, err
		}
		authorizationURL, err := oauth.AuthorizeURL(codexauth.AuthorizationRequest{
			RedirectURI: accountOAuthCallbackURI, CodeChallenge: pkce.CodeChallenge, State: state,
		})
		if err != nil {
			cancel()
			return AccountOAuthSessionResult{}, err
		}
		session.state = state
		session.codeVerifier = pkce.CodeVerifier
		session.result.AuthorizationURL = authorizationURL
		session.result.CallbackListening = true
	} else {
		requestCtx, requestCancel := contextWithOptionalTimeout(ctx, Config().RequestTimeout())
		device, err := oauth.StartDeviceAuthorization(requestCtx)
		requestCancel()
		if err != nil {
			cancel()
			return AccountOAuthSessionResult{}, err
		}
		session.device = device
		session.result.VerificationURL = device.VerificationURL
		session.result.UserCode = device.UserCode
		session.result.IntervalSeconds = device.IntervalSeconds
	}

	s.mu.Lock()
	s.cleanupLocked(time.Now())
	s.sessions[session.result.ID] = session
	if session.state != "" {
		s.states[session.state] = session.result.ID
	}
	result := session.result
	s.mu.Unlock()

	if mode == OAuthLoginModeDevice {
		go s.completeDevice(session.result.ID, oauth)
	}
	return result, nil
}

// Get 返回授权会话的最新非敏感状态。
func (s *AccountOAuthService) Get(id string) (AccountOAuthSessionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[strings.TrimSpace(id)]
	if !ok {
		return AccountOAuthSessionResult{}, errors.New("OAuth session not found")
	}
	s.expireLocked(session, time.Now())
	return session.result, nil
}

// CompleteBrowser 使用授权码或完整回调 URL 完成浏览器 OAuth 登录。
func (s *AccountOAuthService) CompleteBrowser(id string, input AccountOAuthCompleteInput) (AccountOAuthSessionResult, error) {
	code, state, callbackErr := parseAccountOAuthCallback(input)
	if callbackErr != nil {
		if err := s.validateBrowserState(id, state); err != nil {
			return AccountOAuthSessionResult{}, err
		}
		s.failByID(id, callbackErr)
		return AccountOAuthSessionResult{}, callbackErr
	}
	session, err := s.beginBrowserCompletion(id, state)
	if err != nil {
		return AccountOAuthSessionResult{}, err
	}
	httpClient, err := UpstreamHTTPClient()
	if err != nil {
		s.failByID(id, err)
		return AccountOAuthSessionResult{}, err
	}
	oauth := codexauth.NewOAuthClient(codexauth.WithHTTPClient(httpClient))
	tokens, err := oauth.ExchangeAuthorizationCode(session.ctx, code, accountOAuthCallbackURI, session.codeVerifier)
	if err != nil {
		s.failByID(id, err)
		return AccountOAuthSessionResult{}, err
	}
	if err := s.finishTokens(id, *tokens, "account.oauth.browser"); err != nil {
		return AccountOAuthSessionResult{}, err
	}
	return s.Get(id)
}

func (s *AccountOAuthService) validateBrowserState(id, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[strings.TrimSpace(id)]
	if !ok {
		return errors.New("OAuth session not found")
	}
	if session.result.Mode != OAuthLoginModeBrowser {
		return errors.New("OAuth session is not a browser login")
	}
	if strings.TrimSpace(state) == "" || state != session.state {
		return errors.New("OAuth callback state does not match")
	}
	return nil
}

// Cancel 取消仍在等待或完成中的 OAuth 登录会话。
func (s *AccountOAuthService) Cancel(id string) (AccountOAuthSessionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[strings.TrimSpace(id)]
	if !ok {
		return AccountOAuthSessionResult{}, errors.New("OAuth session not found")
	}
	if !isTerminalOAuthStatus(session.result.Status) {
		session.result.Status = OAuthSessionCancelled
		session.result.Error = "授权已取消"
		session.cancel()
	}
	return session.result, nil
}

func (s *AccountOAuthService) completeDevice(id string, oauth *codexauth.OAuthClient) {
	s.mu.Lock()
	session := s.sessions[id]
	if session == nil || session.device == nil {
		s.mu.Unlock()
		return
	}
	ctx := session.ctx
	device := *session.device
	s.mu.Unlock()

	tokens, err := oauth.CompleteDeviceAuthorization(ctx, device)
	if err != nil {
		s.failByID(id, err)
		return
	}
	if _, err := s.beginDeviceCompletion(id); err != nil {
		return
	}
	_ = s.finishTokens(id, *tokens, "account.oauth.device")
}

func (s *AccountOAuthService) beginBrowserCompletion(id, state string) (*accountOAuthSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[strings.TrimSpace(id)]
	if !ok {
		return nil, errors.New("OAuth session not found")
	}
	if session.result.Mode != OAuthLoginModeBrowser {
		return nil, errors.New("OAuth session is not a browser login")
	}
	if strings.TrimSpace(state) == "" || state != session.state {
		return nil, errors.New("OAuth callback state does not match")
	}
	if err := s.beginCompletionLocked(session); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *AccountOAuthService) beginDeviceCompletion(id string) (*accountOAuthSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return nil, errors.New("OAuth session not found")
	}
	if session.result.Mode != OAuthLoginModeDevice {
		return nil, errors.New("OAuth session is not a device login")
	}
	if err := s.beginCompletionLocked(session); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *AccountOAuthService) beginCompletionLocked(session *accountOAuthSession) error {
	s.expireLocked(session, time.Now())
	if session.result.Status != OAuthSessionPending {
		return fmt.Errorf("OAuth session is already %s", session.result.Status)
	}
	session.result.Status = OAuthSessionCompleting
	session.result.Error = ""
	return nil
}

func (s *AccountOAuthService) finishTokens(id string, tokens codexauth.TokenSet, auditAction string) error {
	file, err := codexauth.NewAccountFile(tokens, "")
	if err != nil {
		s.failByID(id, err)
		return err
	}
	s.mu.Lock()
	session := s.sessions[id]
	if session == nil {
		s.mu.Unlock()
		return errors.New("OAuth session not found")
	}
	pool := session.pool
	s.mu.Unlock()

	account, err := AccountServiceApp.upsertOfficialAccount(file, pool, auditAction)
	if err != nil {
		s.failByID(id, err)
		return err
	}
	s.mu.Lock()
	if current := s.sessions[id]; current != nil && current.result.Status == OAuthSessionCompleting {
		current.result.Status = OAuthSessionSuccess
		current.result.AccountGuid = account.Guid
		current.result.Error = ""
		current.cancel()
	}
	s.mu.Unlock()
	AccountServiceApp.SyncOfficialAccountAsync(account.Guid)
	return nil
}

func (s *AccountOAuthService) failByID(id string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[strings.TrimSpace(id)]
	if session == nil || isTerminalOAuthStatus(session.result.Status) {
		return
	}
	if errors.Is(err, context.Canceled) && session.result.Status == OAuthSessionCancelled {
		return
	}
	if errors.Is(err, context.DeadlineExceeded) || time.Now().UnixMilli() >= session.result.ExpiresAt {
		session.result.Status = OAuthSessionExpired
		session.result.Error = "授权会话已过期"
	} else {
		session.result.Status = OAuthSessionFailed
		session.result.Error = truncateError(err)
	}
	session.cancel()
}

func (s *AccountOAuthService) expireLocked(session *accountOAuthSession, now time.Time) {
	if session == nil || isTerminalOAuthStatus(session.result.Status) || now.UnixMilli() < session.result.ExpiresAt {
		return
	}
	session.result.Status = OAuthSessionExpired
	session.result.Error = "授权会话已过期"
	session.cancel()
}

func (s *AccountOAuthService) cleanupLocked(now time.Time) {
	for id, session := range s.sessions {
		s.expireLocked(session, now)
		if now.UnixMilli() > session.result.ExpiresAt+int64(time.Hour/time.Millisecond) {
			delete(s.states, session.state)
			delete(s.sessions, id)
		}
	}
}

func isTerminalOAuthStatus(status string) bool {
	return status == OAuthSessionSuccess || status == OAuthSessionFailed || status == OAuthSessionCancelled || status == OAuthSessionExpired
}

func parseAccountOAuthCallback(input AccountOAuthCompleteInput) (string, string, error) {
	code := strings.TrimSpace(input.Code)
	state := strings.TrimSpace(input.State)
	if rawURL := strings.TrimSpace(input.CallbackURL); rawURL != "" {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return "", state, fmt.Errorf("invalid callback URL: %w", err)
		}
		query := parsed.Query()
		code = strings.TrimSpace(query.Get("code"))
		state = strings.TrimSpace(query.Get("state"))
		if oauthError := strings.TrimSpace(query.Get("error")); oauthError != "" {
			description := strings.TrimSpace(query.Get("error_description"))
			if description == "" {
				description = oauthError
			}
			return "", state, errors.New(description)
		}
	}
	if code == "" || state == "" {
		return "", state, errors.New("OAuth callback must contain code and state")
	}
	return code, state, nil
}

func (s *AccountOAuthService) ensureCallbackServer() error {
	s.callbackMu.Lock()
	defer s.callbackMu.Unlock()
	if s.callbackStarted {
		return nil
	}
	listener, err := net.Listen("tcp", accountOAuthCallbackAddress)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", s.handleBrowserCallback)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	s.callbackStarted = true
	go func() { _ = server.Serve(listener) }()
	return nil
}

func (s *AccountOAuthService) handleBrowserCallback(writer http.ResponseWriter, request *http.Request) {
	state := strings.TrimSpace(request.URL.Query().Get("state"))
	s.mu.Lock()
	id := s.states[state]
	s.mu.Unlock()
	if id == "" {
		writeAccountOAuthCallbackPage(writer, false)
		return
	}
	_, err := s.CompleteBrowser(id, AccountOAuthCompleteInput{CallbackURL: accountOAuthCallbackURI + "?" + request.URL.RawQuery})
	writeAccountOAuthCallbackPage(writer, err == nil)
}

var accountOAuthCallbackPage = template.Must(template.New("oauth-callback").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>FreeAI 官方账号授权</title><style>body{margin:0;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#f5f7fa;color:#1f2937}main{max-width:520px;margin:14vh auto;padding:32px}h1{font-size:24px;margin:0 0 12px}p{line-height:1.7;color:#526174}</style></head>
<body><main><h1>{{if .}}授权成功{{else}}授权未完成{{end}}</h1><p>{{if .}}账号已安全写入 FreeAI，可以关闭此页面并返回账号列表。{{else}}回调无效或授权失败，请返回 FreeAI 查看具体状态后重试。{{end}}</p></main></body></html>`))

func writeAccountOAuthCallbackPage(writer http.ResponseWriter, success bool) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	writer.WriteHeader(http.StatusOK)
	_ = accountOAuthCallbackPage.Execute(writer, success)
}
