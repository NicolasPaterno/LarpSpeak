using System.Security.Cryptography;
using System.Text;
using System.Text.Json;

namespace AuditVault;

/// <summary>One immutable audit entry: a call event exactly as it was received.</summary>
public sealed record AuditEvent(
    string MessageId,
    string CallId,
    string Type,
    int Version,
    DateTimeOffset OccurredAt,
    DateTimeOffset ReceivedAt,
    string Payload,
    string Sha256,
    string? RawEnvelope = null)
{
    public static AuditEvent From(Envelope e, DateTimeOffset receivedAt) => new(
        e.MessageId, e.CorrelationId, e.Type, e.Version, e.OccurredAt, receivedAt,
        e.Payload.GetRawText(), Hash(e.Raw), e.Raw);

    /// <summary>
    /// Tamper evidence: true only when the stored original message still hashes to the stored sha256.
    /// Entries written before the envelope was kept cannot be checked and report false.
    /// </summary>
    public bool Verified => RawEnvelope is not null
        && CryptographicOperations.FixedTimeEquals(Encoding.ASCII.GetBytes(Hash(RawEnvelope)), Encoding.ASCII.GetBytes(Sha256));

    private static string Hash(string raw) => Convert.ToHexStringLower(SHA256.HashData(Encoding.UTF8.GetBytes(raw)));
}

public static class CallStatus
{
    public const string Pending = "PENDING", Active = "ACTIVE", Ended = "ENDED", Failed = "FAILED";
}

/// <summary>Everything the vault knows about one call, derived from its events.</summary>
public sealed record CallTrail(
    string CallId,
    string Status,
    IReadOnlyList<string> Participants,
    IReadOnlyList<string> Present,
    DateTimeOffset? StartedAt,
    DateTimeOffset? EndedAt,
    long? DurationMs,
    string? Reason,
    IReadOnlyList<AuditEvent> Events)
{
    /// <summary>The state is rebuilt from the events on every read, so it does not depend on arrival order.</summary>
    public static CallTrail Build(string callId, IEnumerable<AuditEvent> events)
    {
        var ordered = events.OrderBy(e => e.OccurredAt).ThenBy(e => e.ReceivedAt).ToList();
        var participants = new List<string>();
        var present = new List<string>(); // who is in the call now; Participants keeps everyone who ever took part
        void Add(string? u)
        {
            if (string.IsNullOrEmpty(u)) return;
            if (!participants.Contains(u)) participants.Add(u);
            if (!present.Contains(u)) present.Add(u);
        }

        string status = CallStatus.Pending;
        DateTimeOffset? started = null, ended = null;
        long? duration = null;
        string? reason = null;

        foreach (var e in ordered)
        {
            using var doc = JsonDocument.Parse(e.Payload);
            var p = doc.RootElement;
            switch (e.Type)
            {
                case "CallStarted":
                    started ??= e.OccurredAt;
                    if (status == CallStatus.Pending) status = CallStatus.Active;
                    if (p.TryGetProperty("participants", out var list) && list.ValueKind == JsonValueKind.Array)
                        foreach (var u in list.EnumerateArray()) Add(u.ValueKind == JsonValueKind.String ? u.GetString() : null);
                    break;
                case "ParticipantJoined":
                    Add(Prop(p, "userId"));
                    break;
                case "ParticipantLeft":
                case "ParticipantTimedOut":
                    present.Remove(Prop(p, "userId") ?? "");
                    break;
                case "CallEnded":
                    if (status != CallStatus.Failed) status = CallStatus.Ended;
                    present.Clear();
                    ended = e.OccurredAt;
                    reason = Prop(p, "reason") ?? reason;
                    if (p.TryGetProperty("durationMs", out var d) && d.ValueKind == JsonValueKind.Number && d.TryGetInt64(out var ms)) duration = ms;
                    break;
                case "CallFailed":
                    status = CallStatus.Failed;
                    present.Clear();
                    ended = e.OccurredAt;
                    reason = Prop(p, "reason") ?? reason;
                    break;
            }
        }
        return new CallTrail(callId, status, participants, present, started, ended, duration, reason, ordered);
    }

    private static string? Prop(JsonElement o, string name) =>
        o.TryGetProperty(name, out var v) && v.ValueKind == JsonValueKind.String ? v.GetString() : null;

    /// <summary>The trail without its events, for listings.</summary>
    public CallTrail Summary() => this with { Events = [] };
}
