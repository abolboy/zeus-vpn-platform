package com.zeusvpn.app.data

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.AtomicFile
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.DataInputStream
import java.io.DataOutputStream
import java.io.File
import java.io.FileOutputStream
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * Stores both the active WireGuard profile and an in-flight enrollment attempt.
 *
 * Both files are written under noBackupFilesDir and protected with separate
 * Android Keystore AES-256-GCM keys. The pending record contains the one-time
 * activation code and client private key only so a lost HTTP response can be
 * retried with the same device identity.
 */
class EncryptedWireGuardConfigStore(context: android.content.Context) {
    private val appContext = context.applicationContext
    private val profileFile = AtomicFile(File(appContext.noBackupFilesDir, PROFILE_FILE_NAME))
    private val pendingFile = AtomicFile(File(appContext.noBackupFilesDir, PENDING_FILE_NAME))

    fun hasProfile(): Boolean = profileFile.baseFile.isFile

    fun hasPendingEnrollment(): Boolean = pendingFile.baseFile.isFile

    fun save(profileText: String) {
        require(profileText.isNotBlank()) { "Profile is empty" }
        val plaintext = profileText.toByteArray(Charsets.UTF_8)
        saveEncrypted(
            target = profileFile,
            plaintext = plaintext,
            keyAlias = PROFILE_KEY_ALIAS,
            aad = PROFILE_AAD,
            magic = PROFILE_MAGIC,
            maximumBytes = MAX_PROFILE_BYTES,
        )
    }

    fun read(): ByteArray? = readEncrypted(
        source = profileFile,
        keyAlias = PROFILE_KEY_ALIAS,
        aad = PROFILE_AAD,
        magic = PROFILE_MAGIC,
        maximumBytes = MAX_PROFILE_BYTES,
    )

    fun savePendingEnrollment(payload: ByteArray) {
        saveEncrypted(
            target = pendingFile,
            plaintext = payload,
            keyAlias = PENDING_KEY_ALIAS,
            aad = PENDING_AAD,
            magic = PENDING_MAGIC,
            maximumBytes = MAX_PENDING_BYTES,
        )
    }

    fun readPendingEnrollment(): ByteArray? = readEncrypted(
        source = pendingFile,
        keyAlias = PENDING_KEY_ALIAS,
        aad = PENDING_AAD,
        magic = PENDING_MAGIC,
        maximumBytes = MAX_PENDING_BYTES,
    )

    fun clearPendingEnrollment() {
        pendingFile.delete()
        deleteKey(PENDING_KEY_ALIAS)
    }

    fun clear() {
        profileFile.delete()
        deleteKey(PROFILE_KEY_ALIAS)
        clearPendingEnrollment()
    }

    private fun saveEncrypted(
        target: AtomicFile,
        plaintext: ByteArray,
        keyAlias: String,
        aad: ByteArray,
        magic: Int,
        maximumBytes: Int,
    ) {
        require(plaintext.isNotEmpty()) { "Encrypted data is empty" }
        require(plaintext.size <= maximumBytes) { "Encrypted data is too large" }

        val encrypted = try {
            encrypt(plaintext, keyAlias, aad, magic)
        } finally {
            plaintext.fill(0)
        }

        var output: FileOutputStream? = null
        try {
            output = target.startWrite()
            output.write(encrypted)
            target.finishWrite(output)
        } catch (error: Exception) {
            output?.let(target::failWrite)
            throw error
        } finally {
            encrypted.fill(0)
        }
    }

    private fun readEncrypted(
        source: AtomicFile,
        keyAlias: String,
        aad: ByteArray,
        magic: Int,
        maximumBytes: Int,
    ): ByteArray? {
        if (!source.baseFile.isFile) return null
        val payload = source.openRead().use { it.readBytes() }
        return try {
            decrypt(payload, keyAlias, aad, magic, maximumBytes)
        } finally {
            payload.fill(0)
        }
    }

