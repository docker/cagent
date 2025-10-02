package database

import (
	"context"
	"errors"
)

var ErrEmptyID = errors.New("memory ID cannot be empty")

type UserMemory struct {
	ID        string `description:"The ID of the memory"`
	CreatedAt string `description:"The creation timestamp of the memory"`
	Memory    string `description:"The content of the memory"`
}

type Memory interface {
	AddMemory(ctx context.Context, memory UserMemory) error
	GetMemories(ctx context.Context) ([]UserMemory, error)
	DeleteMemory(ctx context.Context, memory UserMemory) error
}

type Database interface {
	AddMemory(ctx context.Context, memory UserMemory) error
	GetMemories(ctx context.Context) ([]UserMemory, error)
	DeleteMemory(ctx context.Context, memory UserMemory) error
}
