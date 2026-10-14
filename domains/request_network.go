package domains

// RequestNetworkTrace 保存请求发生时的连接快照，不用当前配置补写历史日志。
// ForwardedFor / RealIP / Forwarded 仅是请求头声明，不代表受信任的来源。
type RequestNetworkTrace struct {
	PeerAddress    string                   `json:"peerAddress,omitempty"`
	GatewayAddress string                   `json:"gatewayAddress,omitempty"`
	ForwardedFor   []string                 `json:"forwardedFor,omitempty"`
	RealIP         string                   `json:"realIp,omitempty"`
	Forwarded      []string                 `json:"forwarded,omitempty"`
	Attempts       []UpstreamNetworkAttempt `json:"attempts,omitempty"`
}

type NetworkDNSLookup struct {
	Host      string   `json:"host"`
	Addresses []string `json:"addresses,omitempty"`
}

type UpstreamNetworkAttempt struct {
	StartedAt        int64              `json:"startedAt"`
	AccountGuid      string             `json:"accountGuid,omitempty"`
	AccountName      string             `json:"accountName,omitempty"`
	Target           string             `json:"target"`
	ProxyAddress     string             `json:"proxyAddress,omitempty"`
	LocalAddress     string             `json:"localAddress,omitempty"`
	RemoteAddress    string             `json:"remoteAddress,omitempty"`
	ConnectAddresses []string           `json:"connectAddresses,omitempty"`
	DNS              []NetworkDNSLookup `json:"dns,omitempty"`
	TLSServerName    string             `json:"tlsServerName,omitempty"`
	ConnectionReused bool               `json:"connectionReused"`
	StatusCode       int                `json:"statusCode,omitempty"`
	ErrorType        string             `json:"errorType,omitempty"`
	ExitIP           string             `json:"exitIp,omitempty"`
	ExitIPCheckedAt  int64              `json:"exitIpCheckedAt,omitempty"`
}
