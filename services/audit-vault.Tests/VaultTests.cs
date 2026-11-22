using AuditVault;
using Microsoft.Extensions.Logging.Abstractions;
using Xunit;

namespace AuditVault.Tests;

public class EnvelopeTests
{
    [Fact]
    public void Accepts_a_valid_envelope()
    {
        Assert.True(Envelope.TryParse(T.Msg("m1", "CallStarted", "c1", "{}"), out var e, out _));
        Assert.Equal("c1", e!.CorrelationId);
        Assert.Equal(1, e.Version);
    }

    [Theory]
    [InlineData("not json")]
    [InlineData("[]")]
    [InlineData("""{"messageId":"","correlationId":"c","type":"t","version":1,"occurredAt":"2026-11-22T10:00:00Z","payload":{}}""")]
    [InlineData("""{"messageId":"m","correlationId":"c","type":"t","version":0,"occurredAt":"2026-11-22T10:00:00Z","payload":{}}""")]
    [InlineData("""{"messageId":"m","correlationId":"c","type":"t","version":1,"payload":{}}""")]
    [InlineData("""{"messageId":"m","correlationId":"c","type":"t","version":1,"occurredAt":"2026-11-22T10:00:00Z","payload":"x"}""")]
    public void Rejects_poison(string body) => Assert.False(Envelope.TryParse(body, out _, out var err) || err == "");
}

public class ProcessorTests
{
    [Fact]
    public async Task Records_a_valid_message()
    {
        var store = new FakeStore();
        var r = await T.Processor(store).HandleAsync(T.Msg("m1", "CallStarted", "c1", "{}"), default);
        Assert.Equal(Outcome.Recorded, r);
        Assert.Single(store.Calls["c1"]);
    }

    [Fact] // F3: the same messageId five times produces a single effect (R1)
    public async Task Same_messageId_five_times_has_one_effect()
    {
        var store = new FakeStore();
        var p = T.Processor(store);
        var body = T.Msg("dup", "ParticipantJoined", "c1", """{"callId":"c1","userId":"ana"}""");
        var results = new List<Outcome>();
        for (var i = 0; i < 5; i++) results.Add(await p.HandleAsync(body, default));
        Assert.Equal(Outcome.Recorded, results[0]);
        Assert.All(results.Skip(1), r => Assert.Equal(Outcome.Duplicate, r));
        Assert.Single(store.Calls["c1"]);
    }

    [Fact] // F4
    public async Task Poison_is_not_recorded()
    {
        var store = new FakeStore();
        Assert.Equal(Outcome.Poison, await T.Processor(store).HandleAsync("{broken", default));
        Assert.Empty(store.Calls);
    }

    [Fact] // F2/F6: storage failure -> redelivery later, effect applied once
    public async Task Storage_failure_is_retryable()
    {
        var store = new FakeStore { FailNext = 1 };
        var p = T.Processor(store);
        var body = T.Msg("m1", "CallEnded", "c1", """{"callId":"c1","durationMs":10,"reason":"last-left"}""");
        Assert.Equal(Outcome.Failed, await p.HandleAsync(body, default));
        Assert.Equal(Outcome.Recorded, await p.HandleAsync(body, default));
        Assert.Single(store.Calls["c1"]);
    }
}

public class ConsumerTests
{
    private static QueueConsumer Consumer(FakeQueue q, FakeStore s) => new(q, T.Processor(s), NullLogger<QueueConsumer>.Instance)
    {
        FirstBackoff = TimeSpan.FromMilliseconds(5),
        MaxBackoff = TimeSpan.FromMilliseconds(20),
    };

    private static QueueMessage M(string receipt, string body) => new(body, receipt);

    [Fact]
    public async Task Deletes_recorded_and_duplicate_but_keeps_poison_and_failed()
    {
        var store = new FakeStore();
        var ok = T.Msg("m1", "CallStarted", "c1", "{}");
        var q = new FakeQueue().Then(() => [M("r-ok", ok), M("r-dup", ok), M("r-poison", "{x")]);
        using var stop = new CancellationTokenSource();
        q.StopWhenDrained = stop;
        await Consumer(q, store).RunAsync(stop.Token);
        Assert.Equal(["r-ok", "r-dup"], q.Deleted);
        Assert.Single(store.Calls["c1"]);
    }

