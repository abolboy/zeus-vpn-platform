package com.zeusvpn.app.network

import java.util.ArrayDeque

/**
 * Converts monotonically increasing WireGuard byte counters into bounded
 * throughput samples. The timestamps must come from a monotonic clock.
 */
data class TrafficRateSample(
    val downloadBytesPerSecond: Double,
    val uploadBytesPerSecond: Double,
)

class TrafficRateSampler(
    private val maxSamples: Int = DEFAULT_MAX_SAMPLES,
    private val maxSampleGapMillis: Long = DEFAULT_MAX_SAMPLE_GAP_MILLIS,
) {
    private data class Snapshot(
        val receivedBytes: Long,
        val sentBytes: Long,
        val timestampMillis: Long,
    )

    private val samples = ArrayDeque<TrafficRateSample>()
    private var previous: Snapshot? = null

    init {
        require(maxSamples >= 2) { "At least two samples are required for a useful chart." }
        require(maxSampleGapMillis > 0L) { "The maximum sample gap must be positive." }
    }

    @Synchronized
    fun addSnapshot(
        receivedBytes: Long,
        sentBytes: Long,
        timestampMillis: Long,
    ): List<TrafficRateSample> {
        require(receivedBytes >= 0L && sentBytes >= 0L) { "Byte counters cannot be negative." }
        require(timestampMillis >= 0L) { "The monotonic timestamp cannot be negative." }

        val current = Snapshot(receivedBytes, sentBytes, timestampMillis)
        val prior = previous
        previous = current

        if (prior == null) return samples.toList()

        val elapsedMillis = current.timestampMillis - prior.timestampMillis
        if (elapsedMillis <= 0L || elapsedMillis > maxSampleGapMillis) {
            samples.clear()
            return emptyList()
        }

        val receivedDelta = (current.receivedBytes - prior.receivedBytes).coerceAtLeast(0L)
        val sentDelta = (current.sentBytes - prior.sentBytes).coerceAtLeast(0L)
        val sample = TrafficRateSample(
            downloadBytesPerSecond = receivedDelta.toDouble() * 1_000.0 / elapsedMillis,
            uploadBytesPerSecond = sentDelta.toDouble() * 1_000.0 / elapsedMillis,
        )

        samples.addLast(sample)
        while (samples.size > maxSamples) samples.removeFirst()
        return samples.toList()
    }

    @Synchronized
    fun reset() {
        previous = null
        samples.clear()
    }

    private companion object {
        const val DEFAULT_MAX_SAMPLES = 24
        const val DEFAULT_MAX_SAMPLE_GAP_MILLIS = 15_000L
    }
}
