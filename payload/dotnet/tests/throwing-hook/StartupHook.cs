public static class StartupHook
{
    public static void Initialize() => throw new System.ApplicationException("rio-upstream-sentinel");
}
