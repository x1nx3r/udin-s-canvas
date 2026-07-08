# Out-of-Band Swaps

Normal HTMX works like this: a user clicks a button, HTMX sends a request
to the server, the server returns HTML, and HTMX swaps that HTML into the
DOM where the `hx-target` says to put it. One request, one target.

Out-of-band (OOB) swaps let you return HTML for multiple targets in a
single response. The main response goes to the primary target. Additional
elements marked with `hx-swap-oob="true"` are automatically swapped into
their matching DOM elements elsewhere on the page. One request, many
targets.

## When to Use This

- Updating a cart count badge when the user adds an item
- Refreshing a notification indicator alongside the main content
- Updating a sidebar and the main content area in one round trip
- Showing a flash message while the main content changes

## How It Works

The server returns the primary content as usual, but appends extra
elements with `hx-swap-oob="true"`:

```html
<!-- Primary target content -->
<div id="cart-contents">
    <div class="cart-item">Widget — $12.99</div>
    <div class="cart-total">Total: $12.99</div>
</div>

<!-- OOB update: cart badge in the header -->
<span id="cart-badge" hx-swap-oob="true">1</span>

<!-- OOB update: flash message -->
<div id="flash-message" hx-swap-oob="true">
    Added to cart
</div>
```

HTMX parses the entire response, finds elements with `hx-swap-oob="true"`,
and swaps them into the DOM by `id` matching. The swap can be
`innerHTML` (default), `outerHTML`, `beforebegin`, `afterbegin`,
`beforeend`, or `afterend` — same as regular `hx-swap`.

## Server-Side Implementation

In Go, you render the primary content and append OOB elements. Since
everything is server-rendered, you have full control over what appears in
the response:

```go
func CartHandler(w http.ResponseWriter, r *http.Request) {
    cart := getCart(r)
    count := len(cart.Items)

    // Render primary content
    w.Write([]byte(`<div id="cart-contents">`))
    for _, item := range cart.Items {
        fmt.Fprintf(w, `<div class="cart-item">%s — $%.2f</div>`,
            item.Name, item.Price)
    }
    fmt.Fprintf(w, `<div class="cart-total">Total: $%.2f</div>`, cart.Total)
    w.Write([]byte(`</div>`))

    // Render OOB badge
    fmt.Fprintf(w,
        `<span id="cart-badge" hx-swap-oob="true">%d</span>`, count)

    // Render OOB flash
    if cart.JustAdded {
        fmt.Fprintf(w,
            `<div id="flash-message" hx-swap-oob="true">Added to cart</div>`)
    }
}
```

In practice you'd use Templ components rather than string concatenation:

```templ
templ CartResponse(cart Cart) {
	<div id="cart-contents">
		for _, item := range cart.Items {
			<div class="cart-item">
				{ item.Name } — { fmt.Sprintf("$%.2f", item.Price) }
			</div>
		}
		<div class="cart-total">Total: { fmt.Sprintf("$%.2f", cart.Total) }</div>
	</div>
	if cart.Count > 0 {
		<span id="cart-badge" hx-swap-oob="true">
			{ cart.Count }
		</span>
	}
}
```

## Multiple OOB Elements

You can include as many OOB elements as you want. Each must have a unique
`id` matching an existing element on the page. If an OOB element's `id`
doesn't exist in the DOM, HTMX logs a warning and skips it. No crash, no
error — just a console message that's easy to miss during development.

## Swapping Strategies

```html
<!-- innerHTML (default) — replaces content inside the target -->
<span id="badge" hx-swap-oob="true">5</span>

<!-- outerHTML — replaces the target element itself -->
<div id="toast" hx-swap-oob="outerHTML">
    <div class="toast toast--success">Saved</div>
</div>

<!-- beforeend — appends inside the target -->
<li id="log" hx-swap-oob="beforeend">New log entry</li>
```

`outerHTML` is useful when you want to change the element's tag or
attributes, not just its content. `beforeend` is useful for appending to
lists. `delete` removes the target element entirely:

```html
<div id="spinner" hx-swap-oob="delete"></div>
```

This removes the spinner from the DOM after the request completes, no
additional JavaScript needed.

## OOB vs SSE

OOB swaps are for one-shot updates tied to a specific user action. If you
need real-time updates pushed from the server (chat messages, live
notifications, stock tickers), use HTMX's SSE or WebSocket extensions
instead. OOB swaps require a trigger action — a button click, a form
submission, a polling interval. They don't happen on their own.
