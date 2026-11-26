namespace AuditVault;

/// <summary>Configuration comes only from environment variables (same code on MiniStack and real AWS).</summary>
public sealed record VaultConfig(
    string? EndpointUrl,
    string Region,
    string CallEventsQueue,
    string AuditTable,
    string ProcessedTable,
    string ApiToken,
    string? AudioQueue = null,
    string? AudioBucket = null,
    string? AudioKeySecret = null)
{
    /// <summary>Call recording is on when AUDIO_CHUNKS_QUEUE is set.</summary>
    public bool AudioEnabled => AudioQueue is not null;

    public static VaultConfig Load(Func<string, string?> env)
    {
        var missing = new List<string>();
        string Need(string k)
        {
            var v = env(k);
            if (string.IsNullOrEmpty(v)) missing.Add(k);
            return v ?? "";
        }
        string? Opt(string k) => string.IsNullOrEmpty(env(k)) ? null : env(k);

        var audioQueue = Opt("AUDIO_CHUNKS_QUEUE");
        var cfg = new VaultConfig(
            env("AWS_ENDPOINT_URL"),
            string.IsNullOrEmpty(env("AWS_REGION")) ? "us-east-1" : env("AWS_REGION")!,
            Need("CALL_EVENTS_QUEUE"),
            Need("AUDIT_TABLE"),
            Need("PROCESSED_TABLE"),
            Need("AUDIT_API_TOKEN"), // fail closed: the audit API never runs without a token
            audioQueue,
            audioQueue is null ? null : Need("AUDIO_BUCKET"),
            audioQueue is null ? null : Need("AUDIO_KEY_SECRET"));
        if (missing.Count > 0) throw new InvalidOperationException($"missing environment variables: {string.Join(", ", missing)}");
        return cfg;
    }
}
