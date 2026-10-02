package secrets

import "io/fs"

// checkKeyMode does nothing on Windows, which has no Unix permission bits
// to check.
func checkKeyMode(string, fs.FileInfo) error { return nil }
