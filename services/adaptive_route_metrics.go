package services

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wfu-work/free-ai-go/domains"
)

const (
	routeLatencyHalfLife  = 15 * time.Minute
	routeOverloadHalfLife = 10 * time.Minute
	routeLatencyAlpha     = 0.25
	routeOverloadAlpha    = 0.30
	defaultRouteLatencyMs = 2000.0
)

type accountRouteMetric struct {
	InFlight            int
	FirstResponseEWMAMs float64
	LatencyUpdatedAt    time.Time
	OverloadEWMA        float64
	OverloadUpdatedAt   time.Time
	OverloadObserved    bool
}

type accountRouteMetricSnapshot struct {
	InFlight        int
	FirstResponseMs float64
	OverloadRate    float64
}

type adaptiveRouteMetricStore struct {
	sync.Mutex
	accounts map[string]*accountRouteMetric
}

func newAdaptiveRouteMetricStore() *adaptiveRouteMetricStore {
	return &adaptiveRouteMetricStore{accounts: make(map[string]*accountRouteMetric)}
}

var adaptiveRouteMetrics = newAdaptiveRouteMetricStore()

// pickAndAcquire uses dynamic effective weights and reserves one in-flight slot
// on the selected account before returning. This makes concurrent selections
// observe one another instead of stampeding the same currently-fast account.
func (s *adaptiveRouteMetricStore) pickAndAcquire(accounts []domains.Account, cursor int, now time.Time) domains.Account {
	if len(accounts) == 0 {
		return domains.Account{}
	}
	s.Lock()
	defer s.Unlock()

	snapshots := s.snapshotsLocked(accounts, now)
	weights := adaptiveRouteWeights(accounts, snapshots)
	total := 0
	for _, weight := range weights {
		total += weight
	}
	if total <= 0 {
		total = len(accounts)
		for i := range weights {
			weights[i] = 1
		}
	}

	offset := int(mixRouteCursor(uint64(max(cursor, 0))) % uint64(total))
	selected := accounts[len(accounts)-1]
	for i, weight := range weights {
		if offset < weight {
			selected = accounts[i]
			break
		}
		offset -= weight
	}
	metric := s.metricLocked(selected.Guid)
	metric.InFlight++
	return selected
}

func (s *adaptiveRouteMetricStore) observe(accountGuid string, releaseInFlight bool, result *ProxyResult, err error, now time.Time) {
	if strings.TrimSpace(accountGuid) == "" {
		return
	}
	s.Lock()
	defer s.Unlock()
	metric := s.metricLocked(accountGuid)
	if releaseInFlight && metric.InFlight > 0 {
		metric.InFlight--
	}

	if firstResponseMs := routeFirstResponseMs(result); firstResponseMs > 0 {
		sample := float64(firstResponseMs)
		if metric.FirstResponseEWMAMs <= 0 {
			metric.FirstResponseEWMAMs = sample
		} else {
			metric.FirstResponseEWMAMs = metric.FirstResponseEWMAMs*(1-routeLatencyAlpha) + sample*routeLatencyAlpha
		}
		metric.LatencyUpdatedAt = now
	}

	if routeOutcomeObserved(result, err) {
		outcome := 0.0
		if isRouteOverload(result, err) {
			outcome = 1
		}
		current := decayedMetric(metric.OverloadEWMA, metric.OverloadUpdatedAt, routeOverloadHalfLife, now)
		if !metric.OverloadObserved {
			metric.OverloadEWMA = outcome
			metric.OverloadObserved = true
		} else {
			metric.OverloadEWMA = current*(1-routeOverloadAlpha) + outcome*routeOverloadAlpha
		}
		metric.OverloadUpdatedAt = now
	}
}

func (s *adaptiveRouteMetricStore) snapshotsLocked(accounts []domains.Account, now time.Time) map[string]accountRouteMetricSnapshot {
	snapshots := make(map[string]accountRouteMetricSnapshot, len(accounts))
	for _, account := range accounts {
		metric := s.accounts[account.Guid]
		if metric == nil {
			continue
		}
		latency := metric.FirstResponseEWMAMs
		if latency > 0 && !metric.LatencyUpdatedAt.IsZero() {
			confidence := metricDecay(metric.LatencyUpdatedAt, routeLatencyHalfLife, now)
			latency = defaultRouteLatencyMs + (latency-defaultRouteLatencyMs)*confidence
		}
		snapshots[account.Guid] = accountRouteMetricSnapshot{
			InFlight:        metric.InFlight,
			FirstResponseMs: latency,
			OverloadRate:    decayedMetric(metric.OverloadEWMA, metric.OverloadUpdatedAt, routeOverloadHalfLife, now),
		}
	}
	return snapshots
}

