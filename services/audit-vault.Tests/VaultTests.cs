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

    [Theory] // Go marshals time.Time as RFC 3339 with up to 9 fractional digits
    [InlineData("2026-11-22T19:54:11.443817800Z")]
    [InlineData("2026-11-22T19:54:11.4Z")]
    [InlineData("2026-11-22T16:54:11.443817-03:00")]
    public void Accepts_the_timestamps_the_go_services_write(string at)
    {
        Assert.True(Envelope.TryParse(T.Msg("m1", "CallStarted", "c1", "{}", at), out var e, out var err), err);
        Assert.Equal(new DateTimeOffset(2026, 11, 22, 19, 54, 11, TimeSpan.Zero).Date, e!.OccurredAt.UtcDateTime.Date);
    }

    [Fact] // the vault stores times with "O" and reads them back with Parse: no precision or offset surprises
    public void Stored_timestamps_round_trip()
    {
        Envelope.TryParse(T.Msg("m1", "CallStarted", "c1", "{}", "2026-11-22T19:54:11.443817800Z"), out var e, out _);
        var s = e!.OccurredAt.ToString("O", System.Globalization.CultureInfo.InvariantCulture);
        Assert.Equal(e.OccurredAt, DateTimeOffset.Parse(s, System.Globalization.CultureInfo.InvariantCulture));
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
    public void Present_tracks_departures_while_participants_keeps_everyone()
    {
        var t = CallTrail.Build("c1",
        [
            Ev("1", "CallStarted", """{"callId":"c1","participants":["ana","bob"],"capacity":3}""", "2026-11-22T10:00:00Z"),
            Ev("2", "ParticipantJoined", """{"callId":"c1","userId":"carl"}""", "2026-11-22T10:00:01Z"),
            Ev("3", "ParticipantLeft", """{"callId":"c1","userId":"bob"}""", "2026-11-22T10:00:02Z"),
            Ev("4", "ParticipantTimedOut", """{"callId":"c1","userId":"carl"}""", "2026-11-22T10:00:03Z"),
            Ev("5", "ParticipantJoined", """{"callId":"c1","userId":"bob"}""", "2026-11-22T10:00:04Z"),
        ]);
        Assert.Equal(CallStatus.Active, t.Status);
        Assert.Equal(["ana", "bob", "carl"], t.Participants);
        Assert.Equal(["ana", "bob"], t.Present);
    }

    [Theory]
    [InlineData("CallEnded", """{"callId":"c1","durationMs":1,"reason":"last-left"}""")]
    [InlineData("CallFailed", """{"callId":"c1","reason":"relay timeout"}""")]
    public void Nobody_is_present_after_the_call_ends(string type, string payload)
    {
        var t = CallTrail.Build("c1",
        [
            Ev("1", "CallStarted", """{"callId":"c1","participants":["ana","bob"],"capacity":2}""", "2026-11-22T10:00:00Z"),
            Ev("2", type, payload, "2026-11-22T10:00:05Z"),
        ]);
        Assert.Empty(t.Present);
        Assert.Equal(["ana", "bob"], t.Participants);
    }

    [Fact]
    public void Verified_when_the_stored_envelope_matches_its_sha256()
    {
        var e = Ev("1", "CallStarted", "{}", "2026-11-22T10:00:00Z");
        Assert.True(e.Verified);
        Assert.False((e with { RawEnvelope = e.RawEnvelope!.Replace("CallStarted", "CallEnded") }).Verified); // tampered
        Assert.False((e with { RawEnvelope = null }).Verified); // old entry stored without the envelope
        Assert.False((e with { Sha256 = new string('0', 64) }).Verified);
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
    public void Config_requires_queue_tables_and_api_token()
    {
        var ex = Assert.Throws<InvalidOperationException>(() => VaultConfig.Load(_ => null));
        Assert.Contains("CALL_EVENTS_QUEUE", ex.Message);
        Assert.Contains("AUDIT_API_TOKEN", ex.Message);
        // fail closed: everything else set, token missing -> startup fails
        var noToken = Assert.Throws<InvalidOperationException>(() =>
            VaultConfig.Load(k => k switch { "CALL_EVENTS_QUEUE" => "q", "AUDIT_TABLE" => "a", "PROCESSED_TABLE" => "p", _ => null }));
        Assert.Equal("missing environment variables: AUDIT_API_TOKEN", noToken.Message);
        var cfg = VaultConfig.Load(k => k switch { "CALL_EVENTS_QUEUE" => "q", "AUDIT_TABLE" => "a", "PROCESSED_TABLE" => "p", "AUDIT_API_TOKEN" => "t", _ => null });
        Assert.Equal("us-east-1", cfg.Region);
        Assert.Equal("t", cfg.ApiToken);
    }
}
