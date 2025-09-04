package data

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
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

type UserFilter struct {
	Q        string
	Page     int
	PageSize int
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

func (m *UserModel) List(ctx context.Context, f UserFilter) ([]User, int, error) {
	where := `TRUE`
	args := []any{}
	i := 1

	if f.Q != "" {
		where += fmt.Sprintf(" AND (phone_number ILIKE $%d)", i)
		args = append(args, "%"+f.Q+"%")
		i++
	}

	limit := f.PageSize
	offset := (f.Page - 1) * f.PageSize

	args = append(args, limit, offset)

	q := fmt.Sprintf(`
		SELECT id, phone_number, created_at, COUNT(*) OVER() AS total_count
		FROM users
		WHERE %s
		LIMIT $%d OFFSET $%d
	`, where, i, i+1)

	rows, err := m.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var (
		items []User
		total int
	)
	for rows.Next() {
		var u User
		var t int
		if err := rows.Scan(&u.ID, &u.PhoneNumber, &u.CreatedAt, &t); err != nil {
			return nil, 0, err
		}
		items = append(items, u)
		total = t
	}
	if rows.Err() != nil {
		return nil, 0, rows.Err()
	}
	return items, total, nil
}
