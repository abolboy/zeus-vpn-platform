package com.zeusvpn.app.network

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class TorClientEnrollmentRepositoryTest {
    @Test
    fun acceptsOnlyFortyThreeCharacterBase64UrlInvitationCodes() {
        assertTrue(isTorInvitationCodeShapeValid("A".repeat(43)))
        assertTrue(isTorInvitationCodeShapeValid("abcdefghijklmnopqrstuvwxyzABCDEFG-0123456789"))
        assertFalse(isTorInvitationCodeShapeValid("A".repeat(42)))
        assertFalse(isTorInvitationCodeShapeValid("A".repeat(44)))
        assertFalse(isTorInvitationCodeShapeValid("A".repeat(42) + "="))
        assertFalse(isTorInvitationCodeShapeValid("A".repeat(42) + "/"))
    }

    @Test
    fun acceptsOnlyCanonicalUuidDeviceIdentifiers() {
        assertTrue(isTorDeviceIdentifierValid("8e09c3da-fbac-4b34-9ad0-090e1f8d4c47"))
        assertFalse(isTorDeviceIdentifierValid("8e09c3dafbac4b349ad0090e1f8d4c47"))
        assertFalse(isTorDeviceIdentifierValid("8e09c3da-fbac-4b34-9ad0-090e1f8d4c4z"))
        assertFalse(isTorDeviceIdentifierValid("not-a-device-id"))
    }
}
