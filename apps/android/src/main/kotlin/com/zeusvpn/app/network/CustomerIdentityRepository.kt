package com.zeusvpn.app.network

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.util.Base64
import com.zeusvpn.app.BuildConfig
import com.zeusvpn.app.data.EncryptedWireGuardConfigStore
import kotlinx.coroutines.suspendCancellableCoroutine
import net.openid.appauth.AuthState
import net.openid.appauth.AuthorizationException
import net.openid.appauth.AuthorizationRequest
import net.openid.appauth.AuthorizationResponse
import net.openid.appauth.AuthorizationService
import net.openid.appauth.AuthorizationServiceConfiguration
import net.openid.appauth.ResponseTypeValues
import java.io.IOException
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

/**
 * Native OIDC authorization-code + PKCE flow.
 * AppAuth uses the system browser/Custom Tab. AuthState (including refresh credentials)
 * is encrypted with an AES-GCM key protected by Android Keystore.
 */
class CustomerIdentityRepository(context: Context) {
    private val appContext = context.applicationContext
    private val encryptedStore = EncryptedWireGuardConfigStore(appContext)
    private val issuer = BuildConfig.ZEUS_OIDC_ISSUER.trim().trimEnd('/')
    private val clientId = BuildConfig.ZEUS_OIDC_CLIENT_ID.trim()
    private val redirectUri = BuildConfig.ZEUS_OIDC_REDIRECT_URI.trim()

    val isConfigured: Boolean
        get() = issuer.startsWith("https://") && issuer.length <= 512 &&
            clientId.isNotBlank() && clientId.length <= 256 &&
            redirectUri == DEFAULT_REDIRECT_URI

    fun hasStoredSession(): Boolean = runCatching {
        val payload = encryptedStore.readCustomerSession() ?: return false
        try {
            AuthState.jsonDeserialize(String(payload, Charsets.UTF_8)).isAuthorized
        } finally {
            payload.fill(0)
        }
    }.getOrDefault(false)

    suspend fun createAuthorizationIntent(): Intent {
        require(isConfigured) { "Customer OIDC settings are not configured in this build." }
        val configuration = fetchServiceConfiguration()
        val request = AuthorizationRequest.Builder(
            configuration, clientId, ResponseTypeValues.CODE, Uri.parse(redirectUri),
        ).setScope("openid email profile")
            .setUiLocales("fa en")
            .build()

        val service = AuthorizationService(appContext)
        return try {
            service.getAuthorizationRequestIntent(request)
        } finally {
            service.dispose()
        }
    }

    suspend fun completeAuthorization(data: Intent?): String {
        val authorization = AuthorizationResponse.fromIntent(data)
            ?: throw IOException("The identity provider returned no authorization response.")
        val authorizationError = AuthorizationException.fromIntent(data)
        if (authorizationError != null) {
            throw IOException("Customer sign-in failed: ${authorizationError.error ?: "authorization_error"}")
        }

        val service = AuthorizationService(appContext)
        val authState = AuthState(authorization)
        try {
            val tokenResponse = suspendCancellableCoroutine { continuation ->
                val completed = AtomicBoolean(false)
                service.performTokenRequest(authorization.createTokenExchangeRequest()) { token, tokenError ->
                    if (!completed.compareAndSet(false, true)) return@performTokenRequest
                    if (token != null) {
                        authState.update(token, null)
                        continuation.resume(token)
                    } else {
                        authState.update(null, tokenError)
                        continuation.resumeWithException(
                            IOException("Customer token exchange failed: ${tokenError?.error ?: "token_exchange_error"}")
                        )
                    }
                }
                continuation.invokeOnCancellation { completed.set(true) }
            }
            val idToken = tokenResponse.idToken?.takeIf { it.isNotBlank() }
                ?: throw IOException("The identity provider did not issue an OpenID ID token.")
            requireTokenExpiry(idToken)
            persist(authState)
            return idToken
        } finally {
            service.dispose()
        }
    }

    suspend fun freshIdToken(): String {
        val payload = encryptedStore.readCustomerSession()
            ?: throw IOException("Sign in to your ZEUS account first.")
        val authState = try {
            AuthState.jsonDeserialize(String(payload, Charsets.UTF_8))
        } finally {
            payload.fill(0)
        }
        val service = AuthorizationService(appContext)
        try {
            val idToken = suspendCancellableCoroutine { continuation ->
                val completed = AtomicBoolean(false)
                authState.performActionWithFreshTokens(service) { _, token, tokenError ->
                    if (!completed.compareAndSet(false, true)) return@performActionWithFreshTokens
                    if (tokenError != null || token.isNullOrBlank()) {
                        continuation.resumeWithException(IOException("Sign in again to continue."))
                    } else {
                        try {
                            requireTokenExpiry(token)
                            continuation.resume(token)
                        } catch (error: Exception) {
                            continuation.resumeWithException(error)
                        }
                    }
                }
                continuation.invokeOnCancellation { completed.set(true) }
            }
            persist(authState)
            return idToken
        } finally {
            service.dispose()
        }
    }

    fun signOutLocally() {
        encryptedStore.clearCustomerSession()
    }

    private suspend fun fetchServiceConfiguration(): AuthorizationServiceConfiguration =
        suspendCancellableCoroutine { continuation ->
            AuthorizationServiceConfiguration.fetchFromIssuer(Uri.parse(issuer)) { configuration, exception ->
                if (configuration != null) {
                    continuation.resume(configuration)
                } else {
                    continuation.resumeWithException(
                        IOException("Could not load the ZEUS identity provider: ${exception?.error ?: "discovery_failed"}")
                    )
                }
            }
        }

    private fun persist(authState: AuthState) {
        encryptedStore.saveCustomerSession(authState.jsonSerializeString().toByteArray(Charsets.UTF_8))
    }

    private fun requireTokenExpiry(idToken: String) {
        val parts = idToken.split('.')
        require(parts.size == 3) { "The identity provider returned an invalid ID token." }
        val payload = try {
            Base64.decode(parts[1], Base64.URL_SAFE or Base64.NO_WRAP or Base64.NO_PADDING)
        } catch (_: IllegalArgumentException) {
            throw IOException("The identity provider returned an invalid ID token.")
        }
        try {
            val expiry = org.json.JSONObject(String(payload, Charsets.UTF_8)).optLong("exp", 0L)
            if (expiry <= System.currentTimeMillis() / 1000L + 30L) {
                throw IOException("Customer sign-in expired. Sign in again to continue.")
            }
        } finally {
            payload.fill(0)
        }
    }

    private companion object {
        const val DEFAULT_REDIRECT_URI = "com.zeusvpn.app:/oauth2redirect"
    }
}
