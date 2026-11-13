package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrIdempotencyConflict     = errors.New("idempotency key was already used with a different request")
	ErrVPNUserNotFound         = errors.New("VPN user not found")
	ErrNodeNotFound            = errors.New("VPN node not found")
	ErrNoNodeCommand           = errors.New("no node command is currently available")
	ErrNodeCommandNotFound     = errors.New("node command not found")
	ErrNodeCommandConflict     = errors.New("node command is not in an acknowledgeable state")
	ErrNodeCommandLeaseExpired = errors.New("node command lease expired")
)

type Operator struct {
	ID           string
	Login        string
	Role         string
	Status       string
	PasswordHash string
}

type VPNUser struct {
	ID          string    "json:\"id\""
	DisplayName string    "json:\"display_name\""
	Status      string    "json:\"status\""
	MaxDevices  int       "json:\"max_devices\""
	CreatedAt   time.Time "json:\"created_at\""
}

type UserCursor struct {
	CreatedAt time.Time
	ID        string
}

type CreateUserInput struct {
	DisplayName string "json:\"display_name\""
	MaxDevices  int    "json:\"max_devices\""
}

type UpdateUserInput struct {
	DisplayName *string "json:\"display_name,omitempty\""
	Status      *string "json:\"status,omitempty\""
	MaxDevices  *int    "json:\"max_devices,omitempty\""
}

type NodeIdentity struct {
	ID string
}

type NodeCommand struct {
	ID        string          "json:\"id\""
	NodeID    string          "json:\"node_id\""
	Type      string          "json:\"type\""
	Payload   json.RawMessage "json:\"payload\""
	Attempt   int             "json:\"attempt\""
	ExpiresAt time.Time       "json:\"expires_at\""
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

type NodeAgentStore interface {
	FindNodeByCertFingerprint(context.Context, []byte) (NodeIdentity, error)
	LeaseNodeCommand(context.Context, string, time.Duration) (NodeCommand, error)
	AcknowledgeNodeCommand(context.Context, string, string, string, string) error
}
