package services

import (
	"errors"
	"time"

	"freeai/domains"

	"github.com/wfu-work/nav-common-go-lib/global"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const CoreBackupVersion = "freeai-core-backup/v1"

type BackupService struct{}

var BackupServiceApp = BackupService{}

type CoreBackupAccount struct {
	domains.Account
	EncryptedSecret string `json:"encryptedSecret,omitempty"`
}

type CoreBackupPlatformKey struct {
	domains.PlatformKey
	KeyHash      string `json:"keyHash,omitempty"`
	EncryptedKey string `json:"encryptedKey,omitempty"`
}

type CoreBackupPayload struct {
	Version       string                  `json:"version"`
	ExportedAt    int64                   `json:"exportedAt"`
	GatewayConfig GatewayProxyConfigInput `json:"gatewayConfig"`
	Data          CoreBackupData          `json:"data"`
}

type CoreBackupData struct {
	Accounts      []CoreBackupAccount     `json:"accounts"`
	AccountGroups []domains.AccountGroup  `json:"accountGroups"`
	AccountQuotas []domains.AccountQuota  `json:"accountQuotas"`
	ModelMappings []domains.ModelMapping  `json:"modelMappings"`
	PlatformKeys  []CoreBackupPlatformKey `json:"platformKeys"`
	RouteStates   []domains.RouteState    `json:"routeStates"`
}

type CoreBackupImportResult struct {
	Accounts      int `json:"accounts"`
	AccountGroups int `json:"accountGroups"`
	AccountQuotas int `json:"accountQuotas"`
	ModelMappings int `json:"modelMappings"`
	PlatformKeys  int `json:"platformKeys"`
	RouteStates   int `json:"routeStates"`
}

func (s BackupService) ExportCore() (CoreBackupPayload, error) {
	payload := CoreBackupPayload{
		Version:       CoreBackupVersion,
		ExportedAt:    time.Now().UnixMilli(),
		GatewayConfig: GatewayProxyConfig(),
	}
	if err := global.NAV_DB.Order("id asc").Find(&payload.Data.AccountGroups).Error; err != nil {
		return payload, err
	}
	var accounts []domains.Account
	if err := global.NAV_DB.Order("id asc").Find(&accounts).Error; err != nil {
		return payload, err
	}
	payload.Data.Accounts = make([]CoreBackupAccount, 0, len(accounts))
	for _, account := range accounts {
		payload.Data.Accounts = append(payload.Data.Accounts, CoreBackupAccount{
			Account:         account,
			EncryptedSecret: account.EncryptedSecret,
		})
	}
	if err := global.NAV_DB.Order("id asc").Find(&payload.Data.AccountQuotas).Error; err != nil {
		return payload, err
	}
	if err := global.NAV_DB.Order("id asc").Find(&payload.Data.ModelMappings).Error; err != nil {
		return payload, err
	}
	var platformKeys []domains.PlatformKey
	if err := global.NAV_DB.Order("id asc").Find(&platformKeys).Error; err != nil {
		return payload, err
	}
	payload.Data.PlatformKeys = make([]CoreBackupPlatformKey, 0, len(platformKeys))
	for _, key := range platformKeys {
		payload.Data.PlatformKeys = append(payload.Data.PlatformKeys, CoreBackupPlatformKey{
			PlatformKey:  key,
			KeyHash:      key.KeyHash,
			EncryptedKey: key.EncryptedKey,
		})
	}
	if err := global.NAV_DB.Order("id asc").Find(&payload.Data.RouteStates).Error; err != nil {
		return payload, err
	}
	return payload, nil
}

func (s BackupService) ImportCore(payload CoreBackupPayload) (CoreBackupImportResult, error) {
	if payload.Version != CoreBackupVersion {
		return CoreBackupImportResult{}, errors.New("unsupported backup file version")
	}
	result := CoreBackupImportResult{}
	err := global.NAV_DB.Transaction(func(tx *gorm.DB) error {
		if err := upsertByGuid(tx, payload.Data.AccountGroups); err != nil {
			return err
		}
		result.AccountGroups = len(payload.Data.AccountGroups)

		accounts := make([]domains.Account, 0, len(payload.Data.Accounts))
		for _, item := range payload.Data.Accounts {
			account := item.Account
			account.EncryptedSecret = item.EncryptedSecret
			accounts = append(accounts, account)
		}
		if err := upsertByGuid(tx, accounts); err != nil {
			return err
		}
		result.Accounts = len(accounts)

		if err := upsertByGuid(tx, payload.Data.AccountQuotas); err != nil {
			return err
		}
		result.AccountQuotas = len(payload.Data.AccountQuotas)

		if err := upsertByGuid(tx, payload.Data.ModelMappings); err != nil {
			return err
		}
		result.ModelMappings = len(payload.Data.ModelMappings)

		platformKeys := make([]domains.PlatformKey, 0, len(payload.Data.PlatformKeys))
		for _, item := range payload.Data.PlatformKeys {
			key := item.PlatformKey
			key.KeyHash = item.KeyHash
			key.EncryptedKey = item.EncryptedKey
			platformKeys = append(platformKeys, key)
		}
		if err := upsertByGuid(tx, platformKeys); err != nil {
			return err
		}
		result.PlatformKeys = len(platformKeys)

		if err := upsertByGuid(tx, payload.Data.RouteStates); err != nil {
			return err
		}
		result.RouteStates = len(payload.Data.RouteStates)

		return nil
	})
	if err != nil {
		return CoreBackupImportResult{}, err
	}
	if _, err := UpdateGatewayProxyConfig(payload.GatewayConfig); err != nil {
		return CoreBackupImportResult{}, err
	}
	return result, nil
}

func upsertByGuid[T any](tx *gorm.DB, items []T) error {
	if len(items) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "guid"}},
		UpdateAll: true,
	}).Create(&items).Error
}
