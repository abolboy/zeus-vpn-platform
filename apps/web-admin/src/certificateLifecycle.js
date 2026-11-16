const DAY_MS = 24 * 60 * 60 * 1000;
const EXPIRY_WARNING_DAYS = 30;

/**
 * Classify a registered node certificate using its validity window and revocation state.
 * The current time is injectable so boundary behavior can be tested deterministically.
 */
export function getNodeCertificateState(certificate, now = Date.now()) {
  if (certificate?.revoked_at) {
    return { status: "revoked", daysRemaining: null };
  }

  const notBefore = Date.parse(certificate?.not_before ?? "");
  const notAfter = Date.parse(certificate?.not_after ?? "");
  if (!Number.isFinite(notBefore) || !Number.isFinite(notAfter) || notAfter <= notBefore) {
    return { status: "unknown", daysRemaining: null };
  }
  if (notBefore > now) {
    return { status: "not_yet_valid", daysRemaining: null };
  }
  if (notAfter <= now) {
    return { status: "expired", daysRemaining: null };
  }

  const daysRemaining = Math.max(1, Math.ceil((notAfter - now) / DAY_MS));
  return {
    status: daysRemaining <= EXPIRY_WARNING_DAYS ? "expiring" : "active",
    daysRemaining,
  };
}
