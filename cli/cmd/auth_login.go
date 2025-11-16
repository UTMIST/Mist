package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

// TODO: What credentials are we taking?
type LoginCmd struct {
}

func verifyUser(username, password string) error {
	// Placeholder for actual authentication logic
	if username == "admin" && password == "password" {
		return nil
	}
	return errors.New("invalid credentials")
}

// TODO: Figure out how to handle password input without exposing it in the terminal historyn  (go get golang.org/x/term)
// TODO: Where are we storing auth token? Are we getting JWT?

func (l *LoginCmd) Run(ctx *AppContext) error {
	// mist auth login

	fmt.Print("Username: ")

	reader := bufio.NewReader(os.Stdin)
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(strings.ToLower(username))

	fmt.Print("Password: ")
	password, _ := reader.ReadString('\n')
	password = strings.TrimSpace(strings.ToLower(password))
	err := verifyUser(username, password)
	if err != nil {
		fmt.Println("Error during authentication:", err)
	}

	fmt.Println("Logging in with username:", username)

	return nil
}
