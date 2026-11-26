package gitsign

// The supervisor's BuildCmd resolves PublicKeyRef (internal/agent's Git
// policy), so this file is on the supervisor spawn surface
// (internal/daemon/surface.go). Keep it to the reference helpers: editing it
// requires re-recording SpawnSurfaceDigest.

// PublicKeyRef is the 1Password field reference for the item's public key.
func PublicKeyRef(ref string) string { return ref + "/public key" }

// PrivateKeyRef is the 1Password field reference for the item's private key
// in OpenSSH format (the default PKCS#8 form does not parse as an SSH key).
func PrivateKeyRef(ref string) string { return ref + "/private key?ssh-format=openssh" }
