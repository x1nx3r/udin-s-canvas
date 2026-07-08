# Deployment

## The Binary

`go build` produces a static binary. That binary contains your entire
application — server logic, templates, CSS, images, documentation. It has
no external dependencies at runtime. You can copy it to any Linux amd64
machine and run it.

```bash
make build
# output: bin/server
PORT=8080 ./bin/server
```

The binary listens on whatever `PORT` says (default `3001`). Bind it
behind a reverse proxy (nginx, Caddy, Cloudflare Tunnel) or expose it
directly. It doesn't care.

## Embedded Assets

Static files live in `app/assets/public/` and are compiled into the binary
at build time with `//go:embed`:

```go
//go:embed public/*
var Public embed.FS
```

The HTTP handler opens files from this embedded filesystem, not from disk.
This matters on serverless platforms (Vercel, Lambda, Fly.io) where the
container filesystem is read-only or laid out differently than your local
machine. There is no `public/` directory at runtime. There is no path to
resolve. The file is in memory or it doesn't exist.

To add a new static file, drop it in `app/assets/public/` and rebuild. No
configuration. No CDN upload step (though you should use a CDN for
production traffic — this is a local optimization, not a replacement for
proper caching).

## Vercel

Vercel supports Go natively. Push the repository, connect it in the
dashboard, and Vercel runs `go build` on `main.go` automatically.

The `vercel.json`:

```json
{
    "src": "/(.*)",
    "dest": "/main.go"
}
```

This rewrites all requests to the Go binary. No static file serving on
Vercel's side — the binary handles everything, including static assets,
from its embedded filesystem.

Important: the `.templ` files must be pre-compiled before pushing. Vercel
does not run `templ generate`. The generated `_templ.go` files must exist
in the repository. Run `make setup` locally, commit the generated files,
and push.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT`   | `3001`  | HTTP listen port |

That's the only variable the application reads directly. Database
connection strings, API keys, S3 credentials — none of these are handled
by the framework. Your handler code reads them with `os.Getenv()` like you
would in any Go application.

## Cold Starts

Go binaries start in under 10ms on modern hardware. On Vercel's serverless
infrastructure, the container startup time dominates the cold start
latency, not the application startup. Your users will wait roughly 200ms
for the first request after a period of inactivity. This is the same as
any compiled language on Vercel and significantly faster than Node.js or
Python cold starts.

## The `go mod tidy` Problem

`templ` generates `_templ.go` files outside the root module scope. If you
run `go mod tidy`, Go's module analyzer may remove `github.com/a-h/templ`
from `go.mod` because it doesn't see any direct import of the templ
package — the generated files reference it, but `go mod tidy` checks
source files, not generated output.

The fix is on line 12 of `main.go`:

```go
import _ "github.com/a-h/templ"
```

This anonymous import locks the dependency in place. `go mod tidy` sees
it, knows the package is used, and leaves it alone. Without this line,
your next `go mod tidy` will delete templ, the next `go build` will fail,
and you'll spend ten minutes figuring out why.
