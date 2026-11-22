namespace AuditVault;

public enum Outcome
{
    /// <summary>Stored. The message may be deleted from the queue.</summary>
    Recorded,
    /// <summary>Already stored earlier (redelivery). The message may be deleted, no new effect (R1, F3).</summary>
    Duplicate,
    /// <summary>Does not match the contract. NOT deleted: after maxReceiveCount the broker moves it to the DLQ (F4).</summary>
    Poison,
    /// <summary>Storage failed. NOT deleted: it is redelivered (F2, F6).</summary>
    Failed,
}

public sealed class MessageProcessor(IAuditStore store, TimeProvider clock, ILogger<MessageProcessor> log)
{
    public async Task<Outcome> HandleAsync(string body, CancellationToken ct)
    {
        if (!Envelope.TryParse(body, out var env, out var error) || env is null)
        {
            log.LogError("poison message left for DLQ: {Error}", error);
            return Outcome.Poison;
        }
        try
        {
            await store.RecordAsync(AuditEvent.From(env, clock.GetUtcNow()), ct);
            log.LogInformation("audit recorded messageId={MessageId} correlationId={CorrelationId} type={Type}", env.MessageId, env.CorrelationId, env.Type);
            return Outcome.Recorded;
        }
        catch (DuplicateMessageException)
        {
            log.LogInformation("duplicate ignored messageId={MessageId} correlationId={CorrelationId} type={Type}", env.MessageId, env.CorrelationId, env.Type);
            return Outcome.Duplicate;
        }
        catch (Exception ex) when (ex is not OperationCanceledException)
        {
            log.LogWarning(ex, "handling failed, will be redelivered messageId={MessageId} correlationId={CorrelationId}", env.MessageId, env.CorrelationId);
            return Outcome.Failed;
        }
    }
}
