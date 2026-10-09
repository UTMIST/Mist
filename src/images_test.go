package main

import "testing"

func TestCustomImagePreservesEntrypointAndWorkingDirectory(t *testing.T) {
	e := testExecutor()
	image := "registry.example/training:v1"
	e.allowedImages[image] = true
	req, err := e.normalize(CreateJobRequest{Image: image, Args: []string{"--epochs", "10"}})
	if err != nil {
		t.Fatal(err)
	}
	job := e.buildJob("image-test", req)
	c := job.Spec.Template.Spec.Containers[0]
	if len(c.Command) != 0 || c.WorkingDir != "" || len(c.Args) != 2 {
		t.Fatalf("overrode image defaults: %+v", c)
	}
	req.WorkingDirectory = "/app"
	if c := e.buildJob("override-test", req).Spec.Template.Spec.Containers[0]; c.WorkingDir != "/app" {
		t.Fatal("explicit working directory ignored")
	}
	if _, err := e.normalize(CreateJobRequest{Image: image, WorkingDirectory: "relative/path"}); err == nil {
		t.Fatal("accepted relative working directory")
	}
	if _, err := e.normalize(CreateJobRequest{}); err == nil {
		t.Fatal("empty submission silently ran a default image")
	}
}

func TestTenstorrentCustomImageRestrictionIsInCatalogAndValidation(t *testing.T) {
	e := testExecutor()
	e.allowedImages["custom/tt:v1"] = true
	if _, err := e.normalize(CreateJobRequest{Image: "custom/tt:v1", Accelerator: "tenstorrent"}); err == nil {
		t.Fatal("accepted incompatible TT image")
	}
	for _, image := range e.imageCatalog().Images {
		for _, accelerator := range image.Accelerators {
			if accelerator == "tenstorrent" && image.Reference != ttImage {
				t.Fatal("catalog offers incompatible TT image")
			}
		}
	}
}
