# Mist CLI

Requires Go 1.25.1. Build from the repository root:

```bash
go -C cli build -o ../bin/mist .
bin/mist --help
```

The default API URL is `http://127.0.0.1:3000`. Override it using
`MIST_API_URL`, `--api-url`, or the configuration's `api_base_url` field.
Start the Kubernetes API or its local port-forward first.

```bash
bin/mist job submit /path/to/train.py --compute NVIDIA --devices 1 \
  --cpu 2 --memory 2Gi --timeout 1800 --name my-training
bin/mist job submit /path/to/tt_train.py --compute TT --devices 1
bin/mist job submit /path/to/task.sh --compute CPU
bin/mist job list          # Waiting and running jobs
bin/mist job list --all    # Includes completed, failed, and cancelled jobs
bin/mist job status <job-id>
bin/mist job logs <job-id>
bin/mist job cancel <active-job-id>
```

Submission sends the actual file contents to the API. `.py` and `.sh` files
are supported, up to 32 KiB. `--image` selects an image allowed by the API;
otherwise the API chooses a runtime for the compute type. There is no file
upload for datasets or dependencies in this pilot.

NVIDIA counts are whole GPUs (one or two); TT counts are whole n300 boards
(one to four, two chips each). Scripts must use the corresponding runtime.
The TT profile uses QuietBox's existing TT-Metal installation. For a small
built-in accelerator training check, use the Jobs page or the API's
`training-smoke` submission type.

Status, logs, cancellation, and errors come from the real API. Cancellation
does not ask for confirmation. The local pilot has one configured owner;
CLI authentication commands remain placeholders and do not secure it.

```bash
go -C cli test ./...
```
