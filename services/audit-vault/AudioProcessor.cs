namespace AuditVault;

/// <summary>Handles one queue message. Both queues of the vault use the same consumer loop.</summary>
public interface IMessageHandler
{
    Task<Outcome> HandleAsync(string body, CancellationToken ct);
}

/// <summary>
/// AudioChunk -> sealed with AES-256-GCM -> S3. The object key is derived from the message
/// (call, user, startMs, messageId), so handling the same message twice overwrites the same
/// object: the effect is idempotent even if the process dies between the upload and the
/// processed marker (S3 cannot join a transaction, a deterministic key replaces it) (R1).
/// </summary>
public sealed class AudioProcessor(
    IAudioStore audio, IAudioKeyProvider keys, IProcessedStore processed, ILogger<AudioProcessor> log) : IMessageHandler
{
    public async Task<Outcome> HandleAsync(string body, CancellationToken ct)
    {
        if (!Envelope.TryParse(body, out var env, out var error) || env is null)
        {
            log.LogError("poison audio message left for DLQ: {Error}", error);
            return Outcome.Poison;
        }
        if (!AudioChunk.TryParse(env, out var chunk, out error) || chunk is null)
        {
            log.LogError("poison audio message left for DLQ: {Error} messageId={MessageId}", error, env.MessageId);
            return Outcome.Poison;
        }
        try
        {
            if (await processed.SeenAsync(env.MessageId, ct))
            {
                log.LogInformation("duplicate ignored messageId={MessageId} correlationId={CorrelationId} type={Type}", env.MessageId, env.CorrelationId, env.Type);
                return Outcome.Duplicate;
            }
            var key = await keys.GetKeyAsync(ct);
            var sealedChunk = AudioCrypto.Seal(key, ChunkCodec.Serialize(chunk.Frames), AudioCrypto.Aad(chunk.CallId, chunk.UserId, chunk.StartMs));
            var objectKey = AudioKeys.For(chunk.CallId, chunk.UserId, chunk.StartMs, env.MessageId);
            await audio.PutAsync(objectKey, sealedChunk, ct);
            if (!await processed.MarkAsync(env.MessageId, ct))
            {
                // Lost the race to another replica: same key, so S3 still holds one object for this message.
                log.LogInformation("duplicate ignored messageId={MessageId} correlationId={CorrelationId} type={Type}", env.MessageId, env.CorrelationId, env.Type);
                return Outcome.Duplicate;
            }
            log.LogInformation("audio stored messageId={MessageId} correlationId={CorrelationId} userId={UserId} frames={Frames} bytes={Bytes}",
                env.MessageId, env.CorrelationId, chunk.UserId, chunk.Frames.Count, sealedChunk.Length);
            return Outcome.Recorded;
        }
        catch (Exception ex) when (ex is not OperationCanceledException)
        {
            log.LogWarning(ex, "audio handling failed, will be redelivered messageId={MessageId} correlationId={CorrelationId}", env.MessageId, env.CorrelationId);
            return Outcome.Failed;
        }
    }
}

/// <summary>Reads recordings back: lists the tracks of a call and builds one Ogg Opus file per participant.</summary>
public sealed class AudioExporter(IAudioStore audio, IAudioKeyProvider keys)
{
    public sealed record Track(string UserId, int Chunks, long Bytes, DateTimeOffset StartedAt, DateTimeOffset LastChunkAt);

    public async Task<IReadOnlyList<Track>> TracksAsync(string callId, CancellationToken ct) =>
        (await audio.ListAsync(callId, ct))
            .GroupBy(o => o.UserId)
            .Select(g => new Track(g.Key, g.Count(), g.Sum(o => o.Size),
                DateTimeOffset.FromUnixTimeMilliseconds(g.Min(o => o.StartMs)), DateTimeOffset.FromUnixTimeMilliseconds(g.Max(o => o.StartMs))))
            .ToList();

    /// <summary>Opens and joins every chunk of the participant. Returns null when nothing was recorded.
    /// Fails (CryptographicException) if any chunk was tampered with or moved.</summary>
    public async Task<byte[]?> ExportOggAsync(string callId, string userId, CancellationToken ct)
    {
        var chunks = (await audio.ListAsync(callId, ct)).Where(o => o.UserId == userId).ToList();
        if (chunks.Count == 0) return null;
        var key = await keys.GetKeyAsync(ct);
        var frames = new List<OpusFrame>();
        foreach (var c in chunks)
        {
            var blob = await audio.GetAsync(c.Key, ct);
            var plain = AudioCrypto.Open(key, blob, AudioCrypto.Aad(callId, userId, c.StartMs));
            frames.AddRange(ChunkCodec.Parse(plain));
        }
        using var ms = new MemoryStream();
        OggOpus.Write(ms, frames, serial: OggOpus.Crc(System.Text.Encoding.UTF8.GetBytes(callId + userId)));
        return ms.ToArray();
    }
}
