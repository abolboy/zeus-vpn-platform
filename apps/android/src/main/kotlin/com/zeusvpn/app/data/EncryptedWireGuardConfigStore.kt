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
 * Stores a WireGuard profile encrypted with an Android Keystore AES-GCM key.
 * The encrypted file lives in noBackupFilesDir and is excluded from Android backup.
 */
class EncryptedWireGuardConfigStore(context: android.content.Context) {
    private val file = AtomicFile(File(context.applicationContext.noBackupFilesDir, FILE_NAME))

    fun hasProfile(): Boolean = file.baseFile.isFile

    fun save(profileText: String) {
        require(profileText.isNotBlank()) { "Profile is empty" }
        val plaintext = profileText.toByteArray(Charsets.UTF_8)
        require(plaintext.size <= MAX_PROFILE_BYTES) { "Profile is too large" }

        val encrypted = try {
            encrypt(plaintext)
        } finally {
            plaintext.fill(0)
        }

        var output: FileOutputStream? = null
        try {
            output = file.startWrite()
            output.write(encrypted)
            file.finishWrite(output)
        } catch (error: Exception) {
            output?.let(file::failWrite)
            throw error
        } finally {
            encrypted.fill(0)
        }
    }

    fun read(): ByteArray? {
        if (!file.baseFile.isFile) return null
        val payload = file.openRead().use { it.readBytes() }
        return try {
            decrypt(payload)
        } finally {
            payload.fill(0)
        }
    }

    fun clear() {
        file.delete()
        val keyStore = KeyStore.getInstance(ANDROID_KEY_STORE)
        keyStore.load(null)
        if (keyStore.containsAlias(KEY_ALIAS)) {
            keyStore.deleteEntry(KEY_ALIAS)
        }
    }

    private fun encrypt(plaintext: ByteArray): ByteArray {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, getOrCreateKey())
        cipher.updateAAD(AAD)
        val ciphertext = cipher.doFinal(plaintext)
        val iv = cipher.iv
        return ByteArrayOutputStream().use { buffer ->
            DataOutputStream(buffer).use { output ->
                output.writeInt(MAGIC)
                output.writeByte(iv.size)
                output.write(iv)
                output.write(ciphertext)
            }
            buffer.toByteArray()
        }
    }

    private fun decrypt(payload: ByteArray): ByteArray {
        require(payload.size in MIN_ENCRYPTED_BYTES..(MAX_PROFILE_BYTES + 64)) {
            "Encrypted profile has an invalid size"
        }
        val input = DataInputStream(ByteArrayInputStream(payload))
        require(input.readInt() == MAGIC) { "Encrypted profile header is invalid" }
        val ivLength = input.readUnsignedByte()
        require(ivLength == GCM_IV_LENGTH) { "Encrypted profile nonce is invalid" }
        val iv = ByteArray(ivLength)
        input.readFully(iv)
        val ciphertext = input.readBytes()
        require(ciphertext.size >= GCM_TAG_BYTES) { "Encrypted profile is truncated" }

        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.DECRYPT_MODE, getOrCreateKey(), GCMParameterSpec(GCM_TAG_BITS, iv))
        cipher.updateAAD(AAD)
        return cipher.doFinal(ciphertext)
    }

    private fun getOrCreateKey(): SecretKey {
        val keyStore = KeyStore.getInstance(ANDROID_KEY_STORE)
        keyStore.load(null)
        (keyStore.getKey(KEY_ALIAS, null) as? SecretKey)?.let { return it }

        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, ANDROID_KEY_STORE)
        generator.init(
            KeyGenParameterSpec.Builder(
                KEY_ALIAS,
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

    private companion object {
        const val FILE_NAME = "wireguard-profile.aesgcm"
        const val ANDROID_KEY_STORE = "AndroidKeyStore"
        const val KEY_ALIAS = "com.zeusvpn.app.wireguard-profile.v1"
        const val TRANSFORMATION = "AES/GCM/NoPadding"
        const val MAGIC = 0x5A575031 // ZWP1
        const val GCM_IV_LENGTH = 12
        const val GCM_TAG_BITS = 128
        const val GCM_TAG_BYTES = GCM_TAG_BITS / 8
        const val MIN_ENCRYPTED_BYTES = 4 + 1 + GCM_IV_LENGTH + GCM_TAG_BYTES
        const val MAX_PROFILE_BYTES = 64 * 1024
        val AAD = "ZEUS-WIREGUARD-PROFILE-V1".toByteArray(Charsets.UTF_8)
    }
}
