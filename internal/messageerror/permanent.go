// Package messageerror distinguishes a rejected message from a temporary failure.
package messageerror

import "errors"

type permanent struct{ error }

func (e permanent) Unwrap() error { return e.error }

// Permanent preserves the original error for errors.Is/As and skips futile retries.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanent{err}
}

func IsPermanent(err error) bool {
	var target permanent
	return errors.As(err, &target)
}
