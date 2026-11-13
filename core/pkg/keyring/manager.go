package keyring

import (
	"fmt"

	"github.com/zalando/go-keyring"
)

const ServiceName = "ConduitOmniChat"

// KeyringManager maneja el almacenamiento seguro de credenciales usando el SO.
type KeyringManager struct {
	service string
}

// NewKeyringManager crea una nueva instancia de KeyringManager.
func NewKeyringManager() *KeyringManager {
	return &KeyringManager{
		service: ServiceName,
	}
}

// SetSecret guarda un secreto en el keyring del SO.
func (k *KeyringManager) SetSecret(key, secret string) error {
	if err := keyring.Set(k.service, key, secret); err != nil {
		return fmt.Errorf("error guardando secreto en keyring: %w", err)
	}
	return nil
}

// GetSecret obtiene un secreto desde el keyring del SO.
func (k *KeyringManager) GetSecret(key string) (string, error) {
	secret, err := keyring.Get(k.service, key)
	if err != nil {
		return "", fmt.Errorf("error obteniendo secreto de keyring: %w", err)
	}
	return secret, nil
}

// DeleteSecret elimina un secreto del keyring del SO.
func (k *KeyringManager) DeleteSecret(key string) error {
	if err := keyring.Delete(k.service, key); err != nil {
		return fmt.Errorf("error eliminando secreto de keyring: %w", err)
	}
	return nil
}
