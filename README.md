# Mist

Mist: UTMIST's Compute Platform

## Runtime requirements

- Go 1.25.1 (also satisfies the backend's Go 1.24.6 requirement)
- Node.js 22.16.0 or newer, with npm
- Docker Engine and the Docker Compose plugin (`docker compose`)
- Permission to run `docker ps` as the same user that runs the backend

## Run locally

Ensure `go version`, `node --version`, and `docker ps` work before starting.
Run the following from the repository root:

```bash
docker compose up -d redis
docker build -t pytorch-cpu src/docker/pytorch-cpu
```

Start the backend in one terminal. Run it from `src` so it finds its logging configuration:

```bash
cd src
go run .
```

Start the frontend in another terminal:

```bash
cd web-interface
npm install --package-lock=false
npm run dev -- --port 3001
```

Open http://localhost:3001/jobs. The API uses http://localhost:3000.
The repository includes a Bun lockfile; npm installation does not use that lockfile.

The optional CLI can be built from a third terminal:

```bash
cd cli
go build -o ../bin/mist .
../bin/mist --help
```

The frontend and CLI currently contain mock data and placeholder actions. The
backend's job execution is also incomplete; starting these services does not yet
provide end-to-end workload execution.

Stop the backend/frontend with Ctrl+C in their terminals, then run
`docker compose stop redis` from the repository root to stop Redis without deleting its data.
