using Amazon.SQS;
using AuditVault;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Hosting;
using Microsoft.Extensions.Logging;
using Microsoft.Extensions.Logging.Abstractions;
using Xunit;

namespace AuditVault.Tests;

/// <summary>
/// Regression: both consumers were registered with AddHostedService, whose TryAddEnumerable
/// kept only the first ConsumerService, so the audio-chunks queue was never read.
/// </summary>
public class ConsumerRegistrationTests
{
    private static ServiceProvider Build(bool audio)
    {
        var s = new ServiceCollection();
        s.AddSingleton(typeof(ILogger<>), typeof(NullLogger<>));
        s.AddSingleton<IAmazonSQS>(_ => new AmazonSQSClient("test", "test",
            new AmazonSQSConfig { ServiceURL = "http://localhost:4566" }));
        s.AddSingleton<IAuditStore, FakeStore>();
        s.AddSingleton(TimeProvider.System);
        s.AddSingleton<MessageProcessor>();
        s.AddQueueConsumer<MessageProcessor>("call-events");
        if (audio)
        {
            s.AddSingleton<IAudioStore, FakeAudioStore>();
            s.AddSingleton<IAudioKeyProvider>(new FakeKeys());
            s.AddSingleton<IProcessedStore, FakeProcessed>();
            s.AddSingleton<AudioProcessor>();
            s.AddQueueConsumer<AudioProcessor>("audio-chunks");
        }
        return s.BuildServiceProvider();
    }

    [Fact]
    public void Audio_on_runs_one_consumer_per_queue()
    {
        using var sp = Build(audio: true);
        var hosted = sp.GetServices<IHostedService>().ToList();
        Assert.Equal(2, hosted.Count);
        Assert.All(hosted, h => Assert.IsType<ConsumerService>(h));
        Assert.NotSame(hosted[0], hosted[1]);
    }

    [Fact]
    public void Audio_off_runs_only_the_call_events_consumer()
    {
        using var sp = Build(audio: false);
        Assert.Single(sp.GetServices<IHostedService>());
    }

    [Fact]
    public void AddHostedService_would_drop_the_second_consumer()
    {
        // Documents the framework behaviour behind the bug, so nobody "simplifies" back to it.
        var s = new ServiceCollection();
        s.AddHostedService(_ => new ConsumerService(null!));
        s.AddHostedService(_ => new ConsumerService(null!));
        Assert.Single(s, d => d.ServiceType == typeof(IHostedService));
    }
}