    private fun encrypt(
        plaintext: ByteArray,
        keyAlias: String,
        aad: ByteArray,
        magic: Int,
    ): ByteArray {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, getOrCreateKey(keyAlias))
        cipher.updateAAD(aad)
        val ciphertext = cipher.doFinal(plaintext)
        return try {
            ByteArrayOutputStream().use { buffer ->
                DataOutputStream(buffer).use { output ->
                    output.writeInt(magic)
                    output.writeByte(cipher.iv.size)
                    output.write(cipher.iv)
                    output.write(ciphertext)
                }
                buffer.toByteArray()
            }
        } finally {
            ciphertext.fill(0)
        }
    }

    private fun decrypt(
        payload: ByteArray,
        keyAlias: String,
        aad: ByteArray,
        expectedMagic: Int,
        maximumBytes: Int,
    ): ByteArray {
        val maximumEncryptedBytes = maximumBytes + ENVELOPE_OVERHEAD
        require(payload.size in MIN_ENCRYPTED_BYTES..maximumEncryptedBytes) {
            "Encrypted data has an invalid size"
        }
        val input = DataInputStream(ByteArrayInputStream(payload))
        require(input.readInt() == expectedMagic) { "Encrypted data header is invalid" }
        val ivLength = input.readUnsignedByte()
        require(ivLength == GCM_IV_LENGTH) { "Encrypted data nonce is invalid" }
        val iv = ByteArray(ivLength)
        input.readFully(iv)
        val ciphertext = input.readBytes()
        require(ciphertext.size >= GCM_TAG_BYTES) { "Encrypted data is truncated" }

        return try {
            val cipher = Cipher.getInstance(TRANSFORMATION)
            cipher.init(
                Cipher.DECRYPT_MODE,
                getOrCreateKey(keyAlias),
                GCMParameterSpec(GCM_TAG_BITS, iv),
            )
            cipher.updateAAD(aad)
            cipher.doFinal(ciphertext).also { plaintext ->
                require(plaintext.size <= maximumBytes) { "Decrypted data is too large" }
            }
        } finally {
            ciphertext.fill(0)
        }
    }

    private fun getOrCreateKey(alias: String): SecretKey {
        val keyStore = KeyStore.getInstance(ANDROID_KEY_STORE)
        keyStore.load(null)
        (keyStore.getKey(alias, null) as? SecretKey)?.let { return it }

        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, ANDROID_KEY_STORE)
        generator.init(
            KeyGenParameterSpec.Builder(
                alias,
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
            )
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .setRandomizedEncryptionRequired(true)
                .build()
        )
        return generator.generateKey()
    }

    private fun deleteKey(alias: String) {
        val keyStore = KeyStore.getInstance(ANDROID_KEY_STORE)
        keyStore.load(null)
        if (keyStore.containsAlias(alias)) keyStore.deleteEntry(alias)
    }

    private companion object {
        const val PROFILE_FILE_NAME = "wireguard-profile.aesgcm"
        const val PENDING_FILE_NAME = "client-enrollment.pending"
        const val ANDROID_KEY_STORE = "AndroidKeyStore"
        const val PROFILE_KEY_ALIAS = "com.zeusvpn.app.wireguard-profile.v1"
        const val PENDING_KEY_ALIAS = "com.zeusvpn.app.client-enrollment.v1"
        const val TRANSFORMATION = "AES/GCM/NoPadding"
        const val PROFILE_MAGIC = 0x5A575031 // ZWP1
        const val PENDING_MAGIC = 0x5A574531 // ZWE1
        const val GCM_IV_LENGTH = 12
        const val GCM_TAG_BITS = 128
        const val GCM_TAG_BYTES = GCM_TAG_BITS / 8
        const val MIN_ENCRYPTED_BYTES = 4 + 1 + GCM_IV_LENGTH + GCM_TAG_BYTES
        const val ENVELOPE_OVERHEAD = 4 + 1 + GCM_IV_LENGTH + GCM_TAG_BYTES
        const val MAX_PROFILE_BYTES = 64 * 1024
        const val MAX_PENDING_BYTES = 8 * 1024
        val PROFILE_AAD = "ZEUS-WIREGUARD-PROFILE-V1".toByteArray(Charsets.UTF_8)
        val PENDING_AAD = "ZEUS-CLIENT-ENROLLMENT-V1".toByteArray(Charsets.UTF_8)
    }
}
