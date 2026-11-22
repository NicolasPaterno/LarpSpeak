using Amazon.SQS;
using Amazon.SQS.Model;

namespace AuditVault;

public sealed record QueueMessage(string Body, string Receipt);

/// <summary>The part of SQS the consumer needs; a fake replaces it in tests.</summary>
public interface IMessageQueue
{
    Task<IReadOnlyList<QueueMessage>> ReceiveAsync(CancellationToken ct);
    Task DeleteAsync(QueueMessage m, CancellationToken ct);
}

public sealed class SqsQueue(IAmazonSQS sqs, string queueName) : IMessageQueue
{
    private string? _url;

    // Resolved lazily and retried on the next call, so a broker that is down at startup is not fatal (R6).
    private async Task<string> UrlAsync(CancellationToken ct) =>
        _url ??= (await sqs.GetQueueUrlAsync(queueName, ct)).QueueUrl;

    public async Task<IReadOnlyList<QueueMessage>> ReceiveAsync(CancellationToken ct)
    {
        var res = await sqs.ReceiveMessageAsync(new ReceiveMessageRequest
        {
            QueueUrl = await UrlAsync(ct),
            MaxNumberOfMessages = 10,
            WaitTimeSeconds = 10,
        }, ct);
        return (res.Messages ?? []).Select(m => new QueueMessage(m.Body, m.ReceiptHandle)).ToList();
    }

    public async Task DeleteAsync(QueueMessage m, CancellationToken ct) =>
        await sqs.DeleteMessageAsync(await UrlAsync(ct), m.Receipt, ct);
}

/// <summary>
/// Long-polls the queue until the host stops. A message is deleted only after it was
/// stored (or recognised as a duplicate); anything else is redelivered and, after
/// maxReceiveCount attempts, moved to the DLQ (R1, F2, F4). Broker errors never end the
/// loop: it backs off and keeps trying, so the service survives a broker outage without
/// a manual restart (R6, F6).
/// </summary>
public sealed class QueueConsumer(IMessageQueue queue, MessageProcessor processor, ILogger<QueueConsumer> log)
{
    public TimeSpan FirstBackoff { get; init; } = TimeSpan.FromMilliseconds(250);
    public TimeSpan MaxBackoff { get; init; } = TimeSpan.FromSeconds(5);

    public async Task RunAsync(CancellationToken stop)
    {
        // Graceful stop: stopping ends the loop between polls, but a poll already in flight
        // is not cancelled. The broker may have handed it messages; aborting would hide them
        // for the whole visibility timeout. They are handled and deleted before returning.
        var ct = CancellationToken.None;
        var wait = TimeSpan.Zero;
        while (!stop.IsCancellationRequested)
        {
            IReadOnlyList<QueueMessage> batch;
            try
            {
                batch = await queue.ReceiveAsync(ct);
                wait = TimeSpan.Zero;
            }
            catch (Exception ex)
            {
                log.LogWarning("broker unreachable, retrying: {Error}", ex.Message);
                wait = Next(wait);
                await DelayAsync(wait, stop);
                continue;
            }

            foreach (var m in batch)
            {
                var outcome = await processor.HandleAsync(m.Body, ct);
                if (outcome is Outcome.Poison or Outcome.Failed) continue;
                try
                {
                    await queue.DeleteAsync(m, ct);
                }
                catch (Exception ex)
                {
                    log.LogWarning("delete failed, message will be redelivered and deduplicated: {Error}", ex.Message);
                }
            }
        }
    }

    private TimeSpan Next(TimeSpan current) =>
        TimeSpan.FromTicks(Math.Min(Math.Max(current.Ticks * 2, FirstBackoff.Ticks), MaxBackoff.Ticks));

    private static async Task DelayAsync(TimeSpan d, CancellationToken stop)
    {
        try { await Task.Delay(d, stop); } catch (OperationCanceledException) { }
    }
}

public sealed class ConsumerService(QueueConsumer consumer) : BackgroundService
{
    protected override Task ExecuteAsync(CancellationToken stoppingToken) => consumer.RunAsync(stoppingToken);
}
