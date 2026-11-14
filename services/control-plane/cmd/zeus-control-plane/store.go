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
	ErrDeviceNotFound          = errors.New("VPN device not found")
	ErrDeviceConflict          = errors.New("VPN device already exists or is not in a mutable state")
	ErrUserNotActive           = errors.New("VPN user is not active")
	ErrDeviceLimitReached      = errors.New("VPN user has reached the device limit")
	ErrNoProvisioningNode      = errors.New("no eligible online VPN node is available")
	ErrAddressPoolExhausted    = errors.New("VPN node address pool is exhausted")
	ErrEnrollmentCodeInvalid   = errors.New("enrollment code is invalid or revoked")
	ErrEnrollmentCodeExpired   = errors.New("enrollment code has expired")
	ErrEnrollmentCodeUsed      = errors.New("enrollment code has already been consumed")
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



type CreateDeviceInput struct {
	DeviceRef string "json:\"device_ref\""
	DeviceName string "json:\"device_name\""
	Platform string "json:\"platform\""
	PublicKey string "json:\"public_key\""
}

type ClientEnrollmentInput struct {
	ActivationCode string `json:"activation_code"`
	DeviceRef string `json:"device_ref"`
	DeviceName string `json:"device_name"`
	Platform string `json:"platform"`
	PublicKey string `json:"public_key"`
}

type EnrollmentCodeResponse struct {
	Code string `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

type VPNPeerProfile struct {
	ID string "json:\"id\""
	NodeID string "json:\"node_id\""
	PublicKey string "json:\"public_key\""
	AssignedIPv4 string "json:\"assigned_ipv4\""
	AssignedIPv6 string "json:\"assigned_ipv6\""
	ServerPublicKey string "json:\"server_public_key\""
	EndpointHost string "json:\"endpoint_host\""
	EndpointPort int "json:\"endpoint_port\""
	Status string "json:\"status\""
}

type VPNDevice struct {
	ID string "json:\"id\""
	UserID string "json:\"user_id\""
	DeviceRef string "json:\"device_ref\""
	DeviceName string "json:\"device_name\""
	Platform string "json:\"platform\""
	Status string "json:\"status\""
	CreatedAt time.Time "json:\"created_at\""
	LastError string "json:\"last_error,omitempty\""
	Peer VPNPeerProfile "json:\"peer\""
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

type DeviceMutationStore interface {
	ListVPNDevices(context.Context, string) ([]VPNDevice, error)
	CreateDeviceEnrollmentCode(context.Context, string, string, []byte, time.Time, string, *string) error
	EnrollVPNDevice(context.Context, []byte, string, []byte, CreateDeviceInput, string, *string) (VPNDevice, bool, error)
	CreateVPNDevice(context.Context, string, string, string, string, []byte, CreateDeviceInput, string, *string) (VPNDevice, bool, error)
	RevokeVPNDevice(context.Context, string, string, string, string, string, []byte, string, *string) (VPNDevice, bool, error)
}

type NodeAgentStore interface {
	FindNodeByCertFingerprint(context.Context, []byte) (NodeIdentity, error)
	LeaseNodeCommand(context.Context, string, time.Duration) (NodeCommand, error)
	AcknowledgeNodeCommand(context.Context, string, string, string, string) error
}
