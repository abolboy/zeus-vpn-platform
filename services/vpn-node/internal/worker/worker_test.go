package worker

import (
	"context"
	"encoding/json"
	"net/netip"
	"testing"
	"time"

	"github.com/abolboy/zeus-vpn-platform/services/vpn-node/internal/controlplaneclient"
	"github.com/abolboy/zeus-vpn-platform/services/vpn-node/internal/wireguard"
)

type fakeClient struct {
	command *controlplaneclient.Command
	acks []ackRecord
}

type ackRecord struct {
	id string
	status string
	code string
}

func (f *fakeClient) Poll(context.Context) (*controlplaneclient.Command, error) {
	return f.command, nil
}

func (f *fakeClient) Acknowledge(_ context.Context, id, status, code string) error {
	f.acks = append(f.acks, ackRecord{id: id, status: status, code: code})
	return nil
}

type fakeManager struct {
	ensureCalls int
	removeCalls int
	peer wireguard.PeerSpec
	publicKey string
	result wireguard.Result
	err error
}

func (f *fakeManager) EnsurePeer(_ context.Context, peer wireguard.PeerSpec) (wireguard.Result, error) {
	f.ensureCalls++
	f.peer = peer
	return f.result, f.err
}

func (f *fakeManager) RemovePeer(_ context.Context, publicKey string) (wireguard.Result, error) {
	f.removeCalls++
	f.publicKey = publicKey
	return f.result, f.err
}

func newTestWorker(t *testing.T, command *controlplaneclient.Command, result wireguard.Result, operationError error) (*Worker, *fakeClient, *fakeManager) {
	t.Helper()
	client := &fakeClient{command: command}
	manager := &fakeManager{result: result, err: operationError}
	w, err := New(client, manager, Config{PollInterval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return w, client, manager
}

func TestEnsurePeerCommandAppliesAndAcknowledges(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{
		"public_key": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"ipv4": "10.77.77.3",
		"ipv6": "fd77:77:77::3",
	})
	command := &controlplaneclient.Command{
		ID: "8e09c3da-fbac-4b34-9ad0-090e1f8d4c47",
		Type: "peer.ensure",
		Payload: payload,
		ExpiresAt: time.Now().Add(time.Minute),
	}
	w, client, manager := newTestWorker(t, command, wireguard.Result{Changed:true}, nil)
	if err := w.process(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	if manager.ensureCalls != 1 || manager.removeCalls != 0 {
		t.Fatalf("unexpected executor calls: ensure=%d remove=%d", manager.ensureCalls, manager.removeCalls)
	}
	if manager.peer.IPv4 != netip.MustParseAddr("10.77.77.3") || manager.peer.IPv6 != netip.MustParseAddr("fd77:77:77::3") {
		t.Fatalf("unexpected peer addresses: %+v", manager.peer)
	}
	if len(client.acks) != 1 || client.acks[0].status != "succeeded" {
		t.Fatalf("unexpected acknowledgement: %+v", client.acks)
	}
}

func TestDryRunDoesNotReportMutationSuccess(t *testing.T) {
	payload := []byte("{\"public_key\":\"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\",\"ipv4\":\"10.77.77.3\",\"ipv6\":\"fd77:77:77::3\"}")
	command := &controlplaneclient.Command{ID:"8e09c3da-fbac-4b34-9ad0-090e1f8d4c47",Type:"peer.ensure",Payload:payload,ExpiresAt:time.Now().Add(time.Minute)}
	w, client, manager := newTestWorker(t, command, wireguard.Result{DryRun:true,Changed:true}, nil)
	if err := w.process(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	if manager.ensureCalls != 1 || len(client.acks) != 1 || client.acks[0].status != "failed" || client.acks[0].code != "mutation_disabled" {
		t.Fatalf("dry-run must not be acknowledged as success: calls=%d acks=%+v", manager.ensureCalls, client.acks)
	}
}

func TestExpiredAndInvalidCommandsAreRejectedBeforeExecution(t *testing.T) {
	for _, tc := range []struct{
		name string
		command *controlplaneclient.Command
		expectedCode string
	}{
		{
			name:"expired",
			command:&controlplaneclient.Command{ID:"8e09c3da-fbac-4b34-9ad0-090e1f8d4c47",Type:"peer.remove",Payload:[]byte("{\"public_key\":\"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\"}"),ExpiresAt:time.Now().Add(-time.Second)},
			expectedCode:"command_expired",
		},
		{
			name:"unknown command",
			command:&controlplaneclient.Command{ID:"8e09c3da-fbac-4b34-9ad0-090e1f8d4c47",Type:"shell.exec",Payload:[]byte("{}"),ExpiresAt:time.Now().Add(time.Minute)},
			expectedCode:"validation_failed",
		},
		{
			name:"extra payload field",
			command:&controlplaneclient.Command{ID:"8e09c3da-fbac-4b34-9ad0-090e1f8d4c47",Type:"peer.remove",Payload:[]byte("{\"public_key\":\"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\",\"command\":\"id\"}"),ExpiresAt:time.Now().Add(time.Minute)},
			expectedCode:"validation_failed",
		},
	} {
		t.Run(tc.name,func(t *testing.T){
			w, client, manager := newTestWorker(t, tc.command, wireguard.Result{}, nil)
			if err := w.process(context.Background(), tc.command); err != nil {
				t.Fatal(err)
			}
			if manager.ensureCalls+manager.removeCalls != 0 {
				t.Fatal("invalid command reached WireGuard executor")
			}
			if len(client.acks)!=1 || client.acks[0].status!="failed" || client.acks[0].code!=tc.expectedCode {
				t.Fatalf("unexpected acknowledgement: %+v",client.acks)
			}
		})
	}
}

func TestRemovePeerCommand(t *testing.T) {
	command:=&controlplaneclient.Command{
		ID:"8e09c3da-fbac-4b34-9ad0-090e1f8d4c47",
		Type:"peer.remove",
		Payload:[]byte("{\"public_key\":\"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\"}"),
		ExpiresAt:time.Now().Add(time.Minute),
	}
	w,client,manager:=newTestWorker(t,command,wireguard.Result{Changed:true},nil)
	if err:=w.process(context.Background(),command);err!=nil{t.Fatal(err)}
	if manager.removeCalls!=1||manager.publicKey!="AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="{
		t.Fatalf("unexpected remove operation: %+v",manager)
	}
	if len(client.acks)!=1||client.acks[0].status!="succeeded"{t.Fatalf("unexpected acknowledgement: %+v",client.acks)}
}
