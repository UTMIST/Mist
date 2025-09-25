package cmd

import (
	"errors"
	"fmt"
)

// TODO: What credentials are we taking?
type LoginCmd struct {
	Username string `arg:"" help:"Your account username"`
	Password string `arg:"" help:"Your account password"`
}

func verifyUser(username, password string) error {
	// Placeholder for actual authentication logic
	if username == "admin" && password == "password" {
		return nil
	}
	return errors.New("invalid credentials")
}

// TODO: Figure out how to handle password input without exposing it in the terminal history
// TODO: Where are we storing auth token? Are we getting JWT?
func (l *LoginCmd) Run() error {
	// mist auth login <username> <password>

	err := verifyUser(l.Username, l.Password)
	if err != nil {
		fmt.Println("Error during authentication:", err)
	}

	fmt.Println("Logging in with username:", l.Username)

	return nil
}
