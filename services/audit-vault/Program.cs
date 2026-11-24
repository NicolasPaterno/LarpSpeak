// audit-vault: consumes call-events (SNS -> SQS) and keeps an append-only audit trail per call.
// With AUDIO_CHUNKS_QUEUE set it also stores the call recordings, encrypted, in S3.
using Amazon;
using Amazon.DynamoDBv2;
using Amazon.Runtime;
using Amazon.S3;
using Amazon.SecretsManager;
using Amazon.SQS;
using AuditVault;

var cfg = VaultConfig.Load(Environment.GetEnvironmentVariable);
var builder = WebApplication.CreateBuilder(args);
builder.WebHost.UseUrls($"http://+:{Environment.GetEnvironmentVariable("PORT") ?? "8080"}");
// finish the in-flight SQS long poll (10s) before exiting; compose stop_grace_period is longer
builder.Services.Configure<HostOptions>(o => o.ShutdownTimeout = TimeSpan.FromSeconds(15));

var region = RegionEndpoint.GetBySystemName(cfg.Region);
builder.Services.AddSingleton(cfg);
builder.Services.AddSingleton<IAmazonDynamoDB>(_ => new AmazonDynamoDBClient(new AmazonDynamoDBConfig { RegionEndpoint = region, ServiceURL = cfg.EndpointUrl }));
builder.Services.AddSingleton<IAmazonSQS>(_ => new AmazonSQSClient(new AmazonSQSConfig { RegionEndpoint = region, ServiceURL = cfg.EndpointUrl }));
builder.Services.AddSingleton<IAuditStore, DynamoAuditStore>();
builder.Services.AddSingleton(TimeProvider.System);
builder.Services.AddSingleton<MessageProcessor>();

// call events -> audit trail
builder.Services.AddHostedService(sp => new ConsumerService(new QueueConsumer(
    new SqsQueue(sp.GetRequiredService<IAmazonSQS>(), cfg.CallEventsQueue),
    sp.GetRequiredService<MessageProcessor>(),
    sp.GetRequiredService<ILogger<QueueConsumer>>())));

if (cfg.AudioEnabled)
{
    builder.Services.AddSingleton<IAmazonS3>(_ => new AmazonS3Client(new AmazonS3Config
    {
        RegionEndpoint = region, ServiceURL = cfg.EndpointUrl,
        ForcePathStyle = true, // MiniStack and other S3-compatible servers have no per-bucket host names
        // only checksum when S3 requires it: emulators do not all understand the SDK's default trailing checksums
        RequestChecksumCalculation = RequestChecksumCalculation.WHEN_REQUIRED,
        ResponseChecksumValidation = ResponseChecksumValidation.WHEN_REQUIRED,
    }));
    builder.Services.AddSingleton<IAmazonSecretsManager>(_ => new AmazonSecretsManagerClient(new AmazonSecretsManagerConfig { RegionEndpoint = region, ServiceURL = cfg.EndpointUrl }));
    builder.Services.AddSingleton<IAudioStore, S3AudioStore>();
    builder.Services.AddSingleton<IAudioKeyProvider, SecretsManagerKeyProvider>();
    builder.Services.AddSingleton<IProcessedStore, DynamoProcessedStore>();
    builder.Services.AddSingleton<AudioProcessor>();
    builder.Services.AddSingleton<AudioExporter>();

    // audio chunks -> encrypted objects in S3 (competing consumers: several replicas may share the queue, R4)
    builder.Services.AddHostedService(sp => new ConsumerService(new QueueConsumer(
        new SqsQueue(sp.GetRequiredService<IAmazonSQS>(), cfg.AudioQueue!),
        sp.GetRequiredService<AudioProcessor>(),
        sp.GetRequiredService<ILogger<QueueConsumer>>())));
}

var app = builder.Build();
AuditApi.Map(app, cfg);
app.Logger.LogInformation("audit-vault starting, aws endpoint {Endpoint}, queue {Queue}, auth {Auth}, recording {Recording}",
    cfg.EndpointUrl ?? "(default)", cfg.CallEventsQueue, cfg.ApiToken is null ? "off" : "bearer token",
    cfg.AudioEnabled ? $"on (queue {cfg.AudioQueue}, bucket {cfg.AudioBucket})" : "off");
app.Run();
