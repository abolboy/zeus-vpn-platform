# WireGuard tunnel connects but has no usable internet

A successful Android `VpnService` / WireGuard engine start is only a local state. A recent WireGuard handshake proves the encrypted peer path is working; it does not prove that the VPN host forwards packets to the internet. The client needs both a working WireGuard peer and a working server egress path.

## Run the read-only check on the VPN node

Copy `scripts/diagnose-wireguard-node.sh` to the VPN host (or clone this repository there), then run:

    sudo env WG_CLIENT_IPV4_CIDR=<actual-client-pool-cidr> bash scripts/diagnose-wireguard-node.sh

The script prints peer handshake age and byte counters with public keys shortened. It does not print private keys and does not change the server.

The diagnostic compares NAT/SNAT rules against both the actual client IPv4 pool and the detected uplink. Supply the pool explicitly (for example, use the exact value configured for `ZEUS_WG_IPV4_POOL` on that node); the script may infer a subnet from the interface route, but that inference must be checked against the real allocation pool. It also checks both the forward path and established return path. It remains read-only.

## Interpret the result

- **No recent handshake:** verify that the client endpoint host/port is correct, the server UDP listen port is allowed by the cloud Security Group and host firewall, the network ACL permits return traffic, and the client/server public keys match. Also check the peer exists on the actual `wg0` interface.
- **Recent handshake, but no received bytes:** the client may reach the server while replies or routed traffic are not returning. Inspect peer AllowedIPs and run `ip -4 route get <client-ip>` plus `ip -6 route get <client-ip>`; each assigned address must resolve through `wg0`, not the public uplink. The node agent now verifies per-peer host routes when it applies a peer. Also inspect the FORWARD chain, upstream route, and cloud network controls.
- **Handshake and bidirectional bytes, but HTTPS tests fail:** inspect DNS reachability, IPv4 forwarding, NAT/SNAT for the WireGuard client subnet, and MTU. A NAT rule must use the actual uplink interface and the correct client source subnet; merely having any MASQUERADE rule is not enough.
- **IPv4 works but IPv6 does not:** the Android full-tunnel profile currently includes `::/0`. The node must have a valid IPv6 forwarding and routed egress design for that route. Do not silently remove IPv6 routing without also deciding how IPv6 leak prevention will work.

## Required server-side conditions

For IPv4 internet access through a WireGuard server, verify all of the following against the actual host and client subnet:

1. `net.ipv4.ip_forward=1`.
2. Forward rules allow traffic from the WireGuard interface to the actual uplink and allow established return traffic.
3. A correct SNAT/MASQUERADE rule translates the WireGuard client subnet on the actual uplink interface.
4. The server itself has a working default route and outbound DNS/HTTPS.
5. The cloud firewall/security group and network ACL allow UDP to the WireGuard listen port and the return path.
6. On AWS EC2, Source/Destination Check is disabled for a host acting as a router.

The guest-side script cannot inspect AWS Source/Destination Check, Security Groups, Network ACLs, or route-table associations. Verify those in the AWS console/API separately.

## Android status

The Android dashboard now distinguishes the local tunnel engine from an HTTPS reachability probe performed by the app process while the VPN is active. The probe checks two independent HTTPS 204 endpoints and is repeated periodically. It is a useful signal, not a guarantee that every destination, app, DNS resolver, or transport is reachable. A blocked probe destination can cause a false negative even when another site works.

## Safe rollout sequence

Use a separate staging WireGuard host first. Back up its configuration, confirm the client address pool and existing peers, apply only reviewed network changes, then reconnect and confirm: a recent handshake, both RX/TX counters increasing, and an HTTPS probe succeeding. Do not enable the ZEUS node agent mutation mode on a production host as a shortcut for fixing routing.
