package com.zeusvpn.app.network

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PendingEnrollmentErrorPolicyTest {
    @Test
    fun clearsPendingEnrollmentForPermanentInviteAndDeviceErrors() {
        val permanentErrors = listOf(
            "activation_code_invalid",
            "activation_code_expired",
            "activation_code_used",
            "device_conflict",
            "device_not_found",
            "enrollment_status_invalid",
            "device_retry_not_allowed",
        )

        permanentErrors.forEach { errorCode ->
            assertTrue(
                "Expected $errorCode to clear the pending enrollment",
                shouldClearPendingEnrollment(errorCode),
            )
        }
    }

    @Test
    fun retriesProvisioningOnlyOnceForAnErrorState() {
        assertTrue(shouldRetryProvisioning("error", "error", retryAlreadyRequested = false))
        assertTrue(shouldRetryProvisioning("provisioning", "error", retryAlreadyRequested = false))
        assertTrue(shouldRetryProvisioning("error", "pending", retryAlreadyRequested = false))
        assertFalse(shouldRetryProvisioning("provisioning", "pending", retryAlreadyRequested = false))
        assertFalse(shouldRetryProvisioning("active", "active", retryAlreadyRequested = false))
        assertFalse(shouldRetryProvisioning("error", "error", retryAlreadyRequested = true))
    }

    @Test
    fun preservesPendingEnrollmentForRetryableOrReauthenticationErrors() {
        val retryableErrors = listOf(
            "peer_provision_timeout",
            "node_unavailable",
            "address_pool_exhausted",
            "rate_limited",
            "device_status_unavailable",
            "customer_identity_invalid",
            "customer_identity_required",
            "peer_provision_failed",
            "http_500",
        )

        retryableErrors.forEach { errorCode ->
            assertFalse(
                "Expected $errorCode to preserve the pending enrollment",
                shouldClearPendingEnrollment(errorCode),
            )
        }
    }
}
