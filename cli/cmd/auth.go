package cmd

type AuthCmd struct {
	Login LoginCmd `cmd:"" help:"Log in to your account"`
	// Logout LogoutCmd     `cmd:"" help:"Log out of your account"`
	// Status AuthStatusCmd `cmd:"" help:"Check your authentication status" default:1`
}

func (a *AuthCmd) Run() error {
	// Possible fallback if no subcommand is provided
	// fmt.Println("(auth root) – try 'mist auth login|logout|status' or mist help")
	return nil
}
