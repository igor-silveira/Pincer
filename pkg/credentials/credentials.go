package credentials

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/argon2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Credential struct {
	ID             string    `gorm:"primaryKey;column:id"`
	Name           string    `gorm:"column:name;not null;uniqueIndex"`
	EncryptedValue []byte    `gorm:"column:encrypted_value;not null"`
	CreatedAt      time.Time `gorm:"column:created_at;not null"`
}

type Store struct {
	db  *gorm.DB
	gcm cipher.AEAD
}

func New(db *gorm.DB, masterKey string) (*Store, error) {
	if masterKey == "" {
		return nil, fmt.Errorf("credentials: master key must not be empty")
	}

	derivedKey := deriveKey(masterKey)

	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, fmt.Errorf("credentials: creating cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("credentials: creating GCM: %w", err)
	}

	return &Store{db: db, gcm: gcm}, nil
}

func (s *Store) Set(ctx context.Context, name, value string) error {
	encrypted, err := s.encrypt([]byte(value))
	if err != nil {
		return fmt.Errorf("credentials: encrypting: %w", err)
	}

	cred := &Credential{
		ID:             uuid.NewString(),
		Name:           name,
		EncryptedValue: encrypted,
		CreatedAt:      time.Now().UTC(),
	}

	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"encrypted_value"}),
	}).Create(cred).Error
}

func (s *Store) Get(ctx context.Context, name string) (string, error) {
	var cred Credential
	err := s.db.WithContext(ctx).
		Where("name = ?", name).
		First(&cred).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", fmt.Errorf("credentials: %q not found", name)
		}
		return "", err
	}

	plaintext, err := s.decrypt(cred.EncryptedValue)
	if err != nil {
		return "", fmt.Errorf("credentials: decrypting %q: %w", name, err)
	}

	return string(plaintext), nil
}

func (s *Store) Delete(ctx context.Context, name string) error {
	result := s.db.WithContext(ctx).Where("name = ?", name).Delete(&Credential{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("credentials: %q not found", name)
	}
	return nil
}

func (s *Store) List(ctx context.Context) ([]string, error) {
	var names []string
	err := s.db.WithContext(ctx).
		Model(&Credential{}).
		Order("name").
		Pluck("name", &names).Error
	return names, err
}

func (s *Store) encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return s.gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (s *Store) decrypt(ciphertext []byte) ([]byte, error) {
	nonceSize := s.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, data := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return s.gcm.Open(nil, nonce, data, nil)
}

func deriveKey(masterKey string) []byte {

	saltHash := sha256.Sum256([]byte("pincer-credential-salt:" + masterKey))
	salt := saltHash[:16]

	return argon2.IDKey([]byte(masterKey), salt, 1, 64*1024, 4, 32)
}
