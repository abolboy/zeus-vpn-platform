package main

import (
	"context"
	"time"
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
