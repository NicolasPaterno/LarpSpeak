// audit-vault: consumes call-events (SNS -> SQS) and keeps an append-only audit trail per call.
// With AUDIO_CHUNKS_QUEUE set it also stores the call recordings, encrypted, in S3.
using Amazon;
using Amazon.DynamoDBv2;
using Amazon.Runtime;
using Amazon.S3;
using Amazon.SecretsManager;
using Amazon.SQS;
using AuditVault;

VaultConfig cfg;
try { cfg = VaultConfig.Load(Environment.GetEnvironmentVariable); }
catch (InvalidOperationException ex)
{
    // Exit with an error instead of letting the exception escape: as PID 1 in the container an unhandled
    // exception left the process spinning (no exit, no restart). Fail fast, the orchestrator restarts it.
    Console.Error.WriteLine($"audit-vault: {ex.Message}");
    return 1;
}
var builder = WebApplication.CreateBuilder(args);
builder.WebHost.UseUrls($"http://+:{Environment.GetEnvironmentVariable("PORT") ?? "8080"}");
// finish the in-flight SQS long poll (10s) before exiting; compose stop_grace_period is longer
builder.Services.Configure<HostOptions>(o => o.ShutdownTimeout = TimeSpan.FromSeconds(15));

var region = RegionEndpoint.GetBySystemName(cfg.Region);
// Same policy as the Go services: a hung connection (paused or partitioned broker) must fail after 15s
// (longer than the 10s long poll) instead of blocking forever, and the SDK does not retry on its own:
// the consumer loop owns retries and backoff, so outages show up in the logs (R6, F6).
T Aws<T>(T c) where T : ClientConfig
{
    c.RegionEndpoint = region;
    c.ServiceURL = cfg.EndpointUrl;
    c.Timeout = TimeSpan.FromSeconds(15);
    c.MaxErrorRetry = 0;
    return c;
}
builder.Services.AddSingleton(cfg);
builder.Services.AddSingleton<IAmazonDynamoDB>(_ => new AmazonDynamoDBClient(Aws(new AmazonDynamoDBConfig())));
builder.Services.AddSingleton<IAmazonSQS>(_ => new AmazonSQSClient(Aws(new AmazonSQSConfig())));
builder.Services.AddSingleton<IAuditStore, DynamoAuditStore>();
builder.Services.AddSingleton(TimeProvider.System);
builder.Services.AddSingleton<MessageProcessor>();

// call events -> audit trail
builder.Services.AddQueueConsumer<MessageProcessor>(cfg.CallEventsQueue);

if (cfg.AudioEnabled)
{
    builder.Services.AddSingleton<IAmazonS3>(_ => new AmazonS3Client(Aws(new AmazonS3Config
    {
        ForcePathStyle = true, // MiniStack and other S3-compatible servers have no per-bucket host names
        // only checksum when S3 requires it: emulators do not all understand the SDK's default trailing checksums
        RequestChecksumCalculation = RequestChecksumCalculation.WHEN_REQUIRED,
        ResponseChecksumValidation = ResponseChecksumValidation.WHEN_REQUIRED,
    })));
    builder.Services.AddSingleton<IAmazonSecretsManager>(_ => new AmazonSecretsManagerClient(Aws(new AmazonSecretsManagerConfig())));
    builder.Services.AddSingleton<IAudioStore, S3AudioStore>();
    builder.Services.AddSingleton<IAudioKeyProvider, SecretsManagerKeyProvider>();
    builder.Services.AddSingleton<IProcessedStore, DynamoProcessedStore>();
    builder.Services.AddSingleton<AudioProcessor>();
    builder.Services.AddSingleton<AudioExporter>();

    // audio chunks -> encrypted objects in S3 (competing consumers: several replicas may share the queue, R4)
    builder.Services.AddQueueConsumer<AudioProcessor>(cfg.AudioQueue!);
}

var app = builder.Build();
AuditApi.Map(app, cfg);
app.Logger.LogInformation("audit-vault starting, aws endpoint {Endpoint}, queue {Queue}, auth {Auth}, recording {Recording}",
    cfg.EndpointUrl ?? "(default)", cfg.CallEventsQueue, "bearer token",
    cfg.AudioEnabled ? $"on (queue {cfg.AudioQueue}, bucket {cfg.AudioBucket})" : "off");
app.Run();
return 0;
