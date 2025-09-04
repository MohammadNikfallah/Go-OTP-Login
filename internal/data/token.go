package data

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"time"
)

// Token represents a user id token record.
// swagger:model Token
type Token struct {
	Plaintext string    `json:"plaintext"`
	Hash      []byte    `json:"-"`
	UserId    int64     `json:"-"`
	Expiry    time.Time `json:"expiry"`
}

type TokenModel struct {
	DB *sql.DB
}

func generateToken(userId int64, ttl time.Duration) (*Token, error) {
	token := &Token{
		UserId: userId,
		Expiry: time.Now().Add(ttl),
	}

	randomBytes := make([]byte, 16)

	_, err := rand.Read(randomBytes)
	if err != nil {
		return nil, err
	}

	token.Plaintext = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(randomBytes)
	hash := sha256.Sum256([]byte(token.Plaintext))
	token.Hash = hash[:]

	return token, nil

}

func (m TokenModel) Insert(token *Token) error {
	query := `INSERT INTO tokens (hash, user_id, expiry)
	VALUES ($1, $2, $3)`

	args := []interface{}{token.Hash, token.UserId, token.Expiry}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	_, err := m.DB.ExecContext(ctx, query, args...)
	return err
}

func (m TokenModel) DeleteAllForUser(userID int64) error {
	query := `DELETE FROM tokens  WHERE user_id = $1`

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	_, err := m.DB.ExecContext(ctx, query, userID)
	return err
}

func (m TokenModel) New(userId int64, ttl time.Duration) (*Token, error) {
	token, err := generateToken(userId, ttl)
	if err != nil {
		return nil, err
	}

	err = m.Insert(token)
	return token, err
}
