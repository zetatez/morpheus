package configstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type ModelConfig struct {
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	APIKey    string    `json:"api_key"`
	Endpoint  string    `json:"endpoint,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type StoreData struct {
	Current   string                 `json:"current"`
	Providers map[string]ModelConfig `json:"providers"`
}

type Store struct {
	dir      string
	dataPath string
	keyPath  string
}

func DefaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "."
	}
	return filepath.Join(home, ".config", "morph")
}

func NewStore(dir string) *Store {
	if dir == "" {
		dir = DefaultConfigDir()
	}
	return &Store{
		dir:      dir,
		dataPath: filepath.Join(dir, "models.enc"),
		keyPath:  filepath.Join(dir, "models.key"),
	}
}

func (s *Store) Load() (StoreData, error) {
	if _, err := os.Stat(s.dataPath); err != nil {
		if os.IsNotExist(err) {
			return StoreData{Providers: map[string]ModelConfig{}}, nil
		}
		return StoreData{}, err
	}

	key, err := s.readKey(false)
	if err != nil {
		return StoreData{}, err
	}

	encrypted, err := os.ReadFile(s.dataPath)
	if err != nil {
		return StoreData{}, err
	}
	if len(encrypted) == 0 {
		return StoreData{Providers: map[string]ModelConfig{}}, nil
	}

	payload, err := decryptPayload(key, encrypted)
	if err != nil {
		return StoreData{}, err
	}

	var data StoreData
	if err := json.Unmarshal(payload, &data); err != nil {
		return StoreData{}, err
	}
	if data.Providers == nil {
		data.Providers = map[string]ModelConfig{}
	}
	return data, nil
}

func (s *Store) Save(data StoreData) error {
	if data.Providers == nil {
		data.Providers = map[string]ModelConfig{}
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	key, err := s.readKey(true)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}

	encrypted, err := encryptPayload(key, payload)
	if err != nil {
		return err
	}
	return os.WriteFile(s.dataPath, encrypted, 0o600)
}

func (s *Store) Upsert(cfg ModelConfig) (StoreData, error) {
	data, err := s.Load()
	if err != nil {
		return StoreData{}, err
	}
	if data.Providers == nil {
		data.Providers = map[string]ModelConfig{}
	}
	cfg.UpdatedAt = time.Now()
	data.Providers[cfg.Provider] = cfg
	data.Current = cfg.Provider
	return data, s.Save(data)
}

func (s *Store) Current() (ModelConfig, bool, error) {
	data, err := s.Load()
	if err != nil {
		return ModelConfig{}, false, err
	}
	if data.Current == "" {
		return ModelConfig{}, false, nil
	}
	cfg, ok := data.Providers[data.Current]
	return cfg, ok, nil
}

func (s *Store) readKey(create bool) ([]byte, error) {
	if _, err := os.Stat(s.keyPath); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if !create {
			return nil, fmt.Errorf("model store key missing")
		}
		if err := os.MkdirAll(s.dir, 0o755); err != nil {
			return nil, err
		}
		key := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, err
		}
		encoded := base64.StdEncoding.EncodeToString(key)
		if err := os.WriteFile(s.keyPath, []byte(encoded), 0o600); err != nil {
			return nil, err
		}
		return key, nil
	}

	data, err := os.ReadFile(s.keyPath)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return nil, err
	}
	if len(decoded) != 32 {
		return nil, errors.New("invalid model store key length")
	}
	return decoded, nil
}

func encryptPayload(key []byte, payload []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	cipherText := gcm.Seal(nil, nonce, payload, nil)
	out := append(nonce, cipherText...)
	encoded := base64.StdEncoding.EncodeToString(out)
	return []byte(encoded), nil
}

func decryptPayload(key []byte, encoded []byte) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("invalid encrypted payload")
	}
	nonce := raw[:gcm.NonceSize()]
	cipherText := raw[gcm.NonceSize():]
	return gcm.Open(nil, nonce, cipherText, nil)
}
