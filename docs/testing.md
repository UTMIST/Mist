## Setup

For testing, docker engine and docker-compose should be installed - see [here](https://docs.docker.com/engine/install/).
Additionally, golang should be [installed](https://go.dev/doc/install).

## Testing

Start containers by running `docker-compose up` in the main directory (docker must be installed).

Run tests on the main application by running `go test` in the `src` directory. Note that `TestIntegration` will fail without the containers in [`docker-compose.yml`](./docker-compose.yml) up and running.
