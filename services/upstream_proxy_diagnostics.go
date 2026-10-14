package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type UpstreamProxyTestInput struct {
	UpstreamProxyURL              string                         `json:"upstreamProxyUrl"`
	UpstreamProxyCredentials      *UpstreamProxyCredentialsInput `json:"upstreamProxyCredentials,omitempty"`
	UpstreamProxyClearCredentials bool                           `json:"upstreamProxyClearCredentials,omitempty"`
}

type UpstreamProxyCheck struct {
	Name       string `json:"name"`
	Reachable  bool   `json:"reachable"`
	StatusCode int    `json:"statusCode,omitempty"`
	LatencyMs  int64  `json:"latencyMs"`
	Message    string `json:"message"`
}

type UpstreamProxyTestResult struct {
	ProxyAddress string               `json:"proxyAddress"`
	ExitIP       string               `json:"exitIp,omitempty"`
	CheckedAt    int64                `json:"checkedAt"`
	Checks       []UpstreamProxyCheck `json:"checks"`
}

type upstreamProxyProbe struct {
	name   string
	url    string
	exitIP bool
}

// TestUpstreamProxy uses an isolated client, without saving config or sending
// account credentials. All destinations are fixed rather than user-supplied.
func TestUpstreamProxy(ctx context.Context, input UpstreamProxyTestInput) (UpstreamProxyTestResult, error) {
	proxyURL, err := resolveProxyCredentials(input.UpstreamProxyURL, input.UpstreamProxyCredentials, input.UpstreamProxyClearCredentials, Config())
	if err != nil {
		return UpstreamProxyTestResult{}, err
	}
	return testUpstreamProxy(ctx, proxyURL, []upstreamProxyProbe{
		{name: "出口 IP", url: "https://api.ipify.org?format=json", exitIP: true},
		{name: "ChatGPT / Codex", url: "https://chatgpt.com/backend-api/codex/models"},
		{name: "OpenAI API", url: "https://api.openai.com/v1/models"},
		{name: "OpenAI 认证", url: "https://auth.openai.com/.well-known/openid-configuration"},
	})
}

func testUpstreamProxy(ctx context.Context, proxyURL string, probes []upstreamProxyProbe) (UpstreamProxyTestResult, error) {
	if proxyURL == "" {
		return UpstreamProxyTestResult{}, errors.New("请填写代理地址后再测试")
	}
	client, err := newUpstreamHTTPClient(proxyURL)
	if err != nil {
		return UpstreamProxyTestResult{}, err
	}
	defer client.CloseIdleConnections()
	// Report the configured endpoint's response, including redirects and WAF
	// restrictions; do not follow a redirect to an unrelated destination.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	parsed, _ := url.Parse(proxyURL)
	parsed.User = nil
	result := UpstreamProxyTestResult{
		ProxyAddress: parsed.String(), CheckedAt: time.Now().UnixMilli(),
		Checks: make([]UpstreamProxyCheck, len(probes)),
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i, probe := range probes {
		wg.Go(func() {
			check, exitIP := probeUpstreamProxy(ctx, client, probe)
			result.Checks[i] = check
			if probe.exitIP {
				result.ExitIP = exitIP
			}
		})
	}
	wg.Wait()
	return result, nil
}

func probeUpstreamProxy(ctx context.Context, client *http.Client, probe upstreamProxyProbe) (UpstreamProxyCheck, string) {
	started := time.Now()
	check := UpstreamProxyCheck{Name: probe.name}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, probe.url, nil)
	if err != nil {
		check.Message = "检测目标地址无效"
		return check, ""
	}
	response, err := client.Do(request)
	check.LatencyMs = time.Since(started).Milliseconds()
	if err != nil {
		check.Message = "代理连接失败，请检查地址、认证和网络"
		var netErr net.Error
		if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &netErr) && netErr.Timeout() {
			check.Message = "代理连接超时"
		} else if errors.Is(err, context.Canceled) {
			check.Message = "检测已取消"
		}
		return check, ""
	}
	defer response.Body.Close()
	check.StatusCode = response.StatusCode
	if probe.exitIP {
		var body struct {
			IP string `json:"ip"`
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, 4097))
		check.LatencyMs = time.Since(started).Milliseconds()
		if response.StatusCode != http.StatusOK || readErr != nil || len(data) > 4096 || json.Unmarshal(data, &body) != nil || net.ParseIP(strings.TrimSpace(body.IP)) == nil {
			check.Message = "无法获取出口 IP；请检查代理和 IP 检测服务"
			return check, ""
		}
		check.Reachable, check.Message = true, "已获取出口 IP，请与购买的静态 IP 核对"
		return check, net.ParseIP(strings.TrimSpace(body.IP)).String()
	}
	check.Reachable = response.StatusCode >= 200 && response.StatusCode < 400 || response.StatusCode == http.StatusUnauthorized
	if check.Reachable {
		check.Message = "连接成功；未验证账号权限"
	} else if response.StatusCode == http.StatusForbidden {
		check.Message = "目标拒绝访问，请检查地区或访问限制"
	} else if response.StatusCode == http.StatusProxyAuthRequired {
		check.Message = "代理认证失败，请检查用户名和密码"
	} else {
		check.Message = "目标返回 HTTP " + strconv.Itoa(response.StatusCode) + "，请检查代理或上游状态"
	}
	return check, ""
}
