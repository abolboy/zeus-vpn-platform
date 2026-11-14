package com.zeusvpn.app.network

import android.os.Build
import android.os.SystemClock
import com.wireguard.crypto.KeyPair
import com.zeusvpn.app.BuildConfig
import com.zeusvpn.app.data.EncryptedWireGuardConfigStore
import com.zeusvpn.app.vpn.WireGuardProfileBuilder
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.io.InputStream
import java.io.IOException
import java.net.URI
import java.util.UUID
import javax.net.ssl.HttpsURLConnection

data class ClientEnrollmentResult(
    val profileText: String,
    val deviceID: String,
)

class ClientEnrollmentException(
    val errorCode: String,
    message: String,
) : Exception(message)

class PendingEnrollmentExistsException : Exception(
    "An unfinished enrollment is stored securely on this device. Retry it before starting another one."
)

/**
 * Client-side activation. The private key is generated locally and remains inside
 * the encrypted pending record/profile store; only its public key is sent to the API.
 */
class ClientEnrollmentRepository(
    context: android.content.Context,
) {
    private val appContext = context.applicationContext
    private val encryptedStore = EncryptedWireGuardConfigStore(appContext)
    private val configuredBaseUrl = BuildConfig.ZEUS_API_BASE_URL.trim().trimEnd('/')

    val isConfigured: Boolean
        get() = runCatching { enrollmentEndpoint(); true }.getOrDefault(false)

    fun hasPendingEnrollment(): Boolean = encryptedStore.hasPendingEnrollment()

    suspend fun enroll(
        activationCode: String,
        deviceName: String = Build.MODEL.orEmpty(),
        idToken: String? = null,
    ): ClientEnrollmentResult = withContext(Dispatchers.IO) {
        val safeDeviceName = deviceName.trim().ifBlank { "Android device" }.take(80)
        val cleanActivationCode = activationCode.trim()
        val cleanIdToken = idToken?.trim()?.takeIf { it.isNotEmpty() }
        val pending = loadPendingEnrollment() ?: createPendingEnrollment(
            activationCode = cleanActivationCode,
            deviceName = safeDeviceName,
            hasCustomerIdentity = cleanIdToken != null,
        )

        if (cleanActivationCode.isNotBlank() && cleanActivationCode != pending.activationCode) {
            throw PendingEnrollmentExistsException()
        }

        val response = try {
            postEnrollment(pending, cleanIdToken)
        } catch (error: ClientEnrollmentException) {
            if (error.errorCode in setOf(
                    "activation_code_invalid",
                    "activation_code_expired",
                    "activation_code_used",
                    "device_conflict",
                )
            ) {
                encryptedStore.clearPendingEnrollment()
            }
            throw error
        }
        var device = response.getJSONObject("device")
        if (pending.activationCode.isBlank()) {
            val identityToken = cleanIdToken
                ?: throw ClientEnrollmentException("customer_identity_required", "برای ادامهٔ ثبت دستگاه دوباره وارد حساب شوید.")
            device = awaitCustomerDeviceActive(device, identityToken)
        }
        val peer = device.getJSONObject("peer")
        val profile = WireGuardProfileBuilder.build(
            clientPrivateKey = pending.privateKey,
            clientIPv4 = peer.getString("assigned_ipv4"),
            clientIPv6 = peer.getString("assigned_ipv6"),
            serverPublicKey = peer.getString("server_public_key"),
            endpointHost = peer.getString("endpoint_host"),
            endpointPort = peer.getInt("endpoint_port"),
        )
        ClientEnrollmentResult(
            profileText = profile,
            deviceID = device.getString("id"),
        )
    }

    fun completeEnrollment() {
        encryptedStore.clearPendingEnrollment()
    }

    private suspend fun awaitCustomerDeviceActive(initial: JSONObject, idToken: String): JSONObject {
        val deviceID = initial.getString("id")
        require(deviceID.matches(Regex("^[0-9a-fA-F-]{36}$"))) {
            "The ZEUS API returned an invalid device identifier."
        }
        var current = initial
        val deadline = SystemClock.elapsedRealtime() + PROVISIONING_TIMEOUT_MS

        while (true) {
            val deviceStatus = current.optString("status")
            val peerStatus = current.optJSONObject("peer")?.optString("status").orEmpty()
            if (deviceStatus == "active" && peerStatus == "active") return current
            if (deviceStatus in setOf("error", "revoking", "revoked") || peerStatus in setOf("error", "revoking", "revoked")) {
                val code = current.optString("last_error").ifBlank { "peer_provision_failed" }
                throw ClientEnrollmentException(
                    code,
                    "سرور نتوانست دستگاه را فعال کند. وضعیت نود یا ظرفیت سرور باید بررسی شود.",
                )
            }
            if (SystemClock.elapsedRealtime() >= deadline) {
                throw ClientEnrollmentException(
                    "peer_provision_timeout",
                    "درخواست ثبت شد اما نود هنوز اعمال کلید را تأیید نکرده است. همین صفحه را بعداً دوباره امتحان کنید.",
                )
            }
            delay(PROVISIONING_POLL_INTERVAL_MS)
            current = fetchCustomerDeviceStatus(deviceID, idToken)
        }
    }

    private suspend fun fetchCustomerDeviceStatus(deviceID: String, idToken: String): JSONObject =
        withContext(Dispatchers.IO) {
            val endpoint = URI(configuredBaseUrl + "/v1/client/devices/" + deviceID).toURL()
            val connection = endpoint.openConnection() as HttpsURLConnection
            try {
                connection.requestMethod = "GET"
                connection.connectTimeout = CONNECT_TIMEOUT_MS
                connection.readTimeout = READ_TIMEOUT_MS
                connection.instanceFollowRedirects = false
                connection.setRequestProperty("Accept", "application/json")
                connection.setRequestProperty("Authorization", "Bearer $idToken")

                val status = connection.responseCode
                val stream = if (status in 200..299) connection.inputStream else connection.errorStream
                val responseText = stream?.use { it.readUtf8Limited(MAX_RESPONSE_BYTES) }.orEmpty()
                val json = runCatching { JSONObject(responseText) }.getOrElse {
                    throw IOException("The ZEUS API returned an invalid device status response.")
                }
                if (status !in 200..299) {
                    val error = json.optJSONObject("error")
                    throw ClientEnrollmentException(
                        error?.optString("code").orEmpty().ifBlank { "http_$status" },
                        error?.optString("message").orEmpty().ifBlank { "بررسی وضعیت ثبت دستگاه انجام نشد." },
                    )
                }
                json
            } finally {
                connection.disconnect()
            }
        }

    private fun createPendingEnrollment(
        activationCode: String,
        deviceName: String,
        hasCustomerIdentity: Boolean,
    ): PendingEnrollment {
        require(
            (activationCode.isNotBlank() && isActivationCodeShapeValid(activationCode)) ||
                (activationCode.isBlank() && hasCustomerIdentity)
        ) {
            "A valid one-time activation code or verified customer session is required."
        }
        require(deviceName.isNotBlank() && deviceName.length <= 80) {
            "Device name is invalid."
        }

        val pair = KeyPair()
        val pending = PendingEnrollment(
            activationCode = activationCode,
            deviceRef = UUID.randomUUID().toString(),
            deviceName = deviceName,
            platform = "android",
            publicKey = pair.publicKey.toBase64(),
            privateKey = pair.privateKey.toBase64(),
            idempotencyKey = UUID.randomUUID().toString(),
        )
        persistPendingEnrollment(pending)
        return pending
    }

    private fun persistPendingEnrollment(pending: PendingEnrollment) {
        val payload = JSONObject()
            .put("activation_code", pending.activationCode)
            .put("device_ref", pending.deviceRef)
            .put("device_name", pending.deviceName)
            .put("platform", pending.platform)
            .put("public_key", pending.publicKey)
            .put("private_key", pending.privateKey)
            .put("idempotency_key", pending.idempotencyKey)
            .toString()
            .toByteArray(Charsets.UTF_8)
        try {
            encryptedStore.savePendingEnrollment(payload)
        } finally {
            payload.fill(0)
        }
    }

    private fun loadPendingEnrollment(): PendingEnrollment? {
        val payload = encryptedStore.readPendingEnrollment() ?: return null
        return try {
            val json = JSONObject(String(payload, Charsets.UTF_8))
            PendingEnrollment(
                activationCode = json.getString("activation_code"),
                deviceRef = json.getString("device_ref"),
                deviceName = json.getString("device_name"),
                platform = json.getString("platform"),
                publicKey = json.getString("public_key"),
                privateKey = json.getString("private_key"),
                idempotencyKey = json.getString("idempotency_key"),
            )
        } finally {
            payload.fill(0)
        }
    }

    private fun postEnrollment(pending: PendingEnrollment, idToken: String?): JSONObject {
        val body = JSONObject()
            .put("device_ref", pending.deviceRef)
            .put("device_name", pending.deviceName)
            .put("platform", pending.platform)
            .put("public_key", pending.publicKey)
            .apply {
                if (pending.activationCode.isNotBlank()) put("activation_code", pending.activationCode)
            }
            .toString()
            .toByteArray(Charsets.UTF_8)

        require(pending.activationCode.isNotBlank() || !idToken.isNullOrBlank()) {
            "A fresh customer identity token is required to continue this enrollment."
        }
        val connection = enrollmentEndpoint().openConnection() as HttpsURLConnection
        try {
            connection.requestMethod = "POST"
            connection.connectTimeout = CONNECT_TIMEOUT_MS
            connection.readTimeout = READ_TIMEOUT_MS
            connection.instanceFollowRedirects = false
            connection.doOutput = true
            connection.setFixedLengthStreamingMode(body.size)
            connection.setRequestProperty("Accept", "application/json")
            connection.setRequestProperty("Content-Type", "application/json; charset=utf-8")
            connection.setRequestProperty("Idempotency-Key", pending.idempotencyKey)
            if (pending.activationCode.isBlank() && !idToken.isNullOrBlank()) {
                connection.setRequestProperty("Authorization", "Bearer $idToken")
            }
            connection.outputStream.use { it.write(body) }

            val status = connection.responseCode
            val stream = if (status in 200..299) connection.inputStream else connection.errorStream
            val responseText = stream?.use { it.readUtf8Limited(MAX_RESPONSE_BYTES) }.orEmpty()
            val json = runCatching { JSONObject(responseText) }.getOrElse {
                throw IOException("The ZEUS API returned an invalid response.")
            }
            if (status !in 200..299) {
                val error = json.optJSONObject("error")
                throw ClientEnrollmentException(
                    errorCode = error?.optString("code").orEmpty().ifBlank { "http_$status" },
                    message = error?.optString("message").orEmpty().ifBlank {
                        "Device activation failed (HTTP $status)."
                    },
                )
            }
            if (status != 202) {
                throw IOException("The ZEUS API returned an unexpected enrollment status.")
            }
            return JSONObject().put("device", json)
        } finally {
            body.fill(0)
            connection.disconnect()
        }
    }

    private fun enrollmentEndpoint(): java.net.URL {
        require(configuredBaseUrl.isNotBlank()) {
            "ZEUS_API_BASE_URL is not configured in this Android build."
        }
        val base = URI(configuredBaseUrl)
        require(
            base.scheme.equals("https", ignoreCase = true) &&
                !base.host.isNullOrBlank() &&
                base.userInfo == null &&
                base.query == null &&
                base.fragment == null
        ) {
            "The ZEUS API base URL must be a valid HTTPS URL."
        }
        return URI(configuredBaseUrl + "/v1/client/enroll").toURL()
    }

    private fun isActivationCodeShapeValid(code: String): Boolean =
        code.length == ACTIVATION_CODE_LENGTH &&
            code.all {
                it in 'A'..'Z' || it in 'a'..'z' || it in '0'..'9' || it == '-' || it == '_'
            }

    private data class PendingEnrollment(
        val activationCode: String,
        val deviceRef: String,
        val deviceName: String,
        val platform: String,
        val publicKey: String,
        val privateKey: String,
        val idempotencyKey: String,
    )

    private companion object {
        const val ACTIVATION_CODE_LENGTH = 43
        const val CONNECT_TIMEOUT_MS = 10_000
        const val READ_TIMEOUT_MS = 20_000
        const val PROVISIONING_TIMEOUT_MS = 90_000L
        const val PROVISIONING_POLL_INTERVAL_MS = 3_000L
        const val MAX_RESPONSE_BYTES = 32 * 1024
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
