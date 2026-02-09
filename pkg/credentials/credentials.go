package credentials

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/argon2"
)

type Store struct {
	db  *sql.DB
	gcm cipher.AEAD
}

func New(db *sql.DB, masterKey string) (*Store, error) {
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

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO credentials (id, name, encrypted_value, created_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET encrypted_value = excluded.encrypted_value`,
		uuid.NewString(), name, encrypted, time.Now().UTC(),
	)
	return err
}

func (s *Store) Get(ctx context.Context, name string) (string, error) {
	var encrypted []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT encrypted_value FROM credentials WHERE name = ?`, name,
	).Scan(&encrypted)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("credentials: %q not found", name)
		}
		return "", err
	}

	plaintext, err := s.decrypt(encrypted)
	if err != nil {
		return "", fmt.Errorf("credentials: decrypting %q: %w", name, err)
	}

	return string(plaintext), nil
}

func (s *Store) Delete(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM credentials WHERE name = ?`, name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("credentials: %q not found", name)
	}
	return nil
}

func (s *Store) List(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM credentials ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
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
