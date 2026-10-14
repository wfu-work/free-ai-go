package services

import (
	"context"
	"crypto/sha256"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/wfu-work/free-ai-go/domains"
)

type networkContextKey uint8

const (
	networkCollectorKey networkContextKey = iota
	networkHTTPTraceKey
	networkAccountKey
	maxNetworkAttempts = 32
)

type requestNetworkCollector struct {
	mu      sync.Mutex
	ingress domains.RequestNetworkTrace
	traces  []*upstreamRequestTrace
}

func withRequestNetworkTrace(r *http.Request) *http.Request {
	if r.Context().Value(networkCollectorKey) != nil {
		return r
	}
	c := &requestNetworkCollector{ingress: inboundNetworkTrace(r)}
	return r.WithContext(context.WithValue(r.Context(), networkCollectorKey, c))
}

// RequestNetworkTrace also captures ingress-only requests rejected before routing.
func RequestNetworkTrace(r *http.Request) *domains.RequestNetworkTrace {
	if r == nil {
		return nil
	}
	if c, ok := r.Context().Value(networkCollectorKey).(*requestNetworkCollector); ok {
		c.mu.Lock()
		result := c.ingress
		traces := append([]*upstreamRequestTrace(nil), c.traces...)
		c.mu.Unlock()
		for _, trace := range traces {
			result.Attempts = append(result.Attempts, trace.networkSnapshot()...)
			if len(result.Attempts) >= maxNetworkAttempts {
				result.Attempts = result.Attempts[:maxNetworkAttempts]
				break
			}
		}
		return &result
	}
	result := inboundNetworkTrace(r)
	return &result
}

func inboundNetworkTrace(r *http.Request) domains.RequestNetworkTrace {
	result := domains.RequestNetworkTrace{PeerAddress: validSocketAddress(r.RemoteAddr)}
	if addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		result.GatewayAddress = validSocketAddress(addr.String())
	}
	// Bound and validate header claims; never promote them to the socket peer.
	for _, value := range strings.Split(boundedNetworkHeader(strings.Join(r.Header.Values("X-Forwarded-For"), ",")), ",") {
		if ip := networkIP(value); ip != "" && len(result.ForwardedFor) < 16 {
			result.ForwardedFor = append(result.ForwardedFor, ip)
		}
	}
	result.RealIP = networkIP(r.Header.Get("X-Real-IP"))
	for _, hop := range strings.Split(boundedNetworkHeader(strings.Join(r.Header.Values("Forwarded"), ",")), ",") {
		for _, pair := range strings.Split(hop, ";") {
			key, value, found := strings.Cut(strings.TrimSpace(pair), "=")
			if found && strings.EqualFold(key, "for") && len(result.Forwarded) < 16 {
				if ip := networkIP(strings.Trim(strings.TrimSpace(value), "\"")); ip != "" {
					result.Forwarded = append(result.Forwarded, ip)
				}
			}
		}
	}
	return result
}

func boundedNetworkHeader(value string) string {
	if len(value) > 4096 {
		return value[:4096]
	}
	return value
}

func networkIP(value string) string {
	value = strings.TrimSpace(value)
	if addr, err := netip.ParseAddr(value); err == nil {
		return addr.Unmap().String()
	}
	if addr, err := netip.ParseAddrPort(value); err == nil {
		return addr.Addr().Unmap().String()
	}
	if addr, err := netip.ParseAddr(strings.Trim(value, "[]")); err == nil {
		return addr.Unmap().String()
	}
	return ""
}

func validSocketAddress(value string) string {
	if addr, err := netip.ParseAddrPort(value); err == nil {
		return addr.String()
	}
	return networkIP(value)
}

// Called by the actual transport's Proxy selector for both direct and proxy
// requests, so a cached client cannot accidentally report today's config.
func recordNetworkRoute(r *http.Request, proxy *url.URL) {
	trace, ok := r.Context().Value(networkHTTPTraceKey).(*upstreamRequestTrace)
	if !ok {
		return
	}
	entry := domains.UpstreamNetworkAttempt{StartedAt: time.Now().UnixMilli(), Target: r.URL.Scheme + "://" + r.URL.Host}
	if account, ok := r.Context().Value(networkAccountKey).(domains.Account); ok {
		entry.AccountGuid, entry.AccountName = account.Guid, account.Name
	}
	proxyURL := ""
	if proxy != nil {
		entry.ProxyAddress = proxy.Scheme + "://" + proxy.Host // no userinfo, path or query
		proxyURL = proxy.String()
	}
	key := networkExitKey(proxyURL)
	trace.mu.Lock()
	if len(trace.network) < maxNetworkAttempts {
		trace.network = append(trace.network, entry)
		trace.exitKeys = append(trace.exitKeys, key)
	}
	trace.mu.Unlock()
	if c, ok := r.Context().Value(networkCollectorKey).(*requestNetworkCollector); ok {
		c.mu.Lock()
		found := false
		for _, existing := range c.traces {
			if existing == trace {
				found = true
				break
			}
		}
		if !found && len(c.traces) < maxNetworkAttempts {
			c.traces = append(c.traces, trace)
		}
		c.mu.Unlock()
	}
}

