// Package auth stores provider API keys in the operating system keyring.
package auth

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

// Service is the keyring service name used by Docty.
const Service = "docty"

// ErrNotFound is returned when there is no stored key.
var ErrNotFound = keyring.ErrNotFound

// Store is the subset of the keyring API used by Docty. It exists so tests can
// use an in-memory implementation.
type Store interface {
	Set(service, user, password string) error
	Get(service, user string) (string, error)
	Delete(service, user string) error
}

type keyringStore struct{}

func (keyringStore) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}

func (keyringStore) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (keyringStore) Delete(service, user string) error {
	return keyring.Delete(service, user)
}

var (
	storeMu sync.RWMutex
	store   Store = keyringStore{}
)

// SetStore replaces the keyring implementation and returns a function that
// restores the previous one. It is intended for tests.
func SetStore(s Store) func() {
	storeMu.Lock()
	old := store
	store = s
	storeMu.Unlock()
	return func() {
		storeMu.Lock()
		store = old
		storeMu.Unlock()
	}
}

func currentStore() Store {
	storeMu.RLock()
	defer storeMu.RUnlock()
	return store
}

// Account returns the keyring user name for a provider. For generic
// OpenAI-compatible endpoints the base URL is part of the account so that
// different endpoints can keep different keys.
func Account(provider, baseURL string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	if p == "" {
		return ""
	}
	if p == "openai-compatible" {
		u := strings.TrimRight(strings.TrimSpace(baseURL), "/")
		if u != "" {
			return "api-key:" + p + ":" + u
		}
	}
	return "api-key:" + p
}

// SetKey stores key for provider. baseURL is only used for generic
// OpenAI-compatible providers.
func SetKey(provider, baseURL, key string) error {
	account := Account(provider, baseURL)
	if account == "" {
		return errors.New("el proveedor está vacío")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("la API key está vacía")
	}
	if err := currentStore().Set(Service, account, key); err != nil {
		return fmt.Errorf("keyring: %w", err)
	}
	return nil
}

// GetKey returns the stored key for provider. baseURL is only used for
// generic OpenAI-compatible providers.
func GetKey(provider, baseURL string) (string, error) {
	account := Account(provider, baseURL)
	if account == "" {
		return "", ErrNotFound
	}
	key, err := currentStore().Get(Service, account)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(key), nil
}

// DeleteKey removes the stored key for provider.
func DeleteKey(provider, baseURL string) error {
	account := Account(provider, baseURL)
	if account == "" {
		return ErrNotFound
	}
	return currentStore().Delete(Service, account)
}

// MemoryStore is an in-memory Store for tests.
type MemoryStore struct {
	mu   sync.Mutex
	data map[string]string
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: map[string]string{}}
}

func (m *MemoryStore) Set(service, user, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[service+"\x00"+user] = password
	return nil
}

func (m *MemoryStore) Get(service, user string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[service+"\x00"+user]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (m *MemoryStore) Delete(service, user string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := service + "\x00" + user
	if _, ok := m.data[k]; !ok {
		return ErrNotFound
	}
	delete(m.data, k)
	return nil
}
