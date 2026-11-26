using System.Buffers.Binary;
using System.Security.Cryptography;
using System.Text;
using AuditVault;
using Microsoft.Extensions.Logging.Abstractions;
using Xunit;

namespace AuditVault.Tests;

public sealed class FakeAudioStore : IAudioStore
{
    public Dictionary<string, byte[]> Objects { get; } = new();
    public int FailPuts { get; set; }
    public int Puts { get; private set; }

    public Task PutAsync(string key, byte[] sealedChunk, CancellationToken ct)
    {
        Puts++;
        if (FailPuts > 0) { FailPuts--; throw new IOException("s3 down"); }
        Objects[key] = sealedChunk;
        return Task.CompletedTask;
    }

    public Task<byte[]> GetAsync(string key, CancellationToken ct) => Task.FromResult(Objects[key]);

    public Task<IReadOnlyList<AudioObject>> ListAsync(string callId, CancellationToken ct) =>
        Task.FromResult<IReadOnlyList<AudioObject>>(Objects
            .Where(o => o.Key.StartsWith(AudioKeys.Prefix(callId), StringComparison.Ordinal))
            .Select(o => { AudioKeys.TryParse(o.Key, out _, out var u, out var s); return new AudioObject(o.Key, u, s, o.Value.Length); })
            .OrderBy(o => o.UserId, StringComparer.Ordinal).ThenBy(o => o.Key, StringComparer.Ordinal).ToList());
}

public sealed class FakeKeys(byte[]? key = null) : IAudioKeyProvider
{
    public static byte[] NewKey() => RandomNumberGenerator.GetBytes(32);
    public byte[] Key { get; } = key ?? NewKey();
    public bool Down { get; set; }
    public Task<byte[]> GetKeyAsync(CancellationToken ct) => Down ? throw new IOException("secrets down") : Task.FromResult(Key);
}

public sealed class FakeProcessed : IProcessedStore
{
    public HashSet<string> Ids { get; } = new();
    public bool Blind { get; set; } // SeenAsync misses: two replicas checked before either marked
    public Task<bool> SeenAsync(string id, CancellationToken ct) => Task.FromResult(!Blind && Ids.Contains(id));
    public Task<bool> MarkAsync(string id, CancellationToken ct) => Task.FromResult(Ids.Add(id));
}

public static class A
{
    public static string Chunk(string messageId, string call, string user, long startMs, params byte[][] frames)
    {
        var fs = string.Join(",", frames.Select((f, i) => $$"""{"ts":{{960 * i}},"data":"{{Convert.ToBase64String(f)}}"}"""));
        return T.Msg(messageId, "AudioChunk", call, $$"""{"callId":"{{call}}","userId":"{{user}}","startMs":{{startMs}},"durationMs":20,"frames":[{{fs}}]}""");
    }

    public static (AudioProcessor p, FakeAudioStore s, FakeKeys k, FakeProcessed d) Processor()
    {
        var s = new FakeAudioStore(); var k = new FakeKeys(); var d = new FakeProcessed();
        return (new AudioProcessor(s, k, d, NullLogger<AudioProcessor>.Instance), s, k, d);
    }

    // a real 20 ms CELT fullband packet header (TOC 0xF8) followed by filler
    public static byte[] Opus(byte fill, int len = 40) { var b = new byte[len]; Array.Fill(b, fill); b[0] = 0xF8; return b; }
}

public class AudioCryptoTests
{
    private static readonly byte[] Aad = AudioCrypto.Aad("call1", "ana", 1_700_000_000_000);

    [Fact]
    public void Seal_then_open_returns_the_original()
    {
        var key = FakeKeys.NewKey();
        var plain = Encoding.UTF8.GetBytes("voice voice voice voice voice");
        var sealedBlob = AudioCrypto.Seal(key, plain, Aad);
        Assert.Equal(plain, AudioCrypto.Open(key, sealedBlob, Aad));
    }

