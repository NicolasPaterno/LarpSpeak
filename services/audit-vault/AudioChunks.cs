using System.Buffers.Binary;
using System.Text.Json;
using System.Text.RegularExpressions;

namespace AuditVault;

/// <summary>One Opus packet and its RTP timestamp (48 kHz clock).</summary>
public readonly record struct OpusFrame(uint Ts, byte[] Data);

/// <summary>Payload of an AudioChunk event (contracts/events/AudioChunk.v1.schema.json), validated.</summary>
public sealed partial record AudioChunk(string CallId, string UserId, long StartMs, long DurationMs, IReadOnlyList<OpusFrame> Frames)
{
    public const int MaxFrames = 5000, MaxFrameBytes = 2000;
    private static readonly Regex Safe = IdPattern();

    /// <summary>Ids end up inside S3 keys, so only a safe alphabet is accepted (no "/", no "..").</summary>
    public static bool IsSafeId(string? s) => s is not null && Safe.IsMatch(s);

    public static bool TryParse(Envelope env, out AudioChunk? chunk, out string error)
    {
        chunk = null;
        error = "";
        if (env.Type != "AudioChunk") { error = $"unexpected type {env.Type}"; return false; }
        if (!IsSafeId(env.MessageId)) { error = "unsafe messageId"; return false; }
        var p = env.Payload;
        var callId = Str(p, "callId");
        var userId = Str(p, "userId");
        if (!IsSafeId(callId) || !IsSafeId(userId)) { error = "callId and userId must be 1-64 of A-Z a-z 0-9 _ -"; return false; }
        if (callId != env.CorrelationId) { error = "callId differs from correlationId"; return false; }
        if (!p.TryGetProperty("startMs", out var s) || !s.TryGetInt64(out var startMs) || startMs < 0) { error = "invalid startMs"; return false; }
        if (!p.TryGetProperty("durationMs", out var d) || !d.TryGetInt64(out var durationMs) || durationMs < 0) { error = "invalid durationMs"; return false; }
        if (!p.TryGetProperty("frames", out var fs) || fs.ValueKind != JsonValueKind.Array) { error = "frames must be an array"; return false; }
        var n = fs.GetArrayLength();
        if (n is < 1 or > MaxFrames) { error = $"frames must have 1..{MaxFrames} items"; return false; }
        var frames = new List<OpusFrame>(n);
        foreach (var f in fs.EnumerateArray())
        {
            if (f.ValueKind != JsonValueKind.Object || !f.TryGetProperty("ts", out var ts) || !ts.TryGetUInt32(out var t)
                || !f.TryGetProperty("data", out var data) || data.ValueKind != JsonValueKind.String)
            { error = "each frame needs ts and data"; return false; }
            byte[] bytes;
            try { bytes = data.GetBytesFromBase64(); }
            catch (FormatException) { error = "frame data is not base64"; return false; }
            if (bytes.Length is 0 or > MaxFrameBytes) { error = $"frame must have 1..{MaxFrameBytes} bytes"; return false; }
            frames.Add(new OpusFrame(t, bytes));
        }
        chunk = new AudioChunk(callId!, userId!, startMs, durationMs, frames);
        return true;
    }

    private static string? Str(JsonElement o, string name) =>
        o.TryGetProperty(name, out var v) && v.ValueKind == JsonValueKind.String ? v.GetString() : null;

    [GeneratedRegex("^[A-Za-z0-9_-]{1,64}$")]
    private static partial Regex IdPattern();
}

/// <summary>Plaintext layout inside a sealed chunk: count u32 | { ts u32 | len u16 | bytes }*, big endian.</summary>
public static class ChunkCodec
{
    public static byte[] Serialize(IReadOnlyList<OpusFrame> frames)
    {
        var size = 4 + frames.Sum(f => 6 + f.Data.Length);
        var buf = new byte[size];
        BinaryPrimitives.WriteUInt32BigEndian(buf, (uint)frames.Count);
        var o = 4;
        foreach (var f in frames)
        {
            BinaryPrimitives.WriteUInt32BigEndian(buf.AsSpan(o), f.Ts);
            BinaryPrimitives.WriteUInt16BigEndian(buf.AsSpan(o + 4), checked((ushort)f.Data.Length));
            f.Data.CopyTo(buf, o + 6);
            o += 6 + f.Data.Length;
        }
        return buf;
    }

    public static List<OpusFrame> Parse(ReadOnlySpan<byte> buf)
    {
        if (buf.Length < 4) throw new InvalidDataException("chunk too short");
        var count = BinaryPrimitives.ReadUInt32BigEndian(buf);
        if (count > AudioChunk.MaxFrames) throw new InvalidDataException("too many frames");
        var frames = new List<OpusFrame>((int)count);
        var o = 4;
        for (var i = 0; i < count; i++)
        {
            if (o + 6 > buf.Length) throw new InvalidDataException("truncated frame header");
            var ts = BinaryPrimitives.ReadUInt32BigEndian(buf[o..]);
            var len = BinaryPrimitives.ReadUInt16BigEndian(buf[(o + 4)..]);
            if (o + 6 + len > buf.Length) throw new InvalidDataException("truncated frame");
            frames.Add(new OpusFrame(ts, buf.Slice(o + 6, len).ToArray()));
            o += 6 + len;
        }
        if (o != buf.Length) throw new InvalidDataException("trailing bytes");
        return frames;
    }
}

/// <summary>S3 object keys of the recordings: audio/{callId}/{userId}/{startMs:D13}-{messageId}.bin</summary>
public static class AudioKeys
{
    public static string Prefix(string callId) => $"audio/{callId}/";
    public static string UserPrefix(string callId, string userId) => $"audio/{callId}/{userId}/";

    // startMs is zero padded so a plain listing returns each participant's chunks in time order.
    // The messageId makes the key deterministic: a redelivered message overwrites the same object.
    public static string For(string callId, string userId, long startMs, string messageId) =>
        $"{UserPrefix(callId, userId)}{startMs:D13}-{messageId}.bin";

    public static bool TryParse(string key, out string callId, out string userId, out long startMs)
    {
        callId = userId = "";
        startMs = 0;
        var parts = key.Split('/');
        if (parts.Length != 4 || parts[0] != "audio") return false;
        var name = parts[3];
        var dash = name.IndexOf('-');
        if (dash != 13 || !long.TryParse(name.AsSpan(0, 13), out startMs)) return false;
        callId = parts[1];
        userId = parts[2];
        return AudioChunk.IsSafeId(callId) && AudioChunk.IsSafeId(userId);
    }
}
