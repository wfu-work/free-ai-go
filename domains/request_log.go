package domains

import common "github.com/wfu-work/nav-common-go-lib/domains"

type RequestLog struct {
	common.BaseDataEntity
	RequestID     string `json:"requestId" gorm:"size:80;uniqueIndex;comment:请求ID"`
	PlatformKeyID string `json:"platformKeyId" gorm:"size:50;index;comment:平台密钥"`
	AccountGuid   string `json:"accountGuid" gorm:"size:50;index;comment:命中账号"`
	Model         string `json:"model" gorm:"size:100;index;comment:请求模型"`
	UpstreamModel string `json:"upstreamModel" gorm:"size:100;comment:上游模型"`
	Provider      string `json:"provider" gorm:"size:40;index;comment:平台"`
	StatusCode    int    `json:"statusCode" gorm:"index;comment:状态码"`
	ErrorType     string `json:"errorType" gorm:"size:80;index;comment:错误类型"`
	Switched      bool   `json:"switched" gorm:"index;comment:是否切换"`
	SwitchCount   int    `json:"switchCount" gorm:"comment:切换次数"`
	SwitchReason  string `json:"switchReason" gorm:"comment:切换原因"`
	LatencyMs     int64  `json:"latencyMs" gorm:"comment:总耗时"`
	FirstTokenMs  int64  `json:"firstTokenMs" gorm:"comment:首Token耗时"`
	InputTokens   int64  `json:"inputTokens" gorm:"comment:输入Token"`
	OutputTokens  int64  `json:"outputTokens" gorm:"comment:输出Token"`
	CreatedAtUnix int64  `json:"createdAtUnix" gorm:"index;comment:创建时间"`
}

func (RequestLog) TableName() string { return "fmg_request_log" }
