using System.Globalization;
using Amazon.DynamoDBv2;
using Amazon.DynamoDBv2.Model;

namespace AuditVault;

/// <summary>
/// DynamoDB layout (hash key "pk" only, like every table created by infra/init):
///   audit-processed: pk = messageId                      -> idempotency marker (R1)
///   audit-records:   pk = "call#{callId}", events = [..]  -> append-only trail of one call
/// A record and its marker are written in ONE transaction: a crash can never leave a
/// recorded event without its marker, nor a marker without its event.
/// </summary>
public sealed class DynamoAuditStore(IAmazonDynamoDB db, VaultConfig cfg) : IAuditStore
{
    private const string Fmt = "O";

    public async Task RecordAsync(AuditEvent e, CancellationToken ct)
    {
        var entry = new AttributeValue
        {
            M = new Dictionary<string, AttributeValue>
            {
                ["messageId"] = S(e.MessageId),
                ["type"] = S(e.Type),
                ["version"] = new AttributeValue { N = e.Version.ToString(CultureInfo.InvariantCulture) },
                ["occurredAt"] = S(e.OccurredAt.ToString(Fmt, CultureInfo.InvariantCulture)),
                ["receivedAt"] = S(e.ReceivedAt.ToString(Fmt, CultureInfo.InvariantCulture)),
                ["payload"] = S(e.Payload),
                ["sha256"] = S(e.Sha256),
            }
        };
        var request = new TransactWriteItemsRequest
        {
            TransactItems =
            [
                // item 0 must stay the idempotency guard: its failure means "duplicate"
                new TransactWriteItem
                {
                    Put = new Put
                    {
                        TableName = cfg.ProcessedTable,
                        Item = new Dictionary<string, AttributeValue>
                        {
                            ["pk"] = S(e.MessageId),
                            ["at"] = S(e.ReceivedAt.ToString(Fmt, CultureInfo.InvariantCulture)),
                        },
                        ConditionExpression = "attribute_not_exists(pk)",
                    }
                },
                new TransactWriteItem
                {
                    Update = new Update
                    {
                        TableName = cfg.AuditTable,
                        Key = new Dictionary<string, AttributeValue> { ["pk"] = S(CallKey(e.CallId)) },
                        UpdateExpression = "SET events = list_append(if_not_exists(events, :empty), :e), callId = :cid, updatedAt = :now",
                        ExpressionAttributeValues = new Dictionary<string, AttributeValue>
                        {
                            [":empty"] = new AttributeValue { L = [], IsLSet = true },
                            [":e"] = new AttributeValue { L = [entry], IsLSet = true },
                            [":cid"] = S(e.CallId),
                            [":now"] = new AttributeValue { N = e.ReceivedAt.ToUnixTimeMilliseconds().ToString(CultureInfo.InvariantCulture) },
                        },
                    }
                },
            ]
        };
        try
        {
            await db.TransactWriteItemsAsync(request, ct);
        }
        catch (TransactionCanceledException ex) when (ex.CancellationReasons?.Count > 0 && ex.CancellationReasons[0].Code == "ConditionalCheckFailed")
        {
            throw new DuplicateMessageException(e.MessageId);
        }
    }

    public async Task<CallTrail?> GetTrailAsync(string callId, CancellationToken ct)
    {
        var res = await db.GetItemAsync(new GetItemRequest
        {
            TableName = cfg.AuditTable,
            Key = new Dictionary<string, AttributeValue> { ["pk"] = S(CallKey(callId)) },
            ConsistentRead = true,
        }, ct);
        return res.IsItemSet ? ToTrail(res.Item) : null;
    }

    public async Task<IReadOnlyList<CallTrail>> ListRecentAsync(int limit, CancellationToken ct)
    {
        var found = new List<(long updatedAt, CallTrail trail)>();
        Dictionary<string, AttributeValue>? last = null;
        do
        {
            var res = await db.ScanAsync(new ScanRequest
            {
                TableName = cfg.AuditTable,
                FilterExpression = "begins_with(pk, :p)",
                ExpressionAttributeValues = new Dictionary<string, AttributeValue> { [":p"] = S("call#") },
                ExclusiveStartKey = last,
            }, ct);
            foreach (var item in res.Items)
            {
                var updated = item.TryGetValue("updatedAt", out var u) && long.TryParse(u.N, CultureInfo.InvariantCulture, out var ms) ? ms : 0;
                found.Add((updated, ToTrail(item).Summary()));
            }
            last = res.LastEvaluatedKey is { Count: > 0 } ? res.LastEvaluatedKey : null;
        } while (last is not null);
        return found.OrderByDescending(f => f.updatedAt).Take(limit).Select(f => f.trail).ToList();
    }

    private static CallTrail ToTrail(Dictionary<string, AttributeValue> item)
    {
        var callId = item.TryGetValue("callId", out var c) ? c.S : item["pk"].S["call#".Length..];
        var events = (item.TryGetValue("events", out var l) && l.IsLSet ? l.L : [])
            .Select(a => a.M)
            .Select(m => new AuditEvent(
                m["messageId"].S, callId, m["type"].S, int.Parse(m["version"].N, CultureInfo.InvariantCulture),
                DateTimeOffset.Parse(m["occurredAt"].S, CultureInfo.InvariantCulture),
                DateTimeOffset.Parse(m["receivedAt"].S, CultureInfo.InvariantCulture),
                m["payload"].S, m["sha256"].S));
        return CallTrail.Build(callId, events);
    }

    private static string CallKey(string callId) => "call#" + callId;
    private static AttributeValue S(string v) => new() { S = v };
}