func (t *upstreamRequestTrace) networkSnapshot() []domains.UpstreamNetworkAttempt {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := append([]domains.UpstreamNetworkAttempt(nil), t.network...)
	for i := range result {
		result[i].ConnectAddresses = append([]string(nil), result[i].ConnectAddresses...)
		result[i].DNS = append([]domains.NetworkDNSLookup(nil), result[i].DNS...)
		for j := range result[i].DNS {
			result[i].DNS[j].Addresses = append([]string(nil), result[i].DNS[j].Addresses...)
		}
		observation := networkExitObservation(t.exitKeys[i])
		result[i].ExitIP, result[i].ExitIPCheckedAt = observation.ip, observation.checkedAt
	}
	return result
}

// Must be called with trace.mu held. Hooks update the active HTTP transaction.
func (t *upstreamRequestTrace) activeNetwork() *domains.UpstreamNetworkAttempt {
	if len(t.network) == 0 {
		return nil
	}
	return &t.network[len(t.network)-1]
}

func (t *upstreamRequestTrace) networkResult(result *ProxyResult, err error) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if entry := t.activeNetwork(); entry != nil {
		if result != nil {
			entry.StatusCode, entry.ErrorType = result.StatusCode, result.ErrorType
		}
		if entry.ErrorType == "" && err != nil {
			entry.ErrorType = classifyError(err)
		}
	}
}

func latestNetworkTrace(ctx context.Context) *upstreamRequestTrace {
	if c, ok := ctx.Value(networkCollectorKey).(*requestNetworkCollector); ok {
		c.mu.Lock()
		defer c.mu.Unlock()
		if len(c.traces) > 0 {
			return c.traces[len(c.traces)-1]
		}
	}
	return nil
}

func finishNetworkAttempt(ctx context.Context, previous *upstreamRequestTrace, result *ProxyResult, err error) {
	if latest := latestNetworkTrace(ctx); latest != nil && latest != previous {
		latest.networkResult(result, err)
	}
}

type exitIPObservation struct {
	ip        string
	checkedAt int64
	nextProbe time.Time
	pending   bool
}

var networkExitCache = struct {
	sync.Mutex
	entries map[[32]byte]exitIPObservation
}{entries: make(map[[32]byte]exitIPObservation)}

func networkExitKey(proxyURL string) [32]byte {
	if parsed, err := url.Parse(strings.TrimSpace(proxyURL)); err == nil {
		parsed.Scheme = strings.ToLower(parsed.Scheme)
		proxyURL = parsed.String()
	}
	return sha256.Sum256([]byte(proxyURL))
}

func networkExitObservation(key [32]byte) exitIPObservation {
	networkExitCache.Lock()
	defer networkExitCache.Unlock()
	entry := networkExitCache.entries[key]
	if time.Since(time.UnixMilli(entry.checkedAt)) > 10*time.Minute {
		return exitIPObservation{}
	}
	return entry
}

func rememberNetworkExitIP(proxyURL, ip string, checkedAt int64) {
	if networkIP(ip) == "" {
		return
	}
	networkExitCache.Lock()
	defer networkExitCache.Unlock()
	if len(networkExitCache.entries) >= 32 {
		clear(networkExitCache.entries)
	}
	networkExitCache.entries[networkExitKey(proxyURL)] = exitIPObservation{ip: networkIP(ip), checkedAt: checkedAt, nextProbe: time.Now().Add(10 * time.Minute)}
}

// Independent, bounded probe: it adds no wait to an OpenAI request. The result
// is explicitly an IP-check snapshot, not an observation made by OpenAI.
func scheduleNetworkExitProbe(proxyURL string) {
	key := networkExitKey(proxyURL)
	networkExitCache.Lock()
	entry := networkExitCache.entries[key]
	if entry.pending || time.Now().Before(entry.nextProbe) {
		networkExitCache.Unlock()
		return
	}
	if len(networkExitCache.entries) >= 32 {
		clear(networkExitCache.entries)
	}
	entry.pending, entry.nextProbe = true, time.Now().Add(time.Minute)
	networkExitCache.entries[key] = entry
	networkExitCache.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client, err := newUpstreamHTTPClient(proxyURL)
		if err == nil {
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			_, ip := probeUpstreamProxy(ctx, client, upstreamProxyProbe{name: "出口 IP", url: "https://api.ipify.org?format=json", exitIP: true})
			client.CloseIdleConnections()
			if ip != "" {
				rememberNetworkExitIP(proxyURL, ip, time.Now().UnixMilli())
			}
		}
		networkExitCache.Lock()
		entry := networkExitCache.entries[key]
		entry.pending = false
		networkExitCache.entries[key] = entry
		networkExitCache.Unlock()
	}()
}
