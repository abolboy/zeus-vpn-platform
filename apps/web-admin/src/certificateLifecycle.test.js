import assert from "node:assert/strict";
import test from "node:test";
import { getNodeCertificateState } from "./certificateLifecycle.js";

const now = Date.parse("2026-11-15T00:00:00.000Z");
const day = 24 * 60 * 60 * 1000;
const validCertificate = (overrides = {}) => ({
  not_before: "2026-11-14T00:00:00.000Z",
  not_after: "2027-02-13T00:00:00.000Z",
  revoked_at: null,
  ...overrides,
});

test("classifies a valid certificate with more than 30 days remaining as active", () => {
  assert.deepEqual(getNodeCertificateState(validCertificate(), now), {
    status: "active",
    daysRemaining: 90,
  });
});

test("warns at the exact 30-day expiry boundary", () => {
  const certificate = validCertificate({
    not_after: new Date(now + 30 * day).toISOString(),
  });
  assert.deepEqual(getNodeCertificateState(certificate, now), {
    status: "expiring",
    daysRemaining: 30,
  });
});

test("warns when less than one day remains without reporting zero days", () => {
  const certificate = validCertificate({
    not_after: new Date(now + 1).toISOString(),
  });
  assert.deepEqual(getNodeCertificateState(certificate, now), {
    status: "expiring",
    daysRemaining: 1,
  });
});

test("classifies a certificate as expired at its exact not-after instant", () => {
  const certificate = validCertificate({
    not_after: new Date(now).toISOString(),
  });
  assert.deepEqual(getNodeCertificateState(certificate, now), {
    status: "expired",
    daysRemaining: null,
  });
});

test("classifies a certificate before not-before as not yet valid", () => {
  const certificate = validCertificate({
    not_before: new Date(now + day).toISOString(),
    not_after: new Date(now + 90 * day).toISOString(),
  });
  assert.deepEqual(getNodeCertificateState(certificate, now), {
    status: "not_yet_valid",
    daysRemaining: null,
  });
});

test("revocation takes precedence over dates", () => {
  assert.deepEqual(getNodeCertificateState(validCertificate({
    not_before: "invalid",
    revoked_at: "2026-11-14T12:00:00.000Z",
  }), now), {
    status: "revoked",
    daysRemaining: null,
  });
});

test("rejects missing, malformed, and inverted validity windows", () => {
  for (const certificate of [
    {},
    validCertificate({ not_before: "not-a-date" }),
    validCertificate({ not_after: "not-a-date" }),
    validCertificate({
      not_before: "2027-01-01T00:00:00.000Z",
      not_after: "2026-12-01T00:00:00.000Z",
    }),
  ]) {
    assert.deepEqual(getNodeCertificateState(certificate, now), {
      status: "unknown",
      daysRemaining: null,
    });
  }
});

test("does not warn at 30 days plus one millisecond", () => {
  const certificate = validCertificate({
    not_after: new Date(now + 30 * day + 1).toISOString(),
  });
  assert.deepEqual(getNodeCertificateState(certificate, now), {
    status: "active",
    daysRemaining: 31,
  });
});
