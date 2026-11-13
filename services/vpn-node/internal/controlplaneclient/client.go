package controlplaneclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const maxResponseBytes = 64 * 1024

type Config struct {
	BaseURL        string
	ClientCertFile string
	ClientKeyFile  string
	ServerCAFile   string
	Timeout        time.Duration
}

type Command struct {
	ID        string          "json:\"id\""
	NodeID    string          "json:\"node_id\""
	Type      string          "json:\"type\""
	Payload   json.RawMessage "json:\"payload\""
	Attempt   int             "json:\"attempt\""
	ExpiresAt time.Time       "json:\"expires_at\""
}

type Client struct {
	baseURL *url.URL
	http    *http.Client
}

type apiErrorEnvelope struct {
	Error struct {
		Code string "json:\"code\""
	} "json:\"error\""
}

var uuidPattern = regexp.MustCompile("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$")
var errorCodePattern = regexp.MustCompile("^(validation_failed|config_rejected|wg_apply_failed|command_expired|internal_error|mutation_disabled)$")

func New(config Config) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(config.BaseURL))
	if err != nil {
		return nil, errors.New("control-plane URL is invalid")
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "" ||
		parsed.ForceQuery || parsed.Fragment != "" {
		return nil, errors.New("control-plane URL must be an HTTPS origin without credentials, path, query, or fragment")
	}
	if config.ClientCertFile == "" || config.ClientKeyFile == "" || config.ServerCAFile == "" {
		return nil, errors.New("client certificate, client private key, and server CA files are required")
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}
	if config.Timeout < time.Second || config.Timeout > 60*time.Second {
		return nil, errors.New("HTTP timeout must be between 1 and 60 seconds")
	}

	certPEM, err := readTLSFile(config.ClientCertFile, false)
	if err != nil {
		return nil, fmt.Errorf("read node client certificate: %w", err)
	}
	keyPEM, err := readTLSFile(config.ClientKeyFile, true)
	if err != nil {
		return nil, fmt.Errorf("read node client private key: %w", err)
	}
	clientCertificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, errors.New("node client certificate or private key is invalid")
	}
	caPEM, err := readTLSFile(config.ServerCAFile, false)
	if err != nil {
		return nil, fmt.Errorf("read control-plane CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("control-plane CA file contains no valid certificates")
	}

	transport := &http.Transport{
		Proxy:                 nil,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, Certificates: []tls.Certificate{clientCertificate}},
		MaxIdleConns:          2,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: config.Timeout,
		DisableCompression:    true,
	}
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   config.Timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &Client{baseURL: parsed, http: httpClient}, nil
}

func (c *Client) CloseIdleConnections() {
	if c != nil && c.http != nil {
		c.http.CloseIdleConnections()
	}
}

func (c *Client) Poll(ctx context.Context) (*Command, error) {
	endpoint := *c.baseURL
	endpoint.Path = "/agent/v1/commands/next"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, errors.New("create node poll request failed")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("control-plane poll failed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
		return nil, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, responseError("node command poll", response)
	}
	body, err := readBounded(response.Body, maxResponseBytes)
	if err != nil {
		return nil, err
	}
	var command Command
	if err := decodeStrict(body, &command); err != nil {
		return nil, errors.New("control-plane returned an invalid node command envelope")
	}
	if !uuidPattern.MatchString(command.ID) || !uuidPattern.MatchString(command.NodeID) ||
		command.Attempt < 1 || command.Attempt > 10 || command.ExpiresAt.IsZero() ||
		(command.Type != "peer.ensure" && command.Type != "peer.remove") ||
		len(command.Payload) == 0 || !json.Valid(command.Payload) {
		return nil, errors.New("control-plane returned a node command outside the supported schema")
	}
	return &command, nil
}

func (c *Client) Acknowledge(ctx context.Context, commandID, status, errorCode string) error {
	if !uuidPattern.MatchString(commandID) {
		return errors.New("invalid node command identifier")
	}
	if status != "succeeded" && status != "failed" {
		return errors.New("invalid node command acknowledgement status")
	}
	if (status == "failed" && !errorCodePattern.MatchString(errorCode)) ||
		(status == "succeeded" && errorCode != "") {
		return errors.New("invalid node command acknowledgement error code")
	}
	payload, err := json.Marshal(struct {
		Status    string "json:\"status\""
		ErrorCode string "json:\"error_code,omitempty\""
	}{Status: status, ErrorCode: errorCode})
	if err != nil {
		return errors.New("encode node command acknowledgement failed")
	}
	endpoint := *c.baseURL
	endpoint.Path = "/agent/v1/commands/" + commandID + "/ack"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return errors.New("create node acknowledgement request failed")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("control-plane acknowledgement failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return responseError("node command acknowledgement", response)
	}
	_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	return err
}

func responseError(operation string, response *http.Response) error {
	defer io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%s rejected the node identity (HTTP %d)", operation, response.StatusCode)
	case http.StatusConflict:
		return fmt.Errorf("%s conflicted with the command lease (HTTP %d)", operation, response.StatusCode)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%s was rate limited by the control plane", operation)
	default:
		return fmt.Errorf("%s returned HTTP %d", operation, response.StatusCode)
	}
}

func readBounded(reader io.Reader, maximum int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, fmt.Errorf("read control-plane response: %w", err)
	}
	if int64(len(body)) > maximum {
		return nil, errors.New("control-plane response exceeds the configured size limit")
	}
	return body, nil
}

func decodeStrict(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}

func readTLSFile(path string, private bool) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		syscall.Close(fd)
		return nil, errors.New("could not open TLS material")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("TLS material must be a regular file")
	}
	if private && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("node client private key must be owner-only (0600 or stricter)")
	}
	return io.ReadAll(file)
}
