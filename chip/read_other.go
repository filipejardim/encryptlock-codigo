//go:build !windows

package chip

import "errors"

// Read: LockKeepers on a computer are Windows only for now.
func Read() (Chip, error) {
	return Chip{}, errors.New("reading the security chip needs Windows")
}
