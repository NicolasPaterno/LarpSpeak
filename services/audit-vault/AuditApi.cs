using System.Security.Cryptography;
using System.Text;
using System.Text.Json;

namespace AuditVault;

/// <summary>Read-only view of the audit trail. /health stays open; /audit/* needs the token when one is configured.</summary>
public static class AuditApi
{
    public static void Map(WebApplication app, VaultConfig cfg)
    {
        app.MapGet("/health", () => Results.Ok(new { status = "ok", service = "audit-vault" }));

        var audit = app.MapGroup("/audit").AddEndpointFilter(async (ctx, next) =>
        {
            if (cfg.ApiToken is not null && !TokenOk(ctx.HttpContext.Request.Headers.Authorization.ToString(), cfg.ApiToken))
                return Results.Json(new { error = "unauthorized" }, statusCode: StatusCodes.Status401Unauthorized);
            return await next(ctx);
        });

        audit.MapGet("/calls", async (IAuditStore store, int? limit, CancellationToken ct) =>
        {
            var calls = await store.ListRecentAsync(Math.Clamp(limit ?? 50, 1, 200), ct);
            return Results.Ok(calls.Select(View));
        });

        audit.MapGet("/calls/{callId}", async (IAuditStore store, string callId, CancellationToken ct) =>
        {
            var trail = await store.GetTrailAsync(callId, ct);
            return trail is null ? Results.NotFound(new { error = "no audit records for this call" }) : Results.Ok(View(trail));
        });
    }

    internal static bool TokenOk(string header, string expected)
    {
        const string prefix = "Bearer ";
        if (!header.StartsWith(prefix, StringComparison.OrdinalIgnoreCase)) return false;
        var given = SHA256.HashData(Encoding.UTF8.GetBytes(header[prefix.Length..].Trim()));
        var want = SHA256.HashData(Encoding.UTF8.GetBytes(expected));
        return CryptographicOperations.FixedTimeEquals(given, want);
    }

    private static object View(CallTrail t) => new
    {
        t.CallId, t.Status, t.Participants, t.StartedAt, t.EndedAt, t.DurationMs, t.Reason,
        EventCount = t.Events.Count,
        Events = t.Events.Select(e => new
        {
            e.MessageId, e.Type, e.Version, e.OccurredAt, e.ReceivedAt, e.Sha256,
            Payload = JsonSerializer.Deserialize<JsonElement>(e.Payload),
        }),
    };
}
