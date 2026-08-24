package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash/fnv"
	"net/http"
	"strings"

	"github.com/wfu-work/free-ai-go/domains"
)

const routeAffinityHeader = "X-FreeAI-Affinity-Key"

// requestRouteAffinityKey 只返回不可逆摘要，不记录或持久化客户端提供的会话标识。
func requestRouteAffinityKey(r *http.Request, body []byte, key domains.PlatformKey) string {
	identity := ""
	if r != nil {
		identity = prefixedAffinityValue("header", r.Header.Get(routeAffinityHeader))
	}
	if identity == "" {
		identity = requestBodyAffinityIdentity(body)
	}
	if identity == "" {
		identity = prefixedAffinityValue("platform-key", key.Guid)
	}
	if identity == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(key.Guid) + "\x00" + identity))
	return hex.EncodeToString(sum[:])
}

func requestBodyAffinityIdentity(body []byte) string {
	var payload map[string]any
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return ""
	}
	if conversation, ok := payload["conversation"].(string); ok {
		if value := prefixedAffinityValue("conversation", conversation); value != "" {
			return value
		}
	}
	if conversation, ok := payload["conversation"].(map[string]any); ok {
		if value := prefixedAffinityValue("conversation", stringMapValue(conversation, "id")); value != "" {
			return value
		}
	}
	for _, field := range []string{"conversation_id", "session_id", "prompt_cache_key"} {
		if value := prefixedAffinityValue(field, stringMapValue(payload, field)); value != "" {
			return value
		}
	}
	if metadata, ok := payload["metadata"].(map[string]any); ok {
		for _, field := range []string{"conversation_id", "session_id", "user_id"} {
			if value := prefixedAffinityValue("metadata."+field, stringMapValue(metadata, field)); value != "" {
				return value
			}
		}
	}
	return prefixedAffinityValue("user", stringMapValue(payload, "user"))
}

func stringMapValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func prefixedAffinityValue(prefix, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) > 512 {
		value = value[:512]
	}
	return prefix + ":" + value
}

// pickSessionAffinityAccount 使用 Rendezvous Hash。候选集顺序不影响结果，
// 某个账号退出候选集时，同一会话会稳定落到剩余账号中的下一高分账号。
func pickSessionAffinityAccount(model RoutedModel, accounts []domains.Account, affinityKey string) domains.Account {
	if len(accounts) == 0 {
		return domains.Account{}
	}
	namespace := model.AccountGroup + "\x00" + model.PublicModel + "\x00" + strings.TrimSpace(affinityKey)
	selected := domains.Account{}
	var selectedScore uint64
	for _, account := range accounts {
		if strings.TrimSpace(account.Guid) == "" {
			continue
		}
		score := sessionAffinityScore(namespace, account.Guid)
		if selected.Guid == "" || score > selectedScore || score == selectedScore && account.Guid < selected.Guid {
			selected = account
			selectedScore = score
		}
	}
	if selected.Guid == "" {
		return accounts[0]
	}
	return selected
}

func sessionAffinityScore(affinityKey, accountGuid string) uint64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(affinityKey))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(accountGuid))
	return hash.Sum64()
}
