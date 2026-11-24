using System.Buffers.Binary;
using System.Text;

namespace AuditVault;

/// <summary>
/// Wraps Opus packets (exactly as the browser sent them) in an Ogg Opus file (RFC 7845),
/// without decoding anything. Packets are played back to back; silence between chunks
/// (a participant that was disconnected) is not filled in.
/// </summary>
public static class OggOpus
{
    private const int PreSkip = 312, SamplesPerSecond = 48000, MaxPacketsPerPage = 50;

    public static void Write(Stream output, IReadOnlyList<OpusFrame> frames, uint serial)
    {
        var packets = frames.Select(f => f.Data).Where(p => Samples(p) > 0).ToList();
        var channels = packets.Count > 0 && (packets[0][0] & 0x04) != 0 ? 2 : 1;
        uint seq = 0;

        var head = new byte[19];
        "OpusHead"u8.CopyTo(head);
        head[8] = 1;                                              // version
        head[9] = (byte)channels;
        BinaryPrimitives.WriteUInt16LittleEndian(head.AsSpan(10), PreSkip);
        BinaryPrimitives.WriteUInt32LittleEndian(head.AsSpan(12), SamplesPerSecond); // original input rate (informative)
        // bytes 16-17: output gain 0; byte 18: channel mapping family 0
        Page(output, 0x02, 0, serial, seq++, [head]);

        var vendor = Encoding.UTF8.GetBytes("larpspeak-audit-vault");
        var tags = new byte[8 + 4 + vendor.Length + 4];
        "OpusTags"u8.CopyTo(tags);
        BinaryPrimitives.WriteUInt32LittleEndian(tags.AsSpan(8), (uint)vendor.Length);
        vendor.CopyTo(tags, 12);                                  // last 4 bytes: zero user comments
        Page(output, 0, 0, serial, seq++, [tags]);

        long granule = PreSkip;
        var batch = new List<byte[]>();
        var segments = 0;
        for (var i = 0; i < packets.Count; i++)
        {
            var need = packets[i].Length / 255 + 1;
            if (batch.Count > 0 && (batch.Count >= MaxPacketsPerPage || segments + need > 255))
            {
                Page(output, 0, granule, serial, seq++, batch);
                batch = [];
                segments = 0;
            }
            batch.Add(packets[i]);
            segments += need;
            granule += Samples(packets[i]);
        }
        // the last page carries the end-of-stream flag (an empty recording still gets a valid, empty last page)
        Page(output, 0x04, granule, serial, seq, batch);
    }

    /// <summary>Samples (48 kHz) in an Opus packet, from its TOC byte (RFC 6716 §3.1). 0 if malformed.</summary>
    public static int Samples(byte[] packet)
    {
        if (packet.Length == 0) return 0;
        var toc = packet[0];
        var config = toc >> 3;
        var perFrame = config switch
        {
            < 12 => (config % 4) switch { 0 => 480, 1 => 960, 2 => 1920, _ => 2880 },  // SILK: 10/20/40/60 ms
            < 16 => (config % 2) == 0 ? 480 : 960,                                       // hybrid: 10/20 ms
            _ => (config % 4) switch { 0 => 120, 1 => 240, 2 => 480, _ => 960 },         // CELT: 2.5/5/10/20 ms
        };
        int frames;
        switch (toc & 3)
        {
            case 0: frames = 1; break;
            case 1:
            case 2: frames = 2; break;
            default:
                if (packet.Length < 2) return 0;
                frames = packet[1] & 0x3F;
                break;
        }
        var total = frames * perFrame;
        return frames == 0 || total > 5760 ? 0 : total;                                  // max 120 ms per packet
    }

    private static void Page(Stream output, byte type, long granule, uint serial, uint seq, IReadOnlyList<byte[]> packets)
    {
        var lacing = new List<byte>();
        foreach (var p in packets)
        {
            for (var n = p.Length; n >= 255; n -= 255) lacing.Add(255);
            lacing.Add((byte)(p.Length % 255));
        }
        var dataLen = packets.Sum(p => p.Length);
        var page = new byte[27 + lacing.Count + dataLen];
        "OggS"u8.CopyTo(page);
        page[5] = type;
        BinaryPrimitives.WriteInt64LittleEndian(page.AsSpan(6), granule);
        BinaryPrimitives.WriteUInt32LittleEndian(page.AsSpan(14), serial);
        BinaryPrimitives.WriteUInt32LittleEndian(page.AsSpan(18), seq);
        page[26] = (byte)lacing.Count;
        lacing.CopyTo(page, 27);
        var o = 27 + lacing.Count;
        foreach (var p in packets) { p.CopyTo(page, o); o += p.Length; }
        BinaryPrimitives.WriteUInt32LittleEndian(page.AsSpan(22), Crc(page));            // computed with the CRC field zeroed
        output.Write(page);
    }

    // Ogg CRC-32: polynomial 0x04C11DB7, MSB first, no inversion, initial value 0.
    private static readonly uint[] Table = BuildTable();

    private static uint[] BuildTable()
    {
        var t = new uint[256];
        for (uint i = 0; i < 256; i++)
        {
            var r = i << 24;
            for (var k = 0; k < 8; k++) r = (r & 0x80000000) != 0 ? (r << 1) ^ 0x04C11DB7 : r << 1;
            t[i] = r;
        }
        return t;
    }

    public static uint Crc(ReadOnlySpan<byte> data)
    {
        uint crc = 0;
        foreach (var b in data) crc = (crc << 8) ^ Table[(byte)(crc >> 24) ^ b];
        return crc;
    }
}