    [Fact]
    public void The_sealed_blob_does_not_contain_the_plaintext_and_changes_each_time()
    {
        var key = FakeKeys.NewKey();
        var plain = Encoding.UTF8.GetBytes("sensitive audio sensitive audio");
        var a = AudioCrypto.Seal(key, plain, Aad);
        var b = AudioCrypto.Seal(key, plain, Aad);
        Assert.DoesNotContain("sensitive", Encoding.UTF8.GetString(a));
        Assert.NotEqual(a, b); // fresh random nonce every time
        Assert.Equal("LSA1", Encoding.ASCII.GetString(a, 0, 4));
    }

    [Fact]
    public void A_flipped_bit_is_detected()
    {
        var key = FakeKeys.NewKey();
        var blob = AudioCrypto.Seal(key, new byte[64], Aad);
        blob[^1] ^= 1;
        Assert.ThrowsAny<CryptographicException>(() => AudioCrypto.Open(key, blob, Aad));
    }

    [Theory]
    [InlineData("call2", "ana", 1_700_000_000_000)]
    [InlineData("call1", "bob", 1_700_000_000_000)]
    [InlineData("call1", "ana", 1_700_000_005_000)]
    public void A_chunk_moved_to_another_call_user_or_time_does_not_open(string call, string user, long start)
    {
        var key = FakeKeys.NewKey();
        var blob = AudioCrypto.Seal(key, new byte[64], Aad);
        Assert.ThrowsAny<CryptographicException>(() => AudioCrypto.Open(key, blob, AudioCrypto.Aad(call, user, start)));
    }

    [Fact]
    public void Another_key_does_not_open_it()
    {
        var blob = AudioCrypto.Seal(FakeKeys.NewKey(), new byte[64], Aad);
        Assert.ThrowsAny<CryptographicException>(() => AudioCrypto.Open(FakeKeys.NewKey(), blob, Aad));
    }

    [Fact]
    public void A_key_of_the_wrong_size_is_refused()
    {
        Assert.Throws<ArgumentException>(() => AudioCrypto.Seal(new byte[16], new byte[8], Aad));
    }
}

public class AudioFormatTests
{
    [Fact]
    public void Chunk_codec_round_trips()
    {
        var frames = new[] { new OpusFrame(0, [1, 2, 3]), new OpusFrame(960, [4]), new OpusFrame(uint.MaxValue, new byte[1275]) };
        var back = ChunkCodec.Parse(ChunkCodec.Serialize(frames));
        Assert.Equal(3, back.Count);
        Assert.Equal(uint.MaxValue, back[2].Ts);
        Assert.Equal(frames[0].Data, back[0].Data);
        Assert.Equal(1275, back[2].Data.Length);
    }

    [Fact]
    public void Chunk_codec_rejects_truncated_and_padded_input()
    {
        var bytes = ChunkCodec.Serialize([new OpusFrame(1, [1, 2, 3, 4])]);
        Assert.Throws<InvalidDataException>(() => ChunkCodec.Parse(bytes[..^1]));
        Assert.Throws<InvalidDataException>(() => ChunkCodec.Parse([.. bytes, 0]));
        Assert.Throws<InvalidDataException>(() => ChunkCodec.Parse([0, 0]));
    }

    [Fact]
    public void Keys_sort_by_time_and_parse_back()
    {
        var k1 = AudioKeys.For("c1", "ana", 99, "m-b");
        var k2 = AudioKeys.For("c1", "ana", 1_700_000_000_000, "m-a");
        Assert.True(string.CompareOrdinal(k1, k2) < 0);
        Assert.True(AudioKeys.TryParse(k2, out var call, out var user, out var start));
        Assert.Equal(("c1", "ana", 1_700_000_000_000), (call, user, start));
        Assert.False(AudioKeys.TryParse("audio/c1/ana/notatime.bin", out _, out _, out _));
        Assert.False(AudioKeys.TryParse("other/c1/ana/0000000000001-m.bin", out _, out _, out _));
    }