func (s *adaptiveRouteMetricStore) metricLocked(accountGuid string) *accountRouteMetric {
	metric := s.accounts[accountGuid]
	if metric == nil {
		metric = &accountRouteMetric{}
		s.accounts[accountGuid] = metric
	}
	return metric
}

// adaptiveRouteWeights keeps configured account weight as the capacity base,
// then adjusts it by relative first-response latency, recent overloads, and
// current concurrency. A minimum exploration weight lets recovered accounts
// re-enter traffic instead of being permanently starved.
func adaptiveRouteWeights(accounts []domains.Account, snapshots map[string]accountRouteMetricSnapshot) []int {
	baselineLatency := adaptiveLatencyBaseline(accounts, snapshots)
	weights := make([]int, len(accounts))
	for i, account := range accounts {
		baseWeight := account.Weight
		if baseWeight <= 0 {
			baseWeight = 1
		}
		if baseWeight > 1000 {
			baseWeight = 1000
		}
		snapshot := snapshots[account.Guid]
		latencyFactor := 1.0
		if snapshot.FirstResponseMs > 0 {
			latencyFactor = math.Sqrt(baselineLatency / snapshot.FirstResponseMs)
			latencyFactor = math.Max(0.5, math.Min(1.5, latencyFactor))
		}
		overloadRate := math.Max(0, math.Min(1, snapshot.OverloadRate))
		overloadFactor := math.Max(0.1, 1-0.9*overloadRate)
		concurrencyFactor := 1 / float64(1+max(snapshot.InFlight, 0))
		effective := float64(baseWeight) * latencyFactor * overloadFactor * concurrencyFactor
		weights[i] = max(1, int(math.Round(effective*100)))
	}
	return weights
}

func adaptiveLatencyBaseline(accounts []domains.Account, snapshots map[string]accountRouteMetricSnapshot) float64 {
	latencies := make([]float64, 0, len(accounts))
	for _, account := range accounts {
		latency := snapshots[account.Guid].FirstResponseMs
		if latency <= 0 {
			latency = defaultRouteLatencyMs
		}
		latencies = append(latencies, latency)
	}
	if len(latencies) == 0 {
		return defaultRouteLatencyMs
	}
	sort.Float64s(latencies)
	middle := len(latencies) / 2
	if len(latencies)%2 == 0 {
		return (latencies[middle-1] + latencies[middle]) / 2
	}
	return latencies[middle]
}

func routeFirstResponseMs(result *ProxyResult) int64 {
	if result == nil {
		return 0
	}
	// Prefer the first content token because response.created may arrive quickly
	// while the user-visible answer still stalls. Error streams have no content
	// token and naturally fall back to their first SSE event.
	if result.FirstTokenMs > 0 {
		return result.FirstTokenMs
	}
	if result.FirstEventMs > 0 {
		return result.FirstEventMs
	}
	if result.UpstreamHeaderMs > 0 {
		return result.UpstreamHeaderMs
	}
	if result.ErrorType == domains.ErrorUpstreamTimeout && result.LatencyMs > 0 {
		return result.LatencyMs
	}
	return 0
}

func isRouteOverload(result *ProxyResult, err error) bool {
	parts := make([]string, 0, 3)
	if result != nil {
		parts = append(parts, result.ErrorType, result.ErrorSummary)
		if result.StatusCode == 503 {
			return true
		}
	}
	if err != nil {
		parts = append(parts, err.Error())
	}
	text := strings.ToLower(strings.Join(parts, " "))
	return strings.Contains(text, "server_is_overloaded") ||
		strings.Contains(text, "service_unavailable_error") ||
		strings.Contains(text, "servers are currently overloaded")
}

func routeOutcomeObserved(result *ProxyResult, err error) bool {
	if result != nil && result.ErrorType == domains.ErrorClientDisconnected {
		return false
	}
	return result != nil || err != nil
}

func decayedMetric(value float64, updatedAt time.Time, halfLife time.Duration, now time.Time) float64 {
	if value == 0 || updatedAt.IsZero() {
		return value
	}
	return value * metricDecay(updatedAt, halfLife, now)
}

func metricDecay(updatedAt time.Time, halfLife time.Duration, now time.Time) float64 {
	if updatedAt.IsZero() || halfLife <= 0 || !now.After(updatedAt) {
		return 1
	}
	return math.Exp(-math.Ln2 * float64(now.Sub(updatedAt)) / float64(halfLife))
}

func mixRouteCursor(value uint64) uint64 {
	value += 0x9e3779b97f4a7c15
	value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
	value = (value ^ (value >> 27)) * 0x94d049bb133111eb
	return value ^ (value >> 31)
}
