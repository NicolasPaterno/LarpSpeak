using AuditVault;
using Microsoft.Extensions.Logging.Abstractions;

namespace AuditVault.Tests;

public static class T
{
    public static string Msg(string id, string type, string call, string payload, string at = "2026-11-22T10:00:00Z") =>
        $$"""{"messageId":"{{id}}","correlationId":"{{call}}","type":"{{type}}","version":1,"occurredAt":"{{at}}","payload":{{payload}}}""";

    public static MessageProcessor Processor(IAuditStore s) => new(s, TimeProvider.System, NullLogger<MessageProcessor>.Instance);
}

/// <summary>In-memory store with the same contract as DynamoAuditStore (duplicate on repeated messageId).</summary>
public sealed class FakeStore : IAuditStore
{
    public Dictionary<string, List<AuditEvent>> Calls { get; } = new();
    public HashSet<string> Processed { get; } = new();
    public int FailNext { get; set; }

    public Task RecordAsync(AuditEvent e, CancellationToken ct)
    {
        if (FailNext > 0) { FailNext--; throw new InvalidOperationException("dynamo down"); }
        if (!Processed.Add(e.MessageId)) throw new DuplicateMessageException(e.MessageId);
        if (!Calls.TryGetValue(e.CallId, out var l)) Calls[e.CallId] = l = new();
        l.Add(e);
        return Task.CompletedTask;
    }

    public Task<CallTrail?> GetTrailAsync(string callId, CancellationToken ct) =>
        Task.FromResult(Calls.TryGetValue(callId, out var l) ? CallTrail.Build(callId, l) : null);

    public Task<IReadOnlyList<CallTrail>> ListRecentAsync(int limit, CancellationToken ct) =>
        Task.FromResult<IReadOnlyList<CallTrail>>(Calls.Select(c => CallTrail.Build(c.Key, c.Value).Summary()).Take(limit).ToList());
}

public sealed class FakeQueue : IMessageQueue
{
    private readonly Queue<Func<IReadOnlyList<QueueMessage>>> _script = new();
    public List<string> Deleted { get; } = new();
    public CancellationTokenSource? StopWhenDrained { get; set; }

    public FakeQueue Then(Func<IReadOnlyList<QueueMessage>> step) { _script.Enqueue(step); return this; }

    public async Task<IReadOnlyList<QueueMessage>> ReceiveAsync(CancellationToken ct)
    {
        if (_script.Count == 0)
        {
            StopWhenDrained?.Cancel();
            await Task.Delay(10, CancellationToken.None);
            return [];
        }
        return _script.Dequeue()();
    }

    public Task DeleteAsync(QueueMessage m, CancellationToken ct) { Deleted.Add(m.Receipt); return Task.CompletedTask; }
}