    [Theory]
    [InlineData(0xF8, 960)]            // CELT fullband, 20 ms, one frame
    [InlineData(0xFC, 960)]            // same config, code 0
    [InlineData(0xE0, 120)]            // CELT 2.5 ms
    [InlineData(0x08, 960)]            // SILK narrowband 20 ms
    [InlineData(0x18, 2880)]           // SILK narrowband 60 ms
    [InlineData(0x78, 960)]            // hybrid fullband 20 ms
    [InlineData(0x71, 960)]            // hybrid fullband 10 ms, code 1: two frames
    public void Packet_duration_comes_from_the_toc_byte(int toc, int samples) =>
        Assert.Equal(samples, OggOpus.Samples([(byte)toc, 0, 0]));

    [Fact]
    public void Code_3_packets_carry_a_frame_count_and_garbage_is_ignored()
    {
        Assert.Equal(3 * 960, OggOpus.Samples([0xFB, 0x03, 0]));
        Assert.Equal(0, OggOpus.Samples([0xFB]));           // code 3 without a count byte
        Assert.Equal(0, OggOpus.Samples([0xFB, 0x00]));     // zero frames
        Assert.Equal(0, OggOpus.Samples([]));
    }
}

public class OggTests
{
    // An independent, bit-by-bit CRC so the table-driven one in the vault is not checked against itself.
    private static uint SlowCrc(byte[] page)
    {
        uint crc = 0;
        foreach (var b in page)
        {
            crc ^= (uint)b << 24;
            for (var i = 0; i < 8; i++) crc = (crc & 0x80000000) != 0 ? (crc << 1) ^ 0x04C11DB7 : crc << 1;
        }
        return crc;
    }

    private sealed record Pg(byte Type, long Granule, uint Serial, uint Seq, List<byte[]> Packets);

    // Minimal Ogg demuxer for packets that do not span pages (true for Opus packets <= 1275 bytes with our paging).
    private static List<Pg> Demux(byte[] file)
    {
        var pages = new List<Pg>();
        var o = 0;
        while (o < file.Length)
        {
            Assert.Equal("OggS", Encoding.ASCII.GetString(file, o, 4));
            Assert.Equal(0, file[o + 4]);
            var n = file[o + 26];
            var headerLen = 27 + n;
            var seg = file.AsSpan(o + 27, n).ToArray();
            var dataLen = seg.Sum(s => s);
            var page = file.AsSpan(o, headerLen + dataLen).ToArray();
            var stored = BinaryPrimitives.ReadUInt32LittleEndian(page.AsSpan(22));
            page[22] = page[23] = page[24] = page[25] = 0;
            Assert.Equal(SlowCrc(page), stored);

            var packets = new List<byte[]>();
            var pos = headerLen;
            var cur = new List<byte>();
            foreach (var s in seg)
            {
                cur.AddRange(page.AsSpan(pos, s).ToArray());
                pos += s;
                if (s < 255) { packets.Add([.. cur]); cur.Clear(); }
            }
            Assert.Empty(cur); // no packet continues on the next page
            pages.Add(new Pg(file[o + 5], BinaryPrimitives.ReadInt64LittleEndian(file.AsSpan(o + 6)),
                BinaryPrimitives.ReadUInt32LittleEndian(file.AsSpan(o + 14)), BinaryPrimitives.ReadUInt32LittleEndian(file.AsSpan(o + 18)), packets));
            o += headerLen + dataLen;
        }
        return pages;
    }

    private static byte[] Encode(IReadOnlyList<OpusFrame> frames, uint serial = 7)
    {
        using var ms = new MemoryStream();
        OggOpus.Write(ms, frames, serial);
        return ms.ToArray();
    }

