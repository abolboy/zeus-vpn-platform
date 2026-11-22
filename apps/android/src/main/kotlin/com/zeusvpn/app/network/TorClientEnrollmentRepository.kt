package com.zeusvpn.app.network

import android.content.Context
import android.os.Build
import com.zeusvpn.app.BuildConfig
import com.zeusvpn.app.data.EncryptedWireGuardConfigStore
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.io.InputStream
import java.net.URI
import java.util.UUID
import javax.net.ssl.HttpsURLConnection

data class TorClientEnrollmentResult(
    val deviceId: String,
    val deviceRef: String,
    val status: String,
)

class TorClientEnrollmentException(
    val errorCode: String,
    message: String,
    val httpStatus: Int? = null,
) : Exception(message)

class PendingTorEnrollmentExistsException : Exception(
    "A Tor enrollment is already pending on this device. Retry or clear that attempt before changing account or invitation."
)

/**
 * Account-bound Tor enrollment. This repository never generates or submits a
 * WireGuard key and never calls the WireGuard enrollment endpoint.
 *
 * The idempotency key and device reference survive process death in a dedicated
 * Android-Keystore AES-GCM record. OIDC ID tokens are refreshed for each request
 * and are never copied into that pending record.
 */
class TorClientEnrollmentRepository(context: Context) {
    private val appContext = context.applicationContext
    private val encryptedStore = EncryptedWireGuardConfigStore(appContext)
    private val identityRepository = CustomerIdentityRepository(appContext)
    private val configuredBaseUrl = BuildConfig.ZEUS_API_BASE_URL.trim().trimEnd('/')

    val isConfigured: Boolean
        get() = runCatching { apiUrl("/v1/client/tor/devices"); true }.getOrDefault(false)

    fun hasPendingEnrollment(): Boolean = encryptedStore.hasPendingTorEnrollment()

    suspend fun enroll(
        invitationCode: String? = null,
        deviceName: String = Build.MODEL.orEmpty(),
    ): TorClientEnrollmentResult = withContext(Dispatchers.IO) {
        val cleanInvitation = invitationCode.orEmpty().trim()
        if (cleanInvitation.isNotEmpty() && !isInvitationCodeShapeValid(cleanInvitation)) {
            throw TorClientEnrollmentException(
                "enrollment_code_invalid",
                "کد دعوت Tor معتبر نیست.",
            )
        }
        if (cleanInvitation.isEmpty() && !identityRepository.isConfigured) {
            throw TorClientEnrollmentException(
                "customer_identity_not_configured",
                "ورود حساب ZEUS در این نسخه پیکربندی نشده است.",
            )
        }

        val safeName = deviceName.filterNot { it.isISOControl() }.trim()
            .take(80).ifBlank { "Android device" }
        val pending = loadPending() ?: PendingTorEnrollment(
            invitationCode = cleanInvitation,
            deviceRef = UUID.randomUUID().toString(),
            deviceName = safeName,
            clientVersion = BuildConfig.VERSION_NAME.take(64),
            idempotencyKey = UUID.randomUUID().toString(),
        ).also(::savePending)

        if (pending.invitationCode != cleanInvitation || pending.deviceName != safeName) {
            throw PendingTorEnrollmentExistsException()
        }

        val authorization = if (cleanInvitation.isNotEmpty()) {
            "EnrollmentCode $cleanInvitation"
        } else {
            "Bearer ${identityRepository.freshIdToken()}"
        }

        val requestBody = JSONObject()
            .put("device_ref", pending.deviceRef)
            .put("device_name", pending.deviceName)
            .put("platform", "android")
            .put("client_version", pending.clientVersion)
            .toString()
            .toByteArray(Charsets.UTF_8)

        val response = try {
            request(
                method = "POST",
                path = "/v1/client/tor/devices",
                authorization = authorization,
                idempotencyKey = pending.idempotencyKey,
                body = requestBody,
            )
        } catch (error: TorClientEnrollmentException) {
            if (error.errorCode in FINAL_ENROLLMENT_ERRORS) {
                encryptedStore.clearPendingTorEnrollment()
            }
            throw error
        } finally {
            requestBody.fill(0)
        }

        val deviceId = response.optString("id")
        val transport = response.optString("transport")
        val status = response.optString("status")
        if (!deviceId.matches(DEVICE_ID_PATTERN) || transport != "tor" || status != "active") {
            throw IOException("The ZEUS API returned an invalid Tor device enrollment response.")
        }

        encryptedStore.clearPendingTorEnrollment()
        TorClientEnrollmentResult(deviceId, pending.deviceRef, status)
    }

    suspend fun getDeviceStatus(deviceId: String): JSONObject = withContext(Dispatchers.IO) {
        require(deviceId.matches(DEVICE_ID_PATTERN)) { "Invalid Tor device identifier." }
        val token = identityRepository.freshIdToken()
        request(
            method = "GET",
            path = "/v1/client/tor/devices/$deviceId",
            authorization = "Bearer $token",
        )
    }

    suspend fun revokeDevice(deviceId: String): JSONObject = withContext(Dispatchers.IO) {
        require(deviceId.matches(DEVICE_ID_PATTERN)) { "Invalid Tor device identifier." }
        val token = identityRepository.freshIdToken()
        request(
            method = "POST",
            path = "/v1/client/tor/devices/$deviceId/revoke",
            authorization = "Bearer $token",
        )
    }

    fun clearPendingEnrollment() {
        encryptedStore.clearPendingTorEnrollment()
    }

