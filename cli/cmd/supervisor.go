package cmd 

type SupervisorCmd struct {
	List SupervisorListCmd `cmd:"" help:"List all supervisors"`
	Status SupervisorStatusCmd `cmd:"" help:"View supervisor status"`
}

func(s *SupervisorCmd) Run() error{
	return nil 
}