    [Fact]
    public void Output_is_a_valid_ogg_opus_stream()
    {
        var frames = Enumerable.Range(0, 130).Select(i => new OpusFrame((uint)(i * 960), A.Opus((byte)i, 20 + i))).ToList(); // 130 packets: several pages
        var pages = Demux(Encode(frames));

        Assert.Equal(0x02, pages[0].Type);                                  // BOS
        var head = pages[0].Packets.Single();
        Assert.Equal("OpusHead", Encoding.ASCII.GetString(head, 0, 8));
        Assert.Equal(1, head[8]);
        Assert.Equal(1, head[9]);                                           // mono: TOC 0xF8 has the stereo bit clear
        Assert.Equal(312, BinaryPrimitives.ReadUInt16LittleEndian(head.AsSpan(10)));
        Assert.Equal("OpusTags", Encoding.ASCII.GetString(pages[1].Packets.Single(), 0, 8));

        Assert.All(pages, p => Assert.Equal(7u, p.Serial));
        Assert.Equal(Enumerable.Range(0, pages.Count).Select(i => (uint)i), pages.Select(p => p.Seq));
        Assert.Equal(0x04, pages[^1].Type);                                 // EOS only on the last page
        Assert.DoesNotContain(pages[..^1], p => (p.Type & 0x04) != 0);
        Assert.True(pages.Count >= 5);                                      // 2 header pages + at least 3 audio pages

        var audio = pages.Skip(2).SelectMany(p => p.Packets).ToList();
        Assert.Equal(frames.Select(f => f.Data), audio);                    // packets pass through untouched
        Assert.Equal(312 + 130 * 960L, pages[^1].Granule);
        Assert.True(pages.Skip(2).Zip(pages.Skip(3), (a, b) => a.Granule <= b.Granule).All(x => x));
    }

    [Fact]
    public void Packets_of_255_bytes_or_more_use_correct_lacing()
    {
        var big = new[] { new OpusFrame(0, A.Opus(1, 255)), new OpusFrame(960, A.Opus(2, 510)), new OpusFrame(1920, A.Opus(3, 1275)) };
        var audio = Demux(Encode(big)).Skip(2).SelectMany(p => p.Packets).ToList();
        Assert.Equal(big.Select(f => f.Data), audio);
    }

    [Fact]
    public void Malformed_packets_are_skipped_and_an_empty_recording_is_still_valid()
    {
        var pages = Demux(Encode([new OpusFrame(0, [0xFB]), new OpusFrame(1, A.Opus(5))]));    // first one is a truncated code-3 packet
        Assert.Single(pages.Skip(2).SelectMany(p => p.Packets));
        var empty = Demux(Encode([]));
        Assert.Equal(3, empty.Count);
        Assert.Equal(0x04, empty[^1].Type);
    }

    [Fact]
    public void Stereo_packets_are_announced_as_two_channels()
    {
        var stereo = A.Opus(1); stereo[0] = 0xFC;                           // 0xFC has the stereo bit (0x04) set
        Assert.Equal(2, Demux(Encode([new OpusFrame(0, stereo)]))[0].Packets[0][9]);
    }
}

public class AudioChunkParsingTests
{
    private static bool Parse(string body, out AudioChunk? chunk, out string error)
    {
        Assert.True(Envelope.TryParse(body, out var env, out _));
        return AudioChunk.TryParse(env!, out chunk, out error);
    }

    [Fact]
    public void Accepts_what_the_relay_publishes()
    {
        Assert.True(Parse(A.Chunk("m-1", "11111111-2222-3333-4444-555555555555", "ana", 1_700_000_000_123, A.Opus(1), A.Opus(2)), out var c, out var err), err);
        Assert.Equal("ana", c!.UserId);
        Assert.Equal(2, c.Frames.Count);
        Assert.Equal(960u, c.Frames[1].Ts);
    }

    [Theory]
    [InlineData("../evil")]
    [InlineData("a/b")]
    [InlineData("")]
    [InlineData("has space")]
    public void User_ids_that_could_escape_an_s3_key_are_rejected(string user)
    {
        var body = T.Msg("m-1", "AudioChunk", "c1", $$"""{"callId":"c1","userId":"{{user}}","startMs":1,"durationMs":1,"frames":[{"ts":0,"data":"+A=="}]}""");
        Assert.False(Parse(body, out _, out _));
    }

