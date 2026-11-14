package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrCustomerIdentityNotVerified = errors.New("customer identity is not verified")
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
	ErrNodeCertificateNotFound = errors.New("node certificate not found")
	ErrNodeCertificateConflict = errors.New("node certificate fingerprint is already registered with different metadata")
	ErrNodeConflict            = errors.New("VPN node name is already registered")
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

type CustomerSelfServiceStore interface {
	EnsureExternalVPNUser(context.Context, string, string) (VPNUser, error)
	EnrollCustomerVPNDevice(context.Context, string, string, []byte, CreateDeviceInput, string, *string) (VPNDevice, bool, error)
}

type NodeAgentStore interface {
	FindNodeByCertFingerprint(context.Context, []byte) (NodeIdentity, error)
	LeaseNodeCommand(context.Context, string, time.Duration) (NodeCommand, error)
	AcknowledgeNodeCommand(context.Context, string, string, string, string) error
}

type NodeAgentCertificate struct {
	ID                string     `json:"id"`
	NodeID            string     `json:"node_id"`
	FingerprintSHA256 string     `json:"fingerprint_sha256"`
	NotBefore         time.Time  `json:"not_before"`
	NotAfter          time.Time  `json:"not_after"`
	RevokedAt         *time.Time `json:"revoked_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

type NodeCertificateStore interface {
	ListNodeAgentCertificates(context.Context, string) ([]NodeAgentCertificate, error)
	RegisterNodeAgentCertificate(context.Context, string, string, []byte, time.Time, time.Time, string, *string) (NodeAgentCertificate, bool, error)
	RevokeNodeAgentCertificate(context.Context, string, string, string, string, *string) (NodeAgentCertificate, bool, error)
}

type VPNNode struct {
	ID                      string     `json:"id"`
	Name                    string     `json:"name"`
	Region                  string     `json:"region"`
	EndpointHost            string     `json:"endpoint_host"`
	EndpointPort            int        `json:"endpoint_port"`
	PublicKey               string     `json:"public_key"`
	InterfaceName           string     `json:"interface_name"`
	Status                  string     `json:"status"`
	LastSeenAt              *time.Time `json:"last_seen_at,omitempty"`
	ClientIPv4Pool          *string    `json:"client_ipv4_pool,omitempty"`
	ClientIPv6Pool          *string    `json:"client_ipv6_pool,omitempty"`
	InterfaceIPv4           *string    `json:"interface_ipv4,omitempty"`
	InterfaceIPv6           *string    `json:"interface_ipv6,omitempty"`
	ActiveCertificateCount  int        `json:"active_certificate_count"`
	CreatedAt               time.Time  `json:"created_at"`
}

type NodeInventoryStore interface {
	ListVPNNodes(context.Context) ([]VPNNode, error)
}

type CreateVPNNodeInput struct {
	Name            string `json:"name"`
	Region          string `json:"region"`
	EndpointHost    string `json:"endpoint_host"`
	EndpointPort    int    `json:"endpoint_port"`
	PublicKey       string `json:"public_key"`
	InterfaceName   string `json:"interface_name"`
	ClientIPv4Pool  string `json:"client_ipv4_pool"`
	ClientIPv6Pool  string `json:"client_ipv6_pool"`
	InterfaceIPv4   string `json:"interface_ipv4"`
	InterfaceIPv6   string `json:"interface_ipv6"`
}

type NodeMutationStore interface {
	CreateVPNNode(context.Context, string, string, string, []byte, CreateVPNNodeInput, string, *string) (VPNNode, bool, error)
}
