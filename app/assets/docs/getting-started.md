# Getting Started

## Prerequisites

- Go 1.22 or later
- Make
- A willingness to write HTML on the server like it's 1999 (it's fine, I promise)

That's it. No Node.js. No `nvm`. No `fnm`. No `.nvmrc`. No "please install
the correct version of Node using our proprietary version manager that
conflicts with the other three version managers you already have." You have
Go. Go is enough.

## Clone

```bash
git clone https://github.com/x1nx3r/gotth.git
cd gotth
make setup
```

`make setup` downloads the standalone Tailwind v4 compiler, tidies the Go
module graph, compiles the initial `.templ` files, and generally gets the
workspace into a state where things compile. It takes about 3 seconds.

## Dev Server

```bash
make dev
```

This runs two things in parallel:

1. `tailwindcss --watch` — scans your files for class names and rebuilds
   `globals.css.output` whenever you save. No PostCSS. No config. No
   "watcher detected a change but the pipeline is still initializing."

2. `air` — a live-reload proxy that listens on port `3000`, forwards to
   your Go app on port `3001`, and injects a WebSocket reload script into
   every HTML response. When you save a `.go` or `.templ` file, `air`
   recompiles the binary and the browser tab reloads automatically.

The proxy exists because Vercel's serverless containers don't support
WebSocket connections during cold boot, so the hot-reload layer is
isolated to development. In production, you hit the Go binary directly.

Opening `http://localhost:3000` should show you a working page. If it
doesn't, check that port `3001` isn't occupied by another process. If it
is, kill it. If you can't kill it, change the `PORT` environment variable
in `.air.toml`.

## Production Build

```bash
make build
```

Output: `bin/server`. A single static binary containing your entire
application — templates, CSS, images, documentation, and routing logic.
Copy it to a server, set `PORT=8080`, run it. No dependencies. No
`node_modules` to carry over. No "but it worked on my machine."

If you're deploying to Vercel, you don't even need this step. Vercel
detects `main.go`, runs `go build` itself, and deploys the result. The
`Makefile` is for local development and non-Vercel hosts.

## File Structure

```
gotth/
├── main.go                  # entry point, router registration
├── app/
│   ├── assets/              # embedded CSS, docs, public files
│   │   └── public/          # images, favicon, static files
│   ├── components/          # reusable templ components
│   ├── docs/                # documentation pages (the one you're reading)
│   ├── layout.templ         # shared HTML shell
│   ├── page.go              # page handlers (Go)
│   └── page.templ           # page templates (Templ)
├── bin/
│   └── tailwindcss          # standalone Tailwind binary
├── .air.toml                # live-reload config
├── go.mod
├── Makefile
└── vercel.json
```

That's the whole thing. Twelve entries. You can memorize it in the time it
takes `npm install` to fail.
