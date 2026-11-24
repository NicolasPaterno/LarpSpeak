using System.Text.Json;

namespace AuditVault;

/// <summary>Shared message format, see contracts/envelope.schema.json.</summary>
public sealed record Envelope(
    string MessageId,
    string CorrelationId,
    string Type,
    int Version,
    DateTimeOffset OccurredAt,
    JsonElement Payload,
    string Raw)
{
    /// <summary>
    /// Rejects anything that does not match the contract, so poison messages are never
    /// acknowledged and end up in the DLQ (F4). Same rules as the Go services.
    /// </summary>
    public static bool TryParse(string body, out Envelope? envelope, out string error)
    {
        envelope = null;
        error = "";
        try
        {
            using var doc = JsonDocument.Parse(body);
            var root = doc.RootElement;
            if (root.ValueKind != JsonValueKind.Object) { error = "envelope must be a JSON object"; return false; }

            var messageId = Str(root, "messageId");
            var correlationId = Str(root, "correlationId");
            var type = Str(root, "type");
            if (string.IsNullOrEmpty(messageId) || string.IsNullOrEmpty(correlationId) || string.IsNullOrEmpty(type))
            { error = "missing messageId, correlationId or type"; return false; }

            if (!root.TryGetProperty("version", out var v) || v.ValueKind != JsonValueKind.Number || !v.TryGetInt32(out var version) || version < 1)
            { error = "missing or invalid version"; return false; }

            if (!root.TryGetProperty("occurredAt", out var at) || at.ValueKind != JsonValueKind.String || !at.TryGetDateTimeOffset(out var occurredAt))
            { error = "missing or invalid occurredAt"; return false; }

            if (!root.TryGetProperty("payload", out var payload) || payload.ValueKind != JsonValueKind.Object)
            { error = "payload must be an object"; return false; }

            envelope = new Envelope(messageId, correlationId, type, version, occurredAt, payload.Clone(), body);
            return true;
        }
        catch (JsonException ex)
        {
            error = $"invalid json: {ex.Message}";
            return false;
        }
    }

    private static string Str(JsonElement o, string name) =>
        o.TryGetProperty(name, out var p) && p.ValueKind == JsonValueKind.String ? p.GetString() ?? "" : "";
}
