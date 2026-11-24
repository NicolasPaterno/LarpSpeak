using System.Security.Cryptography;
using System.Text;

namespace AuditVault;

/// <summary>
/// AES-256-GCM sealing of audio chunks. Layout of a sealed blob:
///   "LSA1" (4) | nonce (12, random per chunk) | tag (16) | ciphertext
/// The associated data (call, user, start time) is authenticated but not stored: it is
/// rebuilt from the S3 key when reading, so a chunk moved to another call, user or time
/// slot fails to open instead of being played in the wrong place.
/// </summary>
public static class AudioCrypto
{
    private static readonly byte[] Magic = "LSA1"u8.ToArray();
    public const int KeyBytes = 32, NonceBytes = 12, TagBytes = 16;
    private const int HeaderBytes = 4 + NonceBytes + TagBytes;

    public static byte[] Seal(byte[] key, ReadOnlySpan<byte> plaintext, ReadOnlySpan<byte> aad)
    {
        CheckKey(key);
        var sealedBlob = new byte[HeaderBytes + plaintext.Length];
        Magic.CopyTo(sealedBlob, 0);
        var nonce = sealedBlob.AsSpan(4, NonceBytes);
        RandomNumberGenerator.Fill(nonce);
        var tag = sealedBlob.AsSpan(4 + NonceBytes, TagBytes);
        using var aes = new AesGcm(key, TagBytes);
        aes.Encrypt(nonce, plaintext, sealedBlob.AsSpan(HeaderBytes), tag, aad);
        return sealedBlob;
    }

    /// <summary>Throws <see cref="CryptographicException"/> when the key, the data or the context do not match.</summary>
    public static byte[] Open(byte[] key, ReadOnlySpan<byte> sealedBlob, ReadOnlySpan<byte> aad)
    {
        CheckKey(key);
        if (sealedBlob.Length < HeaderBytes || !sealedBlob[..4].SequenceEqual(Magic))
            throw new CryptographicException("not a sealed audio chunk");
        var plaintext = new byte[sealedBlob.Length - HeaderBytes];
        using var aes = new AesGcm(key, TagBytes);
        aes.Decrypt(sealedBlob.Slice(4, NonceBytes), sealedBlob[HeaderBytes..], sealedBlob.Slice(4 + NonceBytes, TagBytes), plaintext, aad);
        return plaintext;
    }

    public static byte[] Aad(string callId, string userId, long startMs) =>
        Encoding.UTF8.GetBytes($"{callId}\n{userId}\n{startMs}");

    private static void CheckKey(byte[] key)
    {
        if (key.Length != KeyBytes) throw new ArgumentException($"audio key must be {KeyBytes} bytes, got {key.Length}");
    }
}
