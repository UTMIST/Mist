package cli 

import (
	"bytes"
	"encoding/json"
	// "fmt"
	"io"
	"net/http"
)

type APIClient struct {
	BaseURL string 
}

// Make new API client 
func NewAPIClient(baseURL string) *APIClient{
	return &APIClient{BaseURL: baseURL}
}

func (c *APIClient) SubmitJob(script string, compute string) (string, error) {
	payload := map[string]string{
		"script": script, 
		"compute": compute, 
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	url := c.BaseURL + "/jobs"
	resp, err := http.Post(url, "application.json", bytes.NewBuffer(bodyBytes))

	if err != nil {
		return "", err
	}
	defer resp.Body.Close() 

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}	
	
	return string(respBody), nil
}

func (c *APIClient) GetJobStatus(jobID string) (string, error){
	url := fmt.Sprintf("%s/jobs/status?id=%s", c.BaseURL, jobID)

	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK{
		return "", fmt.Errorf("server return %s: %s", resp.Status, body)
	}

	return string(body), nil
}