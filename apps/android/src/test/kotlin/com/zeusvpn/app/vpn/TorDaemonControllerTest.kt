package com.zeusvpn.app.vpn

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.async
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlin.coroutines.CoroutineContext

class TorDaemonControllerTest {
    @Test
    fun recognizesEstablishedTorCircuitStatus() {
        assertEquals(true, isTorCircuitEstablished("1"))
        assertEquals(true, isTorCircuitEstablished("\"1\""))
    }

    @Test
    fun rejectsUnestablishedOrUnknownTorCircuitStatus() {
        assertEquals(false, isTorCircuitEstablished("0"))
        assertEquals(false, isTorCircuitEstablished(null))
        assertEquals(false, isTorCircuitEstablished("unknown"))
    }

    @Test
    fun parsesQuotedIpv4LoopbackListener() {
        assertEquals(
            TorSocksEndpoint("127.0.0.1", 9050),
            parseTorSocksEndpoint("\"127.0.0.1:9050\""),
        )
    }

    @Test
    fun parsesBracketedIpv6LoopbackListener() {
        assertEquals(
            TorSocksEndpoint("::1", 9150),
            parseTorSocksEndpoint("\"[::1]:9150\""),
        )
    }

    @Test
    fun rejectsNonLoopbackListenerAddresses() {
        assertNull(parseTorSocksEndpoint("\"0.0.0.0:9050\""))
        assertNull(parseTorSocksEndpoint("\"192.168.1.20:9050\""))
    }

    @Test
    fun rejectsMissingMalformedAndOutOfRangePorts() {
        assertNull(parseTorSocksEndpoint(null))
        assertNull(parseTorSocksEndpoint(""))
        assertNull(parseTorSocksEndpoint("\"127.0.0.1\""))
        assertNull(parseTorSocksEndpoint("\"127.0.0.1:0\""))
        assertNull(parseTorSocksEndpoint("\"127.0.0.1:65536\""))
        assertNull(parseTorSocksEndpoint("\"127.0.0.1:not-a-port\""))
    }

    @Test
    fun stopDuringStartupReleasesBindingAndEndsStopped() = runBlocking {
        val binding = FakeTorServiceBinding(circuitStatus = "0", socksEndpoint = null)
        val controller = TorDaemonController(binding, DirectDispatcher)
        val starting = async { runCatching { controller.start() } }

        withTimeout(2_000) { binding.bound.await() }
        controller.stop()

        assertTrue(starting.await().isFailure)
        assertEquals(TorDaemonState.STOPPED, controller.state.value)
        assertEquals(1, binding.bindCalls)
        assertEquals(1, binding.unbindCalls)
    }

    @Test
    fun stopAfterCircuitEstablishedButBeforeSocksEndpointReleasesBinding() = runBlocking {
        val binding = FakeTorServiceBinding(circuitStatus = "1", socksEndpoint = null)
        val controller = TorDaemonController(binding, DirectDispatcher)
        val starting = async { runCatching { controller.start() } }

        withTimeout(2_000) { binding.bound.await() }
        withTimeout(2_000) { binding.socksPollStarted.await() }
        controller.stop()

        assertTrue(starting.await().isFailure)
        assertEquals(TorDaemonState.STOPPED, controller.state.value)
        assertEquals(1, binding.bindCalls)
        assertEquals(1, binding.unbindCalls)
    }

    @Test
    fun cancellationDuringStartupReleasesBinding() = runBlocking {
        val binding = FakeTorServiceBinding(circuitStatus = "0", socksEndpoint = null)
        val controller = TorDaemonController(binding, DirectDispatcher)
        val starting = async { controller.start() }

        withTimeout(2_000) { binding.bound.await() }
        starting.cancelAndJoin()

        assertEquals(TorDaemonState.STOPPED, controller.state.value)
        assertEquals(1, binding.unbindCalls)
    }

    @Test
    fun serviceDisconnectionDuringStartupFailsAndReleasesBinding() = runBlocking {
        val binding = FakeTorServiceBinding(circuitStatus = "0", socksEndpoint = null)
        val controller = TorDaemonController(binding, DirectDispatcher)
        val starting = async { runCatching { controller.start() } }

        withTimeout(2_000) { binding.bound.await() }
        binding.listener?.onDisconnected()

        assertTrue(starting.await().isFailure)
        assertEquals(TorDaemonState.FAILED, controller.state.value)
        assertEquals(1, binding.unbindCalls)
    }

    @Test
    fun serviceErrorDuringStartupFailsImmediatelyAndReleasesBinding() = runBlocking {
        val binding = FakeTorServiceBinding(circuitStatus = "0", socksEndpoint = null)
        val controller = TorDaemonController(binding, DirectDispatcher)
        val starting = async { runCatching { controller.start() } }

        withTimeout(2_000) { binding.bound.await() }
        binding.emitError("Tor native startup failed")

        assertTrue(starting.await().isFailure)
        assertEquals(TorDaemonState.FAILED, controller.state.value)
        assertEquals(1, binding.unbindCalls)
    }

