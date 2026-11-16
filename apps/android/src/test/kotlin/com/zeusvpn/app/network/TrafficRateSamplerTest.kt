package com.zeusvpn.app.network

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class TrafficRateSamplerTest {
    @Test
    fun firstSnapshotOnlyEstablishesBaseline() {
        val sampler = TrafficRateSampler()

        val result = sampler.addSnapshot(
            receivedBytes = 10_000L,
            sentBytes = 4_000L,
            timestampMillis = 1_000L,
        )

        assertTrue(result.isEmpty())
    }

    @Test
    fun calculatesDownloadAndUploadRatesFromCounterDeltas() {
        val sampler = TrafficRateSampler()
        sampler.addSnapshot(10_000L, 4_000L, 1_000L)

        val result = sampler.addSnapshot(30_000L, 9_000L, 3_000L)

        assertEquals(1, result.size)
        assertEquals(10_000.0, result.single().downloadBytesPerSecond, 0.001)
        assertEquals(2_500.0, result.single().uploadBytesPerSecond, 0.001)
    }

    @Test
    fun counterResetDoesNotCreateNegativeOrSpikedRate() {
        val sampler = TrafficRateSampler()
        sampler.addSnapshot(10_000L, 10_000L, 1_000L)

        val result = sampler.addSnapshot(100L, 15_000L, 2_000L).single()

        assertEquals(0.0, result.downloadBytesPerSecond, 0.001)
        assertEquals(5_000.0, result.uploadBytesPerSecond, 0.001)
    }

    @Test
    fun longSamplingGapClearsHistoryAndStartsANewBaseline() {
        val sampler = TrafficRateSampler()
        sampler.addSnapshot(10_000L, 10_000L, 1_000L)
        sampler.addSnapshot(20_000L, 20_000L, 2_000L)

        val afterGap = sampler.addSnapshot(100_000L, 100_000L, 30_000L)
        assertTrue(afterGap.isEmpty())

        val next = sampler.addSnapshot(110_000L, 105_000L, 31_000L).single()
        assertEquals(10_000.0, next.downloadBytesPerSecond, 0.001)
        assertEquals(5_000.0, next.uploadBytesPerSecond, 0.001)
    }

    @Test
    fun historyIsBoundedToConfiguredCapacity() {
        val sampler = TrafficRateSampler(maxSamples = 3)
        sampler.addSnapshot(0L, 0L, 1_000L)

        repeat(5) { index ->
            val bytes = (index + 1L) * 1_000L
            sampler.addSnapshot(bytes, bytes / 2L, 1_000L + (index + 1L) * 1_000L)
        }

        assertEquals(3, sampler.addSnapshot(6_000L, 3_000L, 7_000L).size)
    }
}
