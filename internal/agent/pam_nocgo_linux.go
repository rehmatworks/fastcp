//go:build linux && !cgo

package agent

import "errors"

func pamAuthenticate(username, password string) error {
	return errors.New("this agent was built without PAM support")
}
