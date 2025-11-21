package cmd 
import (
	// "encoding/json"
	"fmt"
	// "strings"
	"mist/cli/api"
)

type SupervisorListCmd struct {
	// --active flag 
	Active bool "help: Show only active supervisors"
}

func (s *SupervisorListCmd) Run() error {
	client := cli.NewAPIClient("http://localhost:3000")

	raw, err := client.GetSupervisors(s.Active)
	if err != nil {
		fmt.Println("Error: ", err)
		return nil 
	}

	fmt.Println("Supervisor List Response: ", raw) 
	return nil 
	// We can pretty this up later, hence raw 
}