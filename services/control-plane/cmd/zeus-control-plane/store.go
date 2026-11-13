package main

import (
	"context"
	"errors"
	"time"
)

var (
	ErrIdempotencyConflict = errors.New("idempotency key was already used with a different request")
	ErrVPNUserNotFound     = errors.New("VPN user not found")
)

type Operator struct {
	ID           string
	Login        string
	Role         string
	Status       string
	PasswordHash string
}

type VPNUser struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"display_name"`
	Status      string    `json:"status"`
	MaxDevices  int       `json:"max_devices"`
	CreatedAt   time.Time `json:"created_at"`
}

type UserCursor struct {
	CreatedAt time.Time
	ID        string
}

type CreateUserInput struct {
	DisplayName string `json:"display_name"`
	MaxDevices  int    `json:"max_devices"`
}

type UpdateUserInput struct {
	DisplayName *string `json:"display_name,omitempty"`
	Status      *string `json:"status,omitempty"`
	MaxDevices  *int    `json:"max_devices,omitempty"`
}

type ControlStore interface {
	Ping(context.Context) error
	FindOperatorByLogin(context.Context, string) (Operator, error)
	HasActiveOwner(context.Context) (bool, error)
	EnsureBootstrapOperator(context.Context, string, string) error
	CreateSession(context.Context, string, []byte, time.Time) error
	FindOperatorForSession(context.Context, []byte) (Operator, error)
	RevokeSession(context.Context, []byte) error
	ListUsers(context.Context, int, *UserCursor) ([]VPNUser, error)
	RecordAudit(context.Context, *string, *string, string, string, string, *string) error
}

type UserMutationStore interface {
	GetVPNUser(context.Context, string) (VPNUser, error)
	CreateVPNUser(context.Context, string, string, string, []byte, CreateUserInput, string, *string) (VPNUser, bool, error)
	UpdateVPNUser(context.Context, string, string, string, string, []byte, UpdateUserInput, string, *string) (VPNUser, bool, error)
}
