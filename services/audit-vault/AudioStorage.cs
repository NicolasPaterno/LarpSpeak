using System.Globalization;
using Amazon.DynamoDBv2;
using Amazon.DynamoDBv2.Model;
using Amazon.S3;
using Amazon.S3.Model;
using Amazon.SecretsManager;
using Amazon.SecretsManager.Model;

namespace AuditVault;

public sealed record AudioObject(string Key, string UserId, long StartMs, long Size);

/// <summary>Where sealed audio chunks live (S3). A fake replaces it in tests.</summary>
public interface IAudioStore
{
    Task PutAsync(string key, byte[] sealedChunk, CancellationToken ct);
    Task<byte[]> GetAsync(string key, CancellationToken ct);
    /// <summary>Every chunk of a call, ordered by participant and then by time.</summary>
    Task<IReadOnlyList<AudioObject>> ListAsync(string callId, CancellationToken ct);
}

/// <summary>Source of the 32-byte AES key (Secrets Manager id AUDIO_KEY_SECRET).</summary>
public interface IAudioKeyProvider
{
    Task<byte[]> GetKeyAsync(CancellationToken ct);
}

/// <summary>Remembers which messageIds were handled (the audit-processed table).</summary>
public interface IProcessedStore
{
    Task<bool> SeenAsync(string messageId, CancellationToken ct);
    /// <summary>Marks it handled; marking twice is not an error (two replicas may race).</summary>
    Task MarkAsync(string messageId, CancellationToken ct);
}

public sealed class S3AudioStore(IAmazonS3 s3, VaultConfig cfg) : IAudioStore
{
    public async Task PutAsync(string key, byte[] sealedChunk, CancellationToken ct)
    {
        using var body = new MemoryStream(sealedChunk, writable: false);
        await s3.PutObjectAsync(new PutObjectRequest
        {
            BucketName = cfg.AudioBucket, Key = key, InputStream = body, ContentType = "application/octet-stream",
        }, ct);
    }

    public async Task<byte[]> GetAsync(string key, CancellationToken ct)
    {
        using var res = await s3.GetObjectAsync(cfg.AudioBucket, key, ct);
        using var ms = new MemoryStream();
        await res.ResponseStream.CopyToAsync(ms, ct);
        return ms.ToArray();
    }

    public async Task<IReadOnlyList<AudioObject>> ListAsync(string callId, CancellationToken ct)
    {
        var found = new List<AudioObject>();
        string? token = null;
        do
        {
            var res = await s3.ListObjectsV2Async(new ListObjectsV2Request
            {
                BucketName = cfg.AudioBucket, Prefix = AudioKeys.Prefix(callId), ContinuationToken = token,
            }, ct);
            foreach (var o in res.S3Objects ?? [])
                if (AudioKeys.TryParse(o.Key, out _, out var user, out var start))
                    found.Add(new AudioObject(o.Key, user, start, o.Size ?? 0));
            token = res.IsTruncated == true ? res.NextContinuationToken : null;
        } while (token is not null);
        return found.OrderBy(o => o.UserId, StringComparer.Ordinal).ThenBy(o => o.Key, StringComparer.Ordinal).ToList();
    }
}

public sealed class SecretsManagerKeyProvider(IAmazonSecretsManager sm, VaultConfig cfg) : IAudioKeyProvider
{
    private byte[]? _key;

    // Read lazily and cached once valid: a broker or secrets outage at startup is not fatal (R6);
    // the message that needed the key just fails and is redelivered.
    public async Task<byte[]> GetKeyAsync(CancellationToken ct)
    {
        if (_key is not null) return _key;
        var res = await sm.GetSecretValueAsync(new GetSecretValueRequest { SecretId = cfg.AudioKeySecret }, ct);
        var key = Convert.FromBase64String(res.SecretString);
        if (key.Length != AudioCrypto.KeyBytes)
            throw new InvalidOperationException($"secret {cfg.AudioKeySecret} must hold {AudioCrypto.KeyBytes} bytes (base64), found {key.Length}");
        return _key = key;
    }
}

public sealed class DynamoProcessedStore(IAmazonDynamoDB db, VaultConfig cfg) : IProcessedStore
{
    public async Task<bool> SeenAsync(string messageId, CancellationToken ct)
    {
        var res = await db.GetItemAsync(new GetItemRequest
        {
            TableName = cfg.ProcessedTable, ConsistentRead = true,
            Key = new Dictionary<string, AttributeValue> { ["pk"] = new() { S = messageId } },
        }, ct);
        return res.IsItemSet;
    }

    public async Task MarkAsync(string messageId, CancellationToken ct)
    {
        try
        {
            await db.PutItemAsync(new PutItemRequest
            {
                TableName = cfg.ProcessedTable,
                Item = new Dictionary<string, AttributeValue>
                {
                    ["pk"] = new() { S = messageId },
                    ["at"] = new() { S = DateTimeOffset.UtcNow.ToString("O", CultureInfo.InvariantCulture) },
                },
                ConditionExpression = "attribute_not_exists(pk)",
            }, ct);
        }
        catch (ConditionalCheckFailedException) { /* another replica marked it first */ }
    }
}
