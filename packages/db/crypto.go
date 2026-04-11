package db

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql/driver"
	"encoding/base64"
	"fmt"
	"io"
)

// encryptionKey is set once at application startup via SetEncryptionKey.
// It must be 32 bytes for AES-256.
var encryptionKey []byte

// SetEncryptionKey initialises the package-level AES-256 encryption key.
// Call this from main() before any DB operations, passing cfg.EncryptionKey.
func SetEncryptionKey(key string) {
	k := []byte(key)
	encryptionKey = make([]byte, 32)
	copy(encryptionKey, k) // pads with 0x00 if key < 32 bytes
}

// EncryptedString is a GORM-compatible type that transparently encrypts values
// with AES-256-GCM before writing to the DB and decrypts on read.
// JSON serialisation is always suppressed (json:"-") on every field of this type.
type EncryptedString string

func (e EncryptedString) Value() (driver.Value, error) {
	if e == "" {
		return "", nil
	}
	if len(encryptionKey) == 0 {
		return string(e), nil // passthrough when key not configured (dev/test)
	}

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("EncryptedString encrypt: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("EncryptedString gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("EncryptedString nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(e), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (e *EncryptedString) Scan(value any) error {
	if value == nil {
		*e = ""
		return nil
	}
	str, ok := value.(string)
	if !ok {
		return fmt.Errorf("EncryptedString: expected string, got %T", value)
	}
	if str == "" {
		*e = ""
		return nil
	}
	if len(encryptionKey) == 0 {
		*e = EncryptedString(str)
		return nil
	}

	ciphertext, err := base64.StdEncoding.DecodeString(str)
	if err != nil {
		// Treat as plaintext (legacy / unencrypted rows)
		*e = EncryptedString(str)
		return nil
	}

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return fmt.Errorf("EncryptedString decrypt cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("EncryptedString decrypt gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return fmt.Errorf("EncryptedString: ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return fmt.Errorf("EncryptedString decrypt: %w", err)
	}

	*e = EncryptedString(plaintext)
	return nil
}
