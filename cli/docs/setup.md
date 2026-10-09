# Mist CLI

Requires Go 1.25.1. Build from the repository root:

```bash
go -C cli build -o ../bin/mist .
bin/mist --help
```

The default API URL is `http://127.0.0.1:3000`. Override it using
`MIST_API_URL`, `--api-url`, or the configuration's `api_base_url` field.
The production endpoint is `http://100.73.139.66:8088/api`. Use it from a
Tailnet device or either SSH host, then authenticate:

```bash
export MIST_API_URL=http://100.73.139.66:8088/api
bin/mist auth login --email your-member-email@example.org
```

The password prompt does not echo. Automation can use `--password-stdin`.
The CLI saves a real session cookie in a mode-0600 config; `--config` selects
an alternate file. Logout with `bin/mist auth logout` revokes the session.

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
otherwise the API chooses a runtime for the compute type. Upload datasets through the website, then pass `--dataset dataset-ID` to attach
one read-only at `/inputs`. Dependencies belong in an approved image.
Result files are downloadable from the website's job Files panel.

NVIDIA counts are whole GPUs (one or two); TT counts are whole n300 boards
(one to four, two chips each). Scripts must use the corresponding runtime.
The TT profile uses QuietBox's existing TT-Metal installation. For a small
built-in accelerator training check, use the Jobs page or the API's
`training-smoke` submission type.

Status, logs, cancellation, and errors come from the real API. Cancellation
does not ask for confirmation. The deployed API authenticates each session and derives job/dataset ownership
from the member account. Legacy fake-token authentication is removed.

```bash
go -C cli test ./...
```
