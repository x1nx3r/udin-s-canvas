# Components

Components are written in `.templ` files and compiled to type-safe Go
functions at build time. There is no runtime component system. No virtual
DOM. No reconciliation. No "this component rendered differently on the
server than on the client, here's a hydration error in your console."
Everything runs on the server. The output is HTML. The browser receives
HTML. The end.

## Basic Component

```templ
templ Greeting(name string) {
	<div class="text-xl font-bold">
		Hello, { name }
	</div>
}
```

Save this as `greeting.templ`. Run `templ generate`. It produces
`greeting_templ.go` with a type-safe Go function. Call it from another
template or from a handler:

```go
// from a handler
component := Greeting("Alice")
component.Render(r.Context(), w)
```

`Greeting` takes a `context.Context` and an `io.Writer`. That's the
interface. Every component does the same thing. There is no
`.withProps()`, `.mergeClass()`, or `.asServerComponent()`. It's a
function that writes to a writer.

## Layouts with Children

```templ
templ Layout(title string, currentPath string) {
	<!DOCTYPE html>
	<html>
		<head>
			<title>{ title }</title>
			<link rel="stylesheet" href="/globals.css" />
			<script src="https://unpkg.com/htmx.org@2"></script>
		</head>
		<body>
			@Navigation(currentPath)
			<main class="container">
				{ children... }
			</main>
			@Footer()
		</body>
	</html>
}
```

The `{ children... }` slot renders whatever is passed inside the
component block. Usage:

```templ
templ Page(title string, body string) {
	@Layout(title, "/docs") {
		<h1>{ title }</h1>
		<div>@templ.Raw(body)</div>
	}
}
```

`@templ.Raw` outputs unescaped HTML. Use it when you're rendering
Markdown content or any HTML string you've already sanitized. Don't use it
with user input directly unless you enjoy XSS vulnerabilities.

## Props and State

Every component parameter is explicit. There is no implicit state, no
context providers, no prop drilling because there's no component tree in
the React sense. You pass what you need:

```templ
templ Card(title string, description string, variant string) {
	<div class={ "card", templ.KV("card--danger", variant == "danger") }>
		<h2>{ title }</h2>
		<p>{ description }</p>
	</div>
}
```

`templ.KV` conditionally applies class names. The first argument is the
base class, the second is a key-value pair where the key is the
conditional class and the value is a boolean. This is how you do
conditional styling in Templ. It compiles to a string concatenation. No
`clsx()`, no `classnames` library, no runtime.

## Attributes

Templ renders attributes inline:

```templ
templ Button(label string, onclick string) {
	<button hx-post={ onclick } class="btn">
		{ label }
	</button>
}
```

HTMX attributes like `hx-post`, `hx-target`, `hx-swap` are just HTML
attributes. Templ doesn't know about HTMX. It doesn't need to. The
browser receives `hx-post="/api/action"` and HTMX intercepts it on the
client. There's no binding layer, no adapter, no plugin.

## Inline Components

```templ
templ Page() {
	<div>
		@Button("Click me", "/api/click")
	</div>
}
```

Components compose with the `@` syntax. That's it. No `import`, no
`require`, no named exports. If the component is in the same package, it's
visible. If it's in another package, import the Go package and call it
like `components.Navigation(currentPath)`.

## Generated Code

When you run `templ generate`, each `.templ` file produces a
`_templ.go` file. You commit these generated files to Git. Vercel's build
process does not run `templ generate` — it compiles the Go source directly,
and the generated `_templ.go` files are already in the repository.

This means any change to a `.templ` file requires running `templ generate`
and committing the result. If you forget, `go build` will fail with a
compilation error because the generated file is stale. The Makefile runs
this step automatically, so `make build` always regenerates before
compiling.
