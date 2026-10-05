package central

import "io/fs"

// checkPrivate does nothing on Windows, where access is governed by the
// file's ACL, which it inherits from the user's profile directory.
func checkPrivate(string, fs.FileInfo) error { return nil }
