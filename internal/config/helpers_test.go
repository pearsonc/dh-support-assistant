package config

import "os"

// osWriteFile is a thin shim so the test file can avoid importing os.
// Keeps Load's test surface focused on precedence rather than IO plumbing.
var osWriteFile = os.WriteFile
