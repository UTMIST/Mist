package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

func TestRunCommand(t *testing.T) {
	for _, scenario := range []string{"success", "nonzero exit", "start failure", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			var removed atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/containers/create"):
					var request struct {
						Cmd        []string
						HostConfig container.HostConfig
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					if len(request.HostConfig.DeviceRequests) != 1 || request.HostConfig.NetworkMode != "none" || len(request.Cmd) != 2 {
						t.Errorf("missing GPU request or isolation: %+v", request)
					}
					w.WriteHeader(http.StatusCreated)
					fmt.Fprint(w, `{"Id":"test-workload"}`)
				case strings.HasSuffix(r.URL.Path, "/start"):
					if scenario == "start failure" {
						w.WriteHeader(500)
						fmt.Fprint(w, `{"message":"GPU unavailable"}`)
						return
					}
					w.WriteHeader(http.StatusNoContent)
				case strings.HasSuffix(r.URL.Path, "/wait"):
					if scenario == "timeout" {
						<-r.Context().Done()
						return
					}
					code := 0
					if scenario == "nonzero exit" {
						code = 1
					}
					fmt.Fprintf(w, `{"StatusCode":%d}`, code)
				case strings.HasSuffix(r.URL.Path, "/logs"):
					w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
					fmt.Fprint(stdcopy.NewStdWriter(w, stdcopy.Stdout), "measured output\n")
				case r.Method == http.MethodDelete:
					removed.Store(true)
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			cli, err := client.NewClientWithOpts(client.WithHost(server.URL), client.WithVersion("1.49"))
			if err != nil {
				t.Fatal(err)
			}
			defer cli.Close()
			mgr := NewDockerMgr(cli, 1, 1)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			output, err := mgr.RunCommand(ctx, "test-image", []string{"python", "benchmark.py"}, true)
			if (err == nil) != (scenario == "success") {
				t.Fatalf("scenario %s: unexpected error %v", scenario, err)
			}
			if scenario == "success" && output != "measured output\n" {
				t.Fatalf("lost logs: %q", output)
			}
			if !removed.Load() || len(mgr.containers) != 0 {
				t.Fatal("workload container was not cleaned up")
			}
		})
	}
}
