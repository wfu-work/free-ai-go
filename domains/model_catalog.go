package domains

import common "github.com/wfu-work/nav-common-go-lib/domains"

// ModelCatalog 保存官方账号远端返回的结构化模型目录。
type ModelCatalog struct {
	common.BaseDataEntity
	VendorCode       string `json:"vendorCode" gorm:"size:40;uniqueIndex:idx_model_catalog_identity,priority:1;index;comment:官方厂商标识"`
	ProductCode      string `json:"productCode" gorm:"size:40;uniqueIndex:idx_model_catalog_identity,priority:2;index;comment:官方产品标识"`
	UpstreamProtocol string `json:"upstreamProtocol" gorm:"size:60;index;comment:上游调用协议"`
	RemoteModelID    string `json:"remoteModelId" gorm:"size:160;uniqueIndex:idx_model_catalog_identity,priority:3;index;comment:官方远端模型ID"`
	DisplayName      string `json:"displayName" gorm:"size:200;comment:官方显示名称"`
	Description      string `json:"description" gorm:"comment:官方模型描述"`
	OwnedBy          string `json:"ownedBy" gorm:"size:100;comment:官方所有者"`
	CapabilitiesJSON string `json:"capabilitiesJson" gorm:"comment:模型能力JSON"`
	RawMetadataJSON  string `json:"-" gorm:"comment:官方原始模型元数据"`
	Source           string `json:"source" gorm:"size:40;index;comment:模型来源"`
	RemoteCreatedAt  int64  `json:"remoteCreatedAt" gorm:"comment:官方模型创建时间"`
	FirstSeenAt      int64  `json:"firstSeenAt" gorm:"index;comment:首次发现时间"`
	LastSeenAt       int64  `json:"lastSeenAt" gorm:"index;comment:最近发现时间"`
	Deprecated       bool   `json:"deprecated" gorm:"index;comment:是否已长期失效"`
}

func (ModelCatalog) TableName() string { return "fmg_model_catalog" }

// AccountModelAvailability 记录账号对官方模型的真实可用性。
type AccountModelAvailability struct {
	common.BaseDataEntity
	AccountGuid      string `json:"accountGuid" gorm:"size:50;uniqueIndex:idx_account_model_identity,priority:1;index;comment:账号GUID"`
	ModelCatalogGuid string `json:"modelCatalogGuid" gorm:"size:50;uniqueIndex:idx_account_model_identity,priority:2;index;comment:模型目录GUID"`
	Available        bool   `json:"available" gorm:"index;comment:最近同步是否可用"`
	FirstSeenAt      int64  `json:"firstSeenAt" gorm:"comment:首次发现时间"`
	LastSeenAt       int64  `json:"lastSeenAt" gorm:"index;comment:最近发现时间"`
	LastError        string `json:"lastError,omitempty" gorm:"comment:最近同步错误"`
	RawMetadataJSON  string `json:"-" gorm:"comment:该账号返回的模型原始元数据"`
}

func (AccountModelAvailability) TableName() string { return "fmg_account_model" }

// ModelExposure 保存官方模型对外暴露为 OpenAI-compatible 模型的策略。
type ModelExposure struct {
	common.BaseDataEntity
	ModelCatalogGuid string `json:"modelCatalogGuid" gorm:"size:50;uniqueIndex;index;comment:模型目录GUID"`
	PublicModel      string `json:"publicModel" gorm:"size:160;uniqueIndex;comment:对外模型ID"`
	Aliases          string `json:"aliases" gorm:"comment:对外模型别名JSON"`
	AccountGroup     string `json:"accountGroup" gorm:"size:80;index;comment:可选账号组限制"`
	TimeoutSec       int    `json:"timeoutSec" gorm:"comment:请求超时秒数"`
	Enabled          bool   `json:"enabled" gorm:"index;comment:是否允许参与路由"`
	Visible          bool   `json:"visible" gorm:"index;comment:是否通过模型列表对外可见"`
}

func (ModelExposure) TableName() string { return "fmg_model_exposure" }

// ModelPricing 保存官方厂商公开的模型 API 参考价。
//
// 价格与账号订阅额度是两套独立数据：本表只用于成本估算，不参与 Codex 订阅额度扣减。
type ModelPricing struct {
	common.BaseDataEntity
	VendorCode               string `json:"vendorCode" gorm:"size:40;uniqueIndex:idx_model_pricing_identity,priority:1;index;comment:官方厂商标识"`
	RemoteModelID            string `json:"remoteModelId" gorm:"size:160;uniqueIndex:idx_model_pricing_identity,priority:2;index;comment:官方远端模型ID"`
	Scope                    string `json:"scope" gorm:"size:40;uniqueIndex:idx_model_pricing_identity,priority:3;index;comment:价格适用范围"`
	ServiceTier              string `json:"serviceTier" gorm:"size:40;uniqueIndex:idx_model_pricing_identity,priority:4;index;comment:服务层级"`
	ContextTier              string `json:"contextTier" gorm:"size:40;uniqueIndex:idx_model_pricing_identity,priority:5;index;comment:上下文价格区间"`
	Currency                 string `json:"currency" gorm:"size:12;comment:币种"`
	Unit                     string `json:"unit" gorm:"size:40;comment:计价单位"`
	InputMicrousdPer1M       *int64 `json:"inputMicrousdPer1M,omitempty" gorm:"comment:每百万输入Token的微美元价格"`
	CachedInputMicrousdPer1M *int64 `json:"cachedInputMicrousdPer1M,omitempty" gorm:"comment:每百万缓存输入Token的微美元价格"`
	CacheWriteMicrousdPer1M  *int64 `json:"cacheWriteMicrousdPer1M,omitempty" gorm:"comment:每百万缓存写入Token的微美元价格"`
	OutputMicrousdPer1M      *int64 `json:"outputMicrousdPer1M,omitempty" gorm:"comment:每百万输出Token的微美元价格"`
	SourceURL                string `json:"sourceUrl" gorm:"size:500;comment:官方定价来源地址"`
	SourceKind               string `json:"sourceKind" gorm:"size:50;index;comment:实时官方文档或内置官方快照"`
	SourceVersion            string `json:"sourceVersion,omitempty" gorm:"size:50;comment:来源版本"`
	LastSyncedAt             int64  `json:"lastSyncedAt" gorm:"index;comment:最近同步时间"`
	Active                   bool   `json:"active" gorm:"index;comment:当前价格是否有效"`
}

// TableName 返回模型定价表名。
func (ModelPricing) TableName() string { return "fmg_model_pricing" }