    [Fact]
    public void Rejects_a_call_id_that_differs_from_the_correlation_id()
    {
        var body = T.Msg("m-1", "AudioChunk", "c1", """{"callId":"c2","userId":"ana","startMs":1,"durationMs":1,"frames":[{"ts":0,"data":"+A=="}]}""");
        Assert.False(Parse(body, out _, out var err));
        Assert.Contains("correlationId", err);
    }

    [Theory]
    [InlineData("""{"callId":"c1","userId":"ana","startMs":1,"durationMs":1,"frames":[]}""")]
    [InlineData("""{"callId":"c1","userId":"ana","startMs":1,"durationMs":1,"frames":[{"ts":0,"data":"not base64!"}]}""")]
    [InlineData("""{"callId":"c1","userId":"ana","startMs":1,"durationMs":1,"frames":[{"ts":0,"data":""}]}""")]
    [InlineData("""{"callId":"c1","userId":"ana","startMs":-5,"durationMs":1,"frames":[{"ts":0,"data":"+A=="}]}""")]
    [InlineData("""{"callId":"c1","userId":"ana","startMs":1,"frames":[{"ts":0,"data":"+A=="}]}""")]
    [InlineData("""{"callId":"c1","userId":"ana","startMs":1,"durationMs":1,"frames":[{"data":"+A=="}]}""")]
    [InlineData("""{"callId":"c1","userId":"ana","durationMs":1,"frames":[{"ts":0,"data":"+A=="}]}""")]
    public void Rejects_malformed_payloads(string payload) =>
        Assert.False(Parse(T.Msg("m-1", "AudioChunk", "c1", payload), out _, out _));

    [Fact]
    public void Rejects_other_event_types_and_unsafe_message_ids()
    {
        Assert.False(Parse(T.Msg("m-1", "CallStarted", "c1", "{}"), out _, out _));
        Assert.False(Parse(A.Chunk("../m", "c1", "ana", 1, A.Opus(1)), out _, out _));
    }
}

public class AudioProcessorTests
{
    [Fact]
    public async Task Stores_the_chunk_encrypted_and_marks_the_message()
    {
        var (p, s, k, d) = A.Processor();
        var r = await p.HandleAsync(A.Chunk("m-1", "c1", "ana", 1_700_000_000_000, A.Opus(7), A.Opus(8)), default);
        Assert.Equal(Outcome.Recorded, r);
        var (key, blob) = Assert.Single(s.Objects);
        Assert.Equal("audio/c1/ana/1700000000000-m-1.bin", key);
        Assert.Contains("m-1", d.Ids);

        var plain = AudioCrypto.Open(k.Key, blob, AudioCrypto.Aad("c1", "ana", 1_700_000_000_000));
        var frames = ChunkCodec.Parse(plain);
        Assert.Equal(2, frames.Count);
        Assert.Equal(A.Opus(7), frames[0].Data);
        Assert.False(blob.AsSpan().IndexOf(A.Opus(7).AsSpan(0, 20)) >= 0, "plaintext audio must not appear in the stored object");
    }

    [Fact] // F3: the same message five times, a single stored object
    public async Task A_message_delivered_five_times_is_stored_once()
    {
        var (p, s, _, _) = A.Processor();
        var body = A.Chunk("dup", "c1", "ana", 1_700_000_000_000, A.Opus(1));
        var outcomes = new List<Outcome>();
        for (var i = 0; i < 5; i++) outcomes.Add(await p.HandleAsync(body, default));
        Assert.Equal(Outcome.Recorded, outcomes[0]);
        Assert.All(outcomes.Skip(1), o => Assert.Equal(Outcome.Duplicate, o));
        Assert.Equal(1, s.Puts);
    }