    private suspend fun request(
        method: String,
        path: String,
        authorization: String,
        idempotencyKey: String? = null,
        body: ByteArray? = null,
    ): JSONObject = withContext(Dispatchers.IO) {
        val connection = apiUrl(path).openConnection() as HttpsURLConnection
        try {
            connection.requestMethod = method
            connection.connectTimeout = CONNECT_TIMEOUT_MS
            connection.readTimeout = READ_TIMEOUT_MS
            connection.instanceFollowRedirects = false
            connection.useCaches = false
            connection.setRequestProperty("Accept", "application/json")
            connection.setRequestProperty("Authorization", authorization)
            idempotencyKey?.let { connection.setRequestProperty("Idempotency-Key", it) }
            if (body != null) {
                connection.doOutput = true
                connection.setRequestProperty("Content-Type", "application/json; charset=utf-8")
                connection.outputStream.use { it.write(body) }
            }

            val status = connection.responseCode
            val stream = if (status in 200..299) connection.inputStream else connection.errorStream
            val responseText = stream?.use { it.readUtf8Limited(MAX_RESPONSE_BYTES) }.orEmpty()
            val json = runCatching { JSONObject(responseText) }.getOrElse {
                throw IOException("The ZEUS API returned an invalid Tor response.")
            }
            if (status !in 200..299) {
                val error = json.optJSONObject("error")
                throw TorClientEnrollmentException(
                    error?.optString("code").orEmpty().ifBlank { "http_$status" },
                    error?.optString("message").orEmpty().ifBlank { "درخواست Tor انجام نشد." },
                    status,
                )
            }
            json
        } finally {
            connection.disconnect()
        }
    }

    private fun apiUrl(path: String): java.net.URL {
        require(path.startsWith("/") && !path.startsWith("//")) { "Invalid ZEUS API path." }
        require(configuredBaseUrl.isNotBlank()) { "ZEUS_API_BASE_URL is not configured." }
        val base = URI(configuredBaseUrl)
        require(
            base.scheme.equals("https", ignoreCase = true) &&
                !base.host.isNullOrBlank() &&
                base.userInfo == null && base.query == null && base.fragment == null
        ) { "The ZEUS API base URL must be a valid HTTPS URL." }
        return URI(configuredBaseUrl + path).toURL()
    }

    private fun loadPending(): PendingTorEnrollment? {
        val bytes = encryptedStore.readPendingTorEnrollment() ?: return null
        return try {
            val json = JSONObject(String(bytes, Charsets.UTF_8))
            PendingTorEnrollment(
                invitationCode = json.getString("invitation_code"),
                deviceRef = json.getString("device_ref"),
                deviceName = json.getString("device_name"),
                clientVersion = json.getString("client_version"),
                idempotencyKey = json.getString("idempotency_key"),
            ).also { pending ->
                require(pending.deviceRef.matches(DEVICE_REF_PATTERN))
                require(pending.idempotencyKey.matches(IDEMPOTENCY_PATTERN))
                require(pending.deviceName.length in 1..80)
                require(pending.clientVersion.length in 1..64)
            }
        } catch (error: Exception) {
            encryptedStore.clearPendingTorEnrollment()
            throw IOException("The encrypted pending Tor enrollment record is invalid.", error)
        } finally {
            bytes.fill(0)
        }
    }

    private fun savePending(pending: PendingTorEnrollment) {
        val bytes = JSONObject()
            .put("invitation_code", pending.invitationCode)
            .put("device_ref", pending.deviceRef)
            .put("device_name", pending.deviceName)
            .put("client_version", pending.clientVersion)
            .put("idempotency_key", pending.idempotencyKey)
            .toString()
            .toByteArray(Charsets.UTF_8)
        encryptedStore.savePendingTorEnrollment(bytes)
    }

    private fun isInvitationCodeShapeValid(value: String): Boolean =
        value.length == INVITATION_CODE_LENGTH && value.all {
            it in 'A'..'Z' || it in 'a'..'z' || it in '0'..'9' || it == '-' || it == '_'
        }

    private data class PendingTorEnrollment(
        val invitationCode: String,
        val deviceRef: String,
        val deviceName: String,
        val clientVersion: String,
        val idempotencyKey: String,
    )

    private companion object {
        val DEVICE_ID_PATTERN = Regex("^[0-9a-fA-F-]{36}$")
        val DEVICE_REF_PATTERN = Regex("^[0-9a-fA-F-]{36}$")
        val IDEMPOTENCY_PATTERN = Regex("^[0-9a-fA-F-]{36}$")
        const val INVITATION_CODE_LENGTH = 43
        const val CONNECT_TIMEOUT_MS = 10_000
        const val READ_TIMEOUT_MS = 20_000
        const val MAX_RESPONSE_BYTES = 32 * 1024
        val FINAL_ENROLLMENT_ERRORS = setOf(
            "enrollment_code_invalid",
            "enrollment_code_expired",
            "enrollment_code_used",
            "device_conflict",
            "idempotency_conflict",
        )
    }
}

private fun InputStream.readUtf8Limited(maximumBytes: Int): String {
    val output = ByteArrayOutputStream()
    val buffer = ByteArray(4096)
    var total = 0
    while (true) {
        val count = read(buffer)
        if (count < 0) break
        total += count
        if (total > maximumBytes) throw IOException("ZEUS API response is too large.")
        output.write(buffer, 0, count)
    }
    buffer.fill(0)
    val bytes = output.toByteArray()
    return try {
        String(bytes, Charsets.UTF_8)
    } finally {
        bytes.fill(0)
    }
}
