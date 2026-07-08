# Routing

Routing uses Go 1.22's `http.ServeMux`. No third-party router. No `chi`,
`gorilla/mux`, `echo`, `fiber`, `gin`, or whatever the Go community decided
was the standard this week. The standard library does method-based routing
and path parameters now. It shipped in Go 1.22. If you're on an older
version, upgrade.

## Basic Routes

```go
mux := http.NewServeMux()

mux.HandleFunc("GET /{$}", app.PageHandler)
mux.HandleFunc("GET /wisdom", app.WisdomHandler)
mux.HandleFunc("POST /chart", app.ChartHandler)
```

The HTTP method prefix is mandatory. `"GET /wisdom"` matches only GET
requests to `/wisdom`. `"POST /chart"` matches only POST requests to
`/chart`. If you omit the method prefix (e.g. just `"/wisdom"`), it
matches any method. Don't do that unless you have a reason.

## Path Parameters

```go
mux.HandleFunc("GET /docs/{slug}", docs.PageHandler)
```

`{slug}` captures anything after `/docs/`. Access it from the request:

```go
slug := r.PathValue("slug")
```

`PathValue` returns the matched segment. If the route had
`GET /files/{path}` and someone visits `/files/images/logo.jpg`, the
handler won't match because `{slug}` doesn't span slashes by default.
Use `{path}` with a `...` pattern if you need multi-segment matching.

## Wildcard Catch-All

```go
mux.HandleFunc("GET /{file}", func(w http.ResponseWriter, r *http.Request) {
    fileName := r.PathValue("file")
    // serve from embedded public FS
})
```

This catches any single-segment request that didn't match a more specific
route. That's key — `http.ServeMux` matches longest prefix first. More
specific routes get tried before the catch-all. So
`GET /docs/getting-started` hits the `{slug}` handler, not the `{file}`
handler.

If the catch-all tries and fails to open the file, it returns 404.
Otherwise it serves the file with the correct `Content-Type` inferred
from the extension.

## Route Registration in main.go

All routes live in `main.go`. There is no file-based routing. You don't
create a file in a folder and have it become a route. You open `main.go`,
find the `mux.HandleFunc` section, and add a line. This is intentional.
Having all routes in one file means you can audit the entire URL space
without grepping through 40 files.

If you have 50 routes, you'll have 50 lines of route registration. You can
automate this with a loop and a map if it bothers you. It doesn't bother
me.

## Route Ordering

```go
// More specific first
mux.HandleFunc("GET /{$}", app.PageHandler)
mux.HandleFunc("GET /docs", docs.PageHandler)
mux.HandleFunc("GET /docs/{slug}", docs.PageHandler)

// Less specific last
mux.HandleFunc("GET /{file}", staticHandler)
```

`/` matches only the root path (because of `{$}`). `/docs` matches the
exact docs path. `/docs/{slug}` matches any docs sub-page. `/{file}` is
the catch-all for static assets. If you put the catch-all first, it will
eat every request before the specific routes get a chance. `http.ServeMux`
prevents this at compile time by enforcing that `{file}` doesn't conflict
with registered patterns.

## Grouping Routes by Package

The convention is to define a handler function in each package and
register it in `main.go`. For example, `app/docs/page.go` exports
`PageHandler`, and `main.go` registers it:

```go
mux.HandleFunc("GET /docs/{slug}", docs.PageHandler)
```

The handler lives next to its template (`page.templ`) in the same
directory. This is the extent of our "colocation." No `layout.tsx` + `page.tsx`
+ `loading.tsx` + `error.tsx` file salad. A handler and a template.