    [Fact] // F3 race: both replicas pass SeenAsync; the one that loses MarkAsync reports a duplicate
    public async Task Losing_the_mark_race_is_a_duplicate()
    {
        var (p, s, _, d) = A.Processor();
        d.Blind = true;
        var body = A.Chunk("race", "c1", "ana", 1_700_000_000_000, A.Opus(1));
        Assert.Equal(Outcome.Recorded, await p.HandleAsync(body, default));
        Assert.Equal(Outcome.Duplicate, await p.HandleAsync(body, default));
        Assert.Single(s.Objects); // same deterministic key: overwritten, still one object
    }

    [Theory] // F4: left for the DLQ, nothing stored
    [InlineData("{broken")]
    [InlineData("""{"messageId":"m","correlationId":"c1","type":"AudioChunk","version":1,"occurredAt":"2026-11-22T10:00:00Z","payload":{"callId":"c1"}}""")]
    public async Task Poison_is_not_stored(string body)
    {
        var (p, s, _, d) = A.Processor();
        Assert.Equal(Outcome.Poison, await p.HandleAsync(body, default));
        Assert.Empty(s.Objects);
        Assert.Empty(d.Ids);
    }

    [Fact] // F2/F6: S3 down -> not marked, so the redelivery stores it
    public async Task A_failed_upload_is_retried_on_redelivery()
    {
        var (p, s, _, d) = A.Processor();
        s.FailPuts = 1;
        var body = A.Chunk("m-1", "c1", "ana", 1_700_000_000_000, A.Opus(1));
        Assert.Equal(Outcome.Failed, await p.HandleAsync(body, default));
        Assert.Empty(d.Ids);
        Assert.Equal(Outcome.Recorded, await p.HandleAsync(body, default));
        Assert.Single(s.Objects);
    }

    [Fact]
    public async Task An_unreachable_key_fails_the_message_without_storing_anything()
    {
        var (p, s, k, _) = A.Processor();
        k.Down = true;
        Assert.Equal(Outcome.Failed, await p.HandleAsync(A.Chunk("m-1", "c1", "ana", 1, A.Opus(1)), default));
        Assert.Empty(s.Objects);
    }

    [Fact] // crash after the upload but before the marker: the redelivery rewrites the same key
    public async Task Redelivery_after_a_lost_marker_overwrites_the_same_object()
    {
        var (p, s, _, d) = A.Processor();
        var body = A.Chunk("m-1", "c1", "ana", 1_700_000_000_000, A.Opus(1));
        await p.HandleAsync(body, default);
        d.Ids.Clear(); // simulate the marker never having been written
        Assert.Equal(Outcome.Recorded, await p.HandleAsync(body, default));
        Assert.Single(s.Objects);
    }
}

public class AudioExportTests
{
    [Fact]
    public async Task Joins_the_chunks_of_one_participant_into_a_playable_file_in_time_order()
    {
        var (p, s, k, _) = A.Processor();
        // delivered out of order and interleaved with another participant
        await p.HandleAsync(A.Chunk("m-3", "c1", "ana", 3_000, A.Opus(30), A.Opus(31)), default);
        await p.HandleAsync(A.Chunk("m-1", "c1", "ana", 1_000, A.Opus(10), A.Opus(11)), default);
        await p.HandleAsync(A.Chunk("m-x", "c1", "bob", 2_000, A.Opus(99)), default);
        await p.HandleAsync(A.Chunk("m-2", "c1", "ana", 2_000, A.Opus(20)), default);
        await p.HandleAsync(A.Chunk("m-o", "other-call", "ana", 1_000, A.Opus(77)), default);

        var exporter = new AudioExporter(s, k);
        var tracks = await exporter.TracksAsync("c1", default);
        Assert.Equal(["ana", "bob"], tracks.Select(t => t.UserId));
        Assert.Equal(3, tracks[0].Chunks);
        Assert.Equal(DateTimeOffset.FromUnixTimeMilliseconds(1_000), tracks[0].StartedAt);

        var ogg = await exporter.ExportOggAsync("c1", "ana", default);
        Assert.NotNull(ogg);
        Assert.Equal("OggS", Encoding.ASCII.GetString(ogg!, 0, 4));
        var fills = FindFills(ogg!);
        Assert.Equal([10, 11, 20, 30, 31], fills);      // time order, no bob, no other call
    }

