# Mist
Mist: UTMIST's Compute Platform

## Runtime Requirements
Note that you will need the following installed:
- docker
- docker-compose
- go


## Testing
Run tests on the main application by running `go test` in the `src` directory. Note that integration tests will fail without the containers in [`docker-compose.yml`](./docker-compose.yml)