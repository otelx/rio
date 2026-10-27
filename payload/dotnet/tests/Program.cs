using System.Reflection;

var assembly = Assembly.LoadFrom(args[0]);
if (assembly.GetName().Name != "Rio.Bootstrap") throw new Exception("Bootstrap identity aliases the upstream hook");
if (assembly.GetReferencedAssemblies().Any(reference => reference.Name!.StartsWith("OpenTelemetry") || reference.Name.StartsWith("System.Diagnostics")))
    throw new Exception("Bootstrap loads agent dependencies before upstream assembly resolution");

var configure = assembly.GetType("StartupHook", true)!.GetMethod("ConfigurePlugins", BindingFlags.NonPublic | BindingFlags.Static)!;
const string name = "OTEL_DOTNET_AUTO_PLUGINS";
const string plugin = "Rio.PropagationPlugin, Rio.Propagation";
var original = Environment.GetEnvironmentVariable(name);
try
{
    foreach (var (configured, expected) in new (string?, string)[] {
        (null, plugin), ("", plugin), ("  ", plugin),
        ("User.Plugin, User.Assembly", plugin + ":User.Plugin, User.Assembly"),
        (" A.Plugin, A : B.Plugin, B ", plugin + ": A.Plugin, A : B.Plugin, B "),
        (plugin, plugin),
        (plugin + ", Version=1.0.0.0, Culture=neutral, PublicKeyToken=null", plugin + ", Version=1.0.0.0, Culture=neutral, PublicKeyToken=null"),
        ("User.Plugin, User.Assembly:" + plugin, "User.Plugin, User.Assembly:" + plugin),
    })
    {
        Environment.SetEnvironmentVariable(name, configured);
        configure.Invoke(null, null);
        if (Environment.GetEnvironmentVariable(name) != expected) throw new Exception("User plugin list was changed or lost");
        configure.Invoke(null, null);
        if (Environment.GetEnvironmentVariable(name) != expected) throw new Exception("Bootstrap prepends a duplicate plugin");
    }
    var initialize = assembly.GetType("StartupHook", true)!.GetMethod("Initialize", BindingFlags.Public | BindingFlags.Static)!;
    const string failFastName = "OTEL_DOTNET_AUTO_FAIL_FAST_ENABLED";
    var failFast = Environment.GetEnvironmentVariable(failFastName);
    try
    {
        // The test output directory deliberately has no original upstream hook.
        // A bootstrap-owned packaging failure must preserve fail-fast policy.
        Environment.SetEnvironmentVariable(failFastName, "false");
        initialize.Invoke(null, BindingFlags.DoNotWrapExceptions, null, null, null);
        Environment.SetEnvironmentVariable(failFastName, "true");
        try
        {
            initialize.Invoke(null, BindingFlags.DoNotWrapExceptions, null, null, null);
            throw new Exception("Bootstrap swallowed a fail-fast setup error");
        }
        catch (FileNotFoundException) { }
    }
    finally { Environment.SetEnvironmentVariable(failFastName, failFast); }

    var originalPath = Path.Combine(Path.GetDirectoryName(Path.GetFullPath(args[0]))!, "OpenTelemetry.AutoInstrumentation.StartupHook.original.dll");
    try
    {
        File.Copy(args[1], originalPath);
        Environment.SetEnvironmentVariable(failFastName, "false");
        try
        {
            initialize.Invoke(null, BindingFlags.DoNotWrapExceptions, null, null, null);
            throw new Exception("Bootstrap swallowed the upstream application's exception");
        }
        catch (ApplicationException error) when (error.Message == "rio-upstream-sentinel") { }
    }
    finally
    {
        File.Delete(originalPath);
        Environment.SetEnvironmentVariable(failFastName, failFast);
    }
    Console.WriteLine("PASS bootstrap identity, BCL-only references, preserved resolved user plugins, default order, idempotence");
}
finally { Environment.SetEnvironmentVariable(name, original); }
