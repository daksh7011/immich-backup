package daemon

// Test-only hooks for the external daemon_test package.

// ResolveServiceEnvWith exposes resolveServiceEnv with injectable rclone
// resolution and environment lookup.
var ResolveServiceEnvWith = resolveServiceEnv

// StableExecutableWith exposes stableExecutable with injectable PATH lookup,
// file comparison and temp dir.
var StableExecutableWith = stableExecutable

// SameFile exposes the default file comparison used by StableExecutable.
var SameFile = sameFile

// CheckUnitPath exposes checkUnitPath.
var CheckUnitPath = checkUnitPath
