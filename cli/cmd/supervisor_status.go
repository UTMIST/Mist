package cmd 
import (
	"fmt"
	"mist/cli/api"
	// "strings"
)

// Support both /status and /status/ID endpoints 
type SupervisorStatusCmd struct {
	ID string `arg:"" optional help: "Supervisor ID (optional)"`
}

func(s *SupervisorStatusCmd) Run() error {
	client := cli.NewAPIClient("http://localhost:3000")

	if s.ID == "" {
		raw, err := client.GetAllSupervisorsStatuses() 
		if err != nil {
			fmt.Println("Error: ", err)
			return nil 
		}

		fmt.Println("All Supervisor Status Response: ", raw)
		return nil 
	} 
		
	raw, err := client.GetSupervisorStatusByID(s.ID)
	if err != nil {
		fmt.Println("Error: ", err)
		return nil 
	}

	fmt.Println("Supervisor Status by ID Response: ", raw)
	return nil 

	// We can pretty this up later, most likely to JSON format, hence raw 
}