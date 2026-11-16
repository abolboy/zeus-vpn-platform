package com.zeusvpn.app.network

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class CustomerSessionPolicyTest {
    @Test
    fun requiresAuthorizedStateAndRefreshCredential() {
        assertTrue(isCustomerSessionRecoverable(true, "refresh-token"))
        assertFalse(isCustomerSessionRecoverable(false, "refresh-token"))
        assertFalse(isCustomerSessionRecoverable(true, null))
        assertFalse(isCustomerSessionRecoverable(true, ""))
        assertFalse(isCustomerSessionRecoverable(true, "   "))
    }
}
