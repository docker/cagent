package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// SQLiteUserStore implements UserStore using SQLite
type SQLiteUserStore struct {
	db *sql.DB
}

// NewSQLiteUserStore creates a new SQLite user store
func NewSQLiteUserStore(db *sql.DB) *SQLiteUserStore {
	return &SQLiteUserStore{db: db}
}

// CreateUser creates a new user in the database
func (s *SQLiteUserStore) CreateUser(ctx context.Context, user *User) error {
	query := `
		INSERT INTO users (id, email, name, password, created_at, updated_at, is_admin)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query,
		user.ID,
		user.Email,
		user.Name,
		user.Password,
		user.CreatedAt.Format(time.RFC3339),
		user.UpdatedAt.Format(time.RFC3339),
		user.IsAdmin,
	)
	return err
}

// GetUserByID retrieves a user by their ID
func (s *SQLiteUserStore) GetUserByID(ctx context.Context, id string) (*User, error) {
	query := `
		SELECT id, email, name, password, created_at, updated_at, is_admin
		FROM users
		WHERE id = ?
	`
	
	var user User
	var createdAt, updatedAt string
	
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.Password,
		&createdAt,
		&updatedAt,
		&user.IsAdmin,
	)
	
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	
	user.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	user.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	
	return &user, nil
}

// GetUserByEmail retrieves a user by their email
func (s *SQLiteUserStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	query := `
		SELECT id, email, name, password, created_at, updated_at, is_admin
		FROM users
		WHERE email = ?
	`
	
	var user User
	var createdAt, updatedAt string
	
	err := s.db.QueryRowContext(ctx, query, email).Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.Password,
		&createdAt,
		&updatedAt,
		&user.IsAdmin,
	)
	
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	
	user.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	user.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	
	return &user, nil
}

// UpdateUser updates a user's information
func (s *SQLiteUserStore) UpdateUser(ctx context.Context, user *User) error {
	query := `
		UPDATE users
		SET email = ?, name = ?, password = ?, updated_at = ?, is_admin = ?
		WHERE id = ?
	`
	
	user.UpdatedAt = time.Now()
	
	result, err := s.db.ExecContext(ctx, query,
		user.Email,
		user.Name,
		user.Password,
		user.UpdatedAt.Format(time.RFC3339),
		user.IsAdmin,
		user.ID,
	)
	
	if err != nil {
		return err
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	
	if rowsAffected == 0 {
		return ErrUserNotFound
	}
	
	return nil
}

// DeleteUser deletes a user from the database
func (s *SQLiteUserStore) DeleteUser(ctx context.Context, id string) error {
	query := `DELETE FROM users WHERE id = ?`
	
	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	
	if rowsAffected == 0 {
		return ErrUserNotFound
	}
	
	return nil
}

// ListUsers returns all users (admin only)
func (s *SQLiteUserStore) ListUsers(ctx context.Context) ([]*User, error) {
	query := `
		SELECT id, email, name, password, created_at, updated_at, is_admin
		FROM users
		ORDER BY created_at DESC
	`
	
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var users []*User
	for rows.Next() {
		var user User
		var createdAt, updatedAt string
		
		err := rows.Scan(
			&user.ID,
			&user.Email,
			&user.Name,
			&user.Password,
			&createdAt,
			&updatedAt,
			&user.IsAdmin,
		)
		
		if err != nil {
			return nil, err
		}
		
		user.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		user.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		
		users = append(users, &user)
	}
	
	return users, nil
}