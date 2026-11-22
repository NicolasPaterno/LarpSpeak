namespace AuditVault;

/// <summary>Configuration comes only from environment variables (same code on MiniStack and real AWS).</summary>
public sealed record VaultConfig(
    string? EndpointUrl,
    string Region,
    string CallEventsQueue,
    string AuditTable,
    string ProcessedTable,
    string? ApiToken)
{
    public static VaultConfig Load(Func<string, string?> env)
    {
        var missing = new List<string>();
        string Need(string k)
        {
            var v = env(k);
            if (string.IsNullOrEmpty(v)) missing.Add(k);
            return v ?? "";
        }
        var cfg = new VaultConfig(
            env("AWS_ENDPOINT_URL"),
            string.IsNullOrEmpty(env("AWS_REGION")) ? "us-east-1" : env("AWS_REGION")!,
            Need("CALL_EVENTS_QUEUE"),
            Need("AUDIT_TABLE"),
            Need("PROCESSED_TABLE"),
            string.IsNullOrEmpty(env("AUDIT_API_TOKEN")) ? null : env("AUDIT_API_TOKEN"));
        if (missing.Count > 0) throw new InvalidOperationException($"missing environment variables: {string.Join(", ", missing)}");
        return cfg;
    }
}
