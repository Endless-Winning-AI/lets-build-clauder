//go:build !linux && !darwin

package pad

import "errors"

// Find lists the supported pads attached to this computer.
func Find() []Found { return nil }

// AccessProblem says why this user can't open the pad, or "" when they can.
func AccessProblem(Found) string { return "" }

func open(Found, func([]byte), func()) (transport, error) {
	return nil, errors.New("this platform is not supported yet")
}