    @Test
    fun unexpectedTorServiceStopDuringStartupFailsAndReleasesBinding() = runBlocking {
        val binding = FakeTorServiceBinding(circuitStatus = "0", socksEndpoint = null)
        val controller = TorDaemonController(binding, DirectDispatcher)
        val starting = async { runCatching { controller.start() } }

        withTimeout(2_000) { binding.bound.await() }
        binding.emitServiceStopped("STOPPING")

        assertTrue(starting.await().isFailure)
        assertEquals(TorDaemonState.FAILED, controller.state.value)
        assertEquals(1, binding.unbindCalls)
    }

    @Test
    fun unexpectedTorServiceStopAfterReadinessMarksControllerFailed() = runBlocking {
        val binding = FakeTorServiceBinding(circuitStatus = "1", socksEndpoint = "\"127.0.0.1:9050\"")
        val controller = TorDaemonController(binding, DirectDispatcher)

        assertEquals(TorSocksEndpoint("127.0.0.1", 9050), controller.start())
        binding.listener?.onServiceStopped("STOPPING")

        assertEquals(TorDaemonState.FAILED, controller.state.value)
        controller.stop()
        assertEquals(TorDaemonState.STOPPED, controller.state.value)
    }

    @Test
    fun stopRetriesTransientBindingCleanupFailure() = runBlocking {
        val binding = FakeTorServiceBinding(
            circuitStatus = "1",
            socksEndpoint = "\"127.0.0.1:9050\"",
            unbindFailuresRemaining = 1,
            unbindFailure = IllegalStateException("unbind failed"),
        )
        val controller = TorDaemonController(binding, DirectDispatcher)
        controller.start()

        val error = runCatching { controller.stop() }.exceptionOrNull()

        assertTrue(error is TorStartupException)
        assertEquals("Failed to release Tor service binding", error?.message)
        val causeMessages = generateSequence(error) { current -> current.cause }
            .mapNotNull { it.message }
            .toList()
        assertTrue(causeMessages.toString(), "unbind failed" in causeMessages)
        assertEquals(TorDaemonState.FAILED, controller.state.value)
        assertEquals(1, binding.unbindCalls)

        controller.stop()

        assertEquals(2, binding.unbindCalls)
        assertEquals(TorDaemonState.STOPPED, controller.state.value)
    }

    @Test
    fun repeatedStartAndStopRebindsExactlyOncePerSession() = runBlocking {
        val binding = FakeTorServiceBinding(circuitStatus = "1", socksEndpoint = "\"127.0.0.1:9050\"")
        val controller = TorDaemonController(binding, DirectDispatcher)
        val expected = TorSocksEndpoint("127.0.0.1", 9050)

        assertEquals(expected, controller.start())
        assertEquals(TorDaemonState.READY, controller.state.value)
        controller.stop()
        assertEquals(expected, controller.start())
        controller.stop()

        assertEquals(2, binding.bindCalls)
        assertEquals(2, binding.unbindCalls)
        assertEquals(TorDaemonState.STOPPED, controller.state.value)
    }

    private class FakeTorServiceBinding(
        private val circuitStatus: String,
        private val socksEndpoint: String?,
        private var unbindFailuresRemaining: Int = 0,
        private val unbindFailure: Exception? = null,
    ) : TorServiceBinding {
        val bound = CompletableDeferred<Unit>()
        val socksPollStarted = CompletableDeferred<Unit>()
        var bindCalls = 0
            private set
        var unbindCalls = 0
            private set
        var listener: TorServiceBinding.Listener? = null
            private set

        override fun bind(listener: TorServiceBinding.Listener): Boolean {
            this.listener = listener
            bindCalls += 1
            listener.onConnected(object : TorServiceApi {
                override fun getInfo(key: String): String? = when (key) {
                    "status/circuit-established" -> circuitStatus
                    "net/listeners/socks" -> {
                        socksPollStarted.complete(Unit)
                        socksEndpoint
                    }
                    else -> null
                }
            })
            bound.complete(Unit)
            return true
        }

        override fun unbind() {
            unbindCalls += 1
            if (unbindFailuresRemaining > 0) {
                unbindFailuresRemaining -= 1
                throw unbindFailure ?: IllegalStateException("unbind failed")
            }
        }

        fun emitError(message: String) {
            listener?.onError(message)
        }

        fun emitServiceStopped(status: String) {
            listener?.onServiceStopped(status)
        }
    }

    private object DirectDispatcher : CoroutineDispatcher() {
        override fun isDispatchNeeded(context: CoroutineContext): Boolean = false

        override fun dispatch(context: CoroutineContext, block: Runnable) {
            block.run()
        }
    }
}
