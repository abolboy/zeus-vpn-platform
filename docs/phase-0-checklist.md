# Phase 0 — Repository and Tooling Checklist

## Scaffold contents
- [x] Proposed monorepo layout, including tracked placeholders for component directories.
- [x] Initial architecture and trust boundaries documented.
- [x] Initial security baseline documented.
- [x] Repository structure verification script added.
- [x] Minimal GitHub Actions structure-check workflow added.

## Verified environment snapshot
- [x] Android device: Android 13, `aarch64` ABI.
- [x] Termux: Node.js 24.22.2, npm 12.2.2, Git 2.56.0, OpenJDK 17.0.21.
- [x] Codex CLI: 0.162.1; Gradle is not installed globally (the project should use its Gradle Wrapper).
- [x] Storage snapshot: 15 GB available at inspection; recheck before Android SDK/build downloads.
- [x] GitHub repository exists: `abolboy/zeus-vpn-platform`, public, default branch `main`.

## Still to verify
- [ ] Android SDK/build-tool availability and a real Gradle Wrapper build.
- [ ] GitHub Actions execution after the scaffold is pushed.
- [ ] Linux VPS OS, kernel, firewall, routing, and UDP reachability.
- [ ] WireGuard installation and an end-to-end tunnel test on a disposable test node.
- [ ] Phone-hosted inbound server feasibility; do not treat it as supported until experimentally proven.

## Initial validation
Run `bash scripts/verify-repository.sh`.

Expected result: `PASS` for required directories and documentation. This is a structure check only, not an Android/Go build or VPN connectivity test.

## Exit criteria
Phase 0 is complete only after GitHub Actions passes, Android/Termux/VPS checks are recorded with command output, and a test VPN tunnel is established on a Linux node. No production deployment is part of Phase 0.