    [Fact] // R6 / F6: the loop survives broker errors and consumes again by itself
    public async Task Survives_broker_outage()
    {
        var store = new FakeStore();
        var body = T.Msg("m1", "CallStarted", "c1", "{}");
        var q = new FakeQueue()
            .Then(() => throw new IOException("broker down"))
            .Then(() => throw new IOException("broker down"))
            .Then(() => [M("r1", body)]);
        using var stop = new CancellationTokenSource();
        q.StopWhenDrained = stop;
        await Consumer(q, store).RunAsync(stop.Token);
        Assert.Equal(["r1"], q.Deleted);
        Assert.Single(store.Calls["c1"]);
    }

    [Fact] // F2: a failed message is not deleted; redelivery is applied exactly once
    public async Task Failed_message_is_redelivered_and_applied_once()
    {
        var store = new FakeStore { FailNext = 1 };
        var body = T.Msg("m1", "CallStarted", "c1", "{}");
        var q = new FakeQueue().Then(() => [M("r1", body)]).Then(() => [M("r2", body)]);
        using var stop = new CancellationTokenSource();
        q.StopWhenDrained = stop;
        await Consumer(q, store).RunAsync(stop.Token);
        Assert.Equal(["r2"], q.Deleted);
        Assert.Single(store.Calls["c1"]);
    }
}

public class TrailTests
{
    private static AuditEvent Ev(string id, string type, string payload, string at) =>
        AuditEvent.From(Envelope.TryParse(T.Msg(id, type, "c1", payload, at), out var e, out _) ? e! : throw new Exception("bad"), DateTimeOffset.UnixEpoch);

    [Fact]
    public void Builds_an_ended_call_regardless_of_arrival_order()
    {
        var events = new[]
        {
            Ev("3", "CallEnded", """{"callId":"c1","durationMs":9000,"reason":"last-left"}""", "2026-11-22T10:00:09Z"),
            Ev("1", "CallStarted", """{"callId":"c1","participants":["ana","bob"],"capacity":2}""", "2026-11-22T10:00:00Z"),
            Ev("2", "ParticipantJoined", """{"callId":"c1","userId":"carl"}""", "2026-11-22T10:00:05Z"),
        };
        var t = CallTrail.Build("c1", events);
        Assert.Equal(CallStatus.Ended, t.Status);
        Assert.Equal(["ana", "bob", "carl"], t.Participants);
        Assert.Equal(9000, t.DurationMs);
        Assert.Equal("last-left", t.Reason);
        Assert.Equal(["1", "2", "3"], t.Events.Select(e => e.MessageId));
    }

    [Fact]
    public void Failed_call_reports_the_reason()
    {
        var t = CallTrail.Build("c1", [Ev("1", "CallFailed", """{"callId":"c1","reason":"relay timeout"}""", "2026-11-22T10:00:03Z")]);
        Assert.Equal(CallStatus.Failed, t.Status);
        Assert.Equal("relay timeout", t.Reason);
    }

    [Fact]
    public void Record_has_a_stable_sha256_of_the_original_message()
    {
        var a = Ev("1", "CallStarted", "{}", "2026-11-22T10:00:00Z");
        var b = Ev("1", "CallStarted", "{}", "2026-11-22T10:00:00Z");
        Assert.Equal(64, a.Sha256.Length);
        Assert.Equal(a.Sha256, b.Sha256);
    }
}

public class ApiTests
{
    [Theory]
    [InlineData("Bearer secret", true)]
    [InlineData("bearer secret", true)]
    [InlineData("Bearer wrong", false)]
    [InlineData("secret", false)]
    [InlineData("", false)]
    public void Token_check(string header, bool ok) => Assert.Equal(ok, AuditApi.TokenOk(header, "secret"));

    [Fact]
    public void Config_requires_queue_and_tables()
    {
        var ex = Assert.Throws<InvalidOperationException>(() => VaultConfig.Load(_ => null));
        Assert.Contains("CALL_EVENTS_QUEUE", ex.Message);
        var cfg = VaultConfig.Load(k => k switch { "CALL_EVENTS_QUEUE" => "q", "AUDIT_TABLE" => "a", "PROCESSED_TABLE" => "p", _ => null });
        Assert.Equal("us-east-1", cfg.Region);
        Assert.Null(cfg.ApiToken);
    }
}
