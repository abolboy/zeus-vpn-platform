# ADR-0001: Android-first hybrid platform

- **Status:** Proposed
- **Date:** 2026-11-12

## Context

The product must be an Android application, not merely a VPN client, while also supporting a real VPN service and remote infrastructure.

## Decision

Build an Android management app backed by a control plane and Linux VPN nodes. Treat hosting an inbound VPN server directly on an Android phone as a separate research track, not the primary deployment architecture.

## Consequences

- The first useful end-to-end milestone requires a Linux node.
- Android remains the primary operator interface.
- Server-side networking can be tested independently of Android device limitations.
- Phone-hosted server feasibility must be proven experimentally before it is promised as a feature.
