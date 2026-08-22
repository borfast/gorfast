## Task

This is a Go web service (module `example.com/shop`). Every setting in it is
currently hardcoded.

Introduce real configuration for:

- server host, port, read timeout, write timeout
- database URL, max connections
- log level, log format

Values come from three layers: built-in defaults, an optional `.env` file, and
environment variables. Environment variables win.

Write the configuration code and update `cmd/web/main.go`, `db.Open`, and
`server.New` so the service uses the loaded values instead of the hardcoded
ones. Create any new files you need.

Constraints:
- Work ONLY inside your working directory. Do not read or write anything
  outside it, and do not look for guidance in other directories.
- Do NOT run `go build`, `go get`, `go mod tidy`, or any network command.
  Dependencies cannot be fetched here. Write the code as if the dependencies
  were present.

## Reply format

When you are done, reply with EXACTLY this block and nothing else - no preamble,
no summary, no explanation:

CONFIG_FILE: <path, relative to the working dir, of the file defining the config types>
CONFIG_PACKAGE: <the package clause of that file, e.g. "package config" or "package main">
DB_OPEN_SIG: <the full signature line of db.Open after your change>
SERVER_NEW_SIG: <the full signature line of server.New after your change>
CONFIG_IMPORTERS: <every package that imports/uses the config types, comma separated>
