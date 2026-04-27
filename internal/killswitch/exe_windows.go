//go:build windows

package killswitch

import "os"

// executableSelf is the real implementation of "what's my exe path".
func executableSelf() (string, error) {
	return os.Executable()
}
