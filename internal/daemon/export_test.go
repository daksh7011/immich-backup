package daemon

// Test-only hooks for the external daemon_test package.

// ResolveServiceEnvWith exposes resolveServiceEnv with injectable rclone
// resolution and environment lookup.
var ResolveServiceEnvWith = resolveServiceEnv