    // each test packet is 40 filler bytes after the TOC: find them in the raw file
    private static List<int> FindFills(byte[] ogg)
    {
        var found = new List<int>();
        for (var i = 0; i + 39 < ogg.Length; i++)
            if (ogg[i] == 0xF8 && ogg[i + 1] == ogg[i + 39] && ogg[i + 1] != 0 && ogg[i + 1] != 0xF8 && ogg[i + 2] == ogg[i + 1])
                found.Add(ogg[i + 1]);
        return found;
    }

    [Fact]
    public async Task Nothing_recorded_means_null_and_no_tracks()
    {
        var (_, s, k, _) = A.Processor();
        var exporter = new AudioExporter(s, k);
        Assert.Null(await exporter.ExportOggAsync("c1", "ana", default));
        Assert.Empty(await exporter.TracksAsync("c1", default));
    }

    [Fact]
    public async Task A_tampered_chunk_is_never_exported()
    {
        var (p, s, k, _) = A.Processor();
        await p.HandleAsync(A.Chunk("m-1", "c1", "ana", 1_000, A.Opus(1)), default);
        s.Objects[s.Objects.Keys.Single()][^1] ^= 0xFF;
        await Assert.ThrowsAnyAsync<CryptographicException>(() => new AudioExporter(s, k).ExportOggAsync("c1", "ana", default));
    }

    [Fact]
    public async Task A_chunk_copied_under_another_participant_is_never_exported()
    {
        var (p, s, k, _) = A.Processor();
        await p.HandleAsync(A.Chunk("m-1", "c1", "ana", 1_000, A.Opus(1)), default);
        var (key, blob) = s.Objects.Single();
        s.Objects[key.Replace("/ana/", "/bob/")] = blob;  // someone with bucket access relabels the audio
        await Assert.ThrowsAnyAsync<CryptographicException>(() => new AudioExporter(s, k).ExportOggAsync("c1", "bob", default));
    }

    [Fact]
    public async Task Another_key_cannot_export()
    {
        var (p, s, _, _) = A.Processor();
        await p.HandleAsync(A.Chunk("m-1", "c1", "ana", 1_000, A.Opus(1)), default);
        await Assert.ThrowsAnyAsync<CryptographicException>(() => new AudioExporter(s, new FakeKeys()).ExportOggAsync("c1", "ana", default));
    }
}

public class AudioConfigTests
{
    [Fact]
    public void Recording_is_off_without_the_queue_and_needs_bucket_and_secret_with_it()
    {
        Func<string, string?> basic(params (string, string)[] extra) => k =>
            new Dictionary<string, string> { ["CALL_EVENTS_QUEUE"] = "q", ["AUDIT_TABLE"] = "a", ["PROCESSED_TABLE"] = "p", ["AUDIT_API_TOKEN"] = "t" }
                .Concat(extra.Select(e => KeyValuePair.Create(e.Item1, e.Item2))).ToDictionary().GetValueOrDefault(k);

        Assert.False(VaultConfig.Load(basic()).AudioEnabled);
        var ex = Assert.Throws<InvalidOperationException>(() => VaultConfig.Load(basic(("AUDIO_CHUNKS_QUEUE", "audio-chunks"))));
        Assert.Contains("AUDIO_BUCKET", ex.Message);
        Assert.Contains("AUDIO_KEY_SECRET", ex.Message);
        var on = VaultConfig.Load(basic(("AUDIO_CHUNKS_QUEUE", "audio-chunks"), ("AUDIO_BUCKET", "b"), ("AUDIO_KEY_SECRET", "s")));
        Assert.True(on.AudioEnabled);
        Assert.Equal("b", on.AudioBucket);
    }
}
