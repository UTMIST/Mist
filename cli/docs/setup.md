## Trying out cli

If you want to try running the cli:

- `cd cli`
- `go build -o ../bin/mist .`
- `../bin/mist {parentcommand} {subcommand}`
- `(Or if you are on windows): go build -o ..\bin\mist.exe .`

Running cli commands:

- `Windows: ".\bin\mist.exe --help"`

To run cli unit tests

- `cd cli`
- `cd cmd`
- To run ALL tests:
  - `go test -v`
- To run a SINGULAR test:
  - `go test -run (Test Name) -v`
