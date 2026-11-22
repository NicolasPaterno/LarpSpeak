// audit-vault: consumes call-events (SNS -> SQS) and keeps an append-only audit trail per call.
using Amazon;
using Amazon.DynamoDBv2;
using Amazon.SQS;
using AuditVault;

var cfg = VaultConfig.Load(Environment.GetEnvironmentVariable);
var builder = WebApplication.CreateBuilder(args);
builder.WebHost.UseUrls($"http://+:{Environment.GetEnvironmentVariable("PORT") ?? "8080"}");
// finish the in-flight SQS long poll (10s) before exiting; compose stop_grace_period is longer
builder.Services.Configure<HostOptions>(o => o.ShutdownTimeout = TimeSpan.FromSeconds(15));

builder.Services.AddSingleton(cfg);
builder.Services.AddSingleton<IAmazonDynamoDB>(_ => new AmazonDynamoDBClient(new AmazonDynamoDBConfig { RegionEndpoint = RegionEndpoint.GetBySystemName(cfg.Region), ServiceURL = cfg.EndpointUrl }));
builder.Services.AddSingleton<IAmazonSQS>(_ => new AmazonSQSClient(new AmazonSQSConfig { RegionEndpoint = RegionEndpoint.GetBySystemName(cfg.Region), ServiceURL = cfg.EndpointUrl }));
builder.Services.AddSingleton<IAuditStore, DynamoAuditStore>();
builder.Services.AddSingleton(TimeProvider.System);
builder.Services.AddSingleton<MessageProcessor>();
builder.Services.AddSingleton<IMessageQueue>(sp => new SqsQueue(sp.GetRequiredService<IAmazonSQS>(), cfg.CallEventsQueue));
builder.Services.AddSingleton<QueueConsumer>();
builder.Services.AddHostedService<ConsumerService>();

var app = builder.Build();
AuditApi.Map(app, cfg);
app.Logger.LogInformation("audit-vault starting, aws endpoint {Endpoint}, queue {Queue}, auth {Auth}",
    cfg.EndpointUrl ?? "(default)", cfg.CallEventsQueue, cfg.ApiToken is null ? "off" : "bearer token");
app.Run();
