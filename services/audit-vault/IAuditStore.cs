namespace AuditVault;

/// <summary>Thrown when a message with this messageId was already recorded (redelivery, F3).</summary>
public sealed class DuplicateMessageException(string messageId)
    : Exception($"message {messageId} already processed");

public interface IAuditStore
{
    /// <summary>
    /// Records the event and marks its messageId as processed in one atomic unit of work (R1).
    /// Throws <see cref="DuplicateMessageException"/> when the messageId was seen before.
    /// </summary>
    Task RecordAsync(AuditEvent e, CancellationToken ct);

    Task<CallTrail?> GetTrailAsync(string callId, CancellationToken ct);

    /// <summary>Most recently updated calls first (summaries only).</summary>
    Task<IReadOnlyList<CallTrail>> ListRecentAsync(int limit, CancellationToken ct);
}
