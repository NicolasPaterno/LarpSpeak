// audit-vault: skeleton. Business logic is tracked in docs/architecture.md.
var app = WebApplication.CreateBuilder(args).Build();
app.MapGet("/health", () => Results.Ok(new { status = "ok", service = "audit-vault" }));
app.Logger.LogInformation("audit-vault starting, aws endpoint {Endpoint}", Environment.GetEnvironmentVariable("AWS_ENDPOINT_URL"));
app.Run();
