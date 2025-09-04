package data

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"time"
)

// User represents a user record.
// swagger:model User
type User struct {
	ID          int64     `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	PhoneNumber string    `json:"phone_number"`
}

type UserModel struct {
	DB *sql.DB
}

var AnonymousUser = &User{}

func (m *User) IsAnonymous() bool {
	return m == AnonymousUser
}

func (m UserModel) Insert(user *User) error {
	query := `
		INSERT INTO users (phone_number)
		VALUES ($1)
		RETURNING id, created_at
	`

	args := []interface{}{user.PhoneNumber}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query, args...).Scan(&user.ID, &user.CreatedAt)
	if err != nil {
		return err
	}

	return nil
}

func (m UserModel) GetByPhoneNumber(PhoneNumber string) (*User, error) {
	query := `
        SELECT id, created_at, phone_number
        FROM users
        WHERE phone_number = $1
    `

	var user User

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query, PhoneNumber).Scan(&user.ID, &user.CreatedAt, &user.PhoneNumber)

	if err != nil {
		return nil, err
	}

	return &user, nil
}

func (m UserModel) GetForToken(tokenPlainText string) (*User, error) {

	tokenHash := sha256.Sum256([]byte(tokenPlainText))

	query := `SELECT users.id, users.created_at,  users.phone_number,  users.version
	FROM users
	INNER JOIN tokens
	ON users.id = tokens.user_id
	WHERE tokens.hash = $1 AND tokens.expiry > $2
	`

	args := []interface{}{tokenHash[:], time.Now()}

	var user User

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query, args...).Scan(&user.ID, &user.CreatedAt, &user.PhoneNumber)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, ErrRecordNotFound
		default:
			return nil, err

		}
	}

	return &user, nil
}

func (m UserModel) GetByID(id int64) (*User, error) {
	query := `
        SELECT id, created_at, phone_number
        FROM users
        WHERE id = $1
    `

	var user User
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query, id).Scan(
		&user.ID,
		&user.CreatedAt,
		&user.PhoneNumber,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, ErrRecordNotFound
		default:
			return nil, err
		}
	}

	return &user, nil
}
