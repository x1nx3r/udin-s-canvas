<p align="center">
  <img src="app/assets/public/goth_lady_logo.jpg" width="120" height="120" alt="GOTTH Logo" style="border: 2px solid #1a1a24;" />
</p>

<h1 align="center">GOTTH Stack Boilerplate</h1>

<p align="center">
  <strong>Live Sanctuary: <a href="https://gotth.x1nx3r.dev" target="_blank">gotth.x1nx3r.dev</a></strong>
</p>

So I made this Go thing that looks like Next.js but doesn't make me want to
throw my laptop into a river. You know Next.js? Yeah. 500 megabytes of
node_modules so you can render a `<div>` that says "Hello World". Somewhere
in there a JavaScript file is serializing a JSON object into another JSON
object just so the card on your landing page can fade in. I don't know who
needs to hear this but you don't need 1,427 transitive dependencies to build
a blog.

Anyway.

This is **GOTTH** — Go, Templ, Tailwind, HTMX. It's Next.js for people who
woke up one day and realized they've spent the last 6 years debugging
Webpack configs instead of building actual software. It compiles to a single
binary. No runtime. No `node_modules`. No "sorry, we changed the API between
versions 14.2.3 and 14.2.4" emails in your inbox at 3AM.

The logo is a goth lady because dark mode is the default and because
JavaScript frameworks make me want to listen to Bauhaus and wear black
turtlenecks unironically.

## Getting Started

```bash
npm run dev
```

Yeah, `npm run dev`. I know. The PTSD is real. But relax — `package.json`
here is just a security blanket. It proxies to a `Makefile` so your muscle
memory doesn't short-circuit. No packages get installed. No Chrome headless
binary downloads itself just so you can see "Hello World" hot-reload. You're
safe.

Open [http://localhost:3000](http://localhost:3000). You'll see a web page.
It loads in under a second because there isn't a React tree reconciling
itself in the background like some kind of digital anxiety attack.

## Why Does This Exist

Because I got tired. Let me walk you through the specific flavor of tired.

### In-Place Compilation

Templ generates Go files from `.templ` templates. If you have a file watcher
watching `.go` files, the generated files trigger the watcher, which
generates more files, which triggers the watcher, and suddenly your CPU
sounds like a jet engine and you're staring at a loop that will outlive your
grandchildren. The JavaScript ecosystem solved this by adding three more
abstraction layers. We solved it by adding one line to `.air.toml`:

```
exclude_regex = [".*_templ\\.go$"]
```

I'm not saying the JS ecosystem over-engineers things. I'm saying they would
install a C++ compiler toolchain to fix a typo in a config file.

### Embedding Assets

You know what's fun? Deploying to Vercel and discovering that your server
can't find its own CSS because the container filesystem is laid out
differently at runtime. You know what's even more fun? This has happened to
everyone and the industry response was "have you tried Serverless
Functions v2 with Edge Middleware and the new Streaming SSR mode?"

Anyway: `//go:embed`. Everything is in the binary now. CSS, images,
documentation, my therapist's phone number. It lives in RAM. It works on
serverless. It's 3 lines of code. I think about this sometimes when I'm
trying to fall asleep.

### What About Dynamic Data? You Know, Like a Real App

So you're reading this and you're thinking "cool, your binary has a cat
picture in it, but my app needs to let users create, read, update, and
delete things. Should I embed a PostgreSQL database into the binary?"

No. Absolutely not. Don't do that. I love that your brain went there but
no.

Static assets are embedded. User uploads, database rows, session tokens,
that one spreadsheet your PM calls "the source of truth" — those go
somewhere else. Get yourself an S3 bucket. Or a DigitalOcean Space. Or a
MinIO instance in a docker compose file that you swear you'll secure one
day. Write files to disk if you're running on a VPS and you trust your
backup strategy (you shouldn't, but I respect the confidence).

"But wait," you say, "what if I use an ephemeral in-memory filesystem?
Like, `//go:embed` but for user uploads? Something that lives in RAM
during the request and then—" No. Stop. You're describing `/tmp` with
extra steps. Serverless containers don't have persistent storage. Even if
you hack together some `memfs` abomination that stores blobs in a
`sync.Map`, that data evaporates the moment your instance cold-starts.
Which it will. At 3AM. On a Saturday. When you're at a wedding.

You *technically* can introduce a phantom in-memory filesystem. It's not
hard. `map[string][]byte` with a `sync.RWMutex`. Congratulations, you've
built the world's least durable object store. It'll work beautifully
during development. You'll deploy it, test it, smile. Then the first real
user uploads a profile picture, the container recycles, and suddenly Gary
from accounting has no avatar. Gary emails you. Gary is cc'd on the
thread with your VP. You will rewrite it to use S3 at 2AM on a Tuesday
while listening to "The Downward Spiral" on repeat.

Just use S3. Or R2. Or whatever object storage your cloud overlord
offers. The embedded assets are for things that ship *with* the binary —
icons, logos, documentation, that one GIF of a cat falling off a chair
that's been in every project since 2007. Your users' data lives somewhere
else. That's not a limitation. That's called "architecture."

Next.js would have you deploy a full Node runtime that boots a headless
Chromium to render a PDF on the server, serializes the result as JSON,
ships it over the network, and deserializes it on the client — all so you
can display a table with three rows. The V8 garbage collector runs, a
worker thread spins up, a memory leak you'll never find grows by 12MB, and
somewhere in San Francisco a React core team member writes a blog post
about how this is actually the future of web development.

There. That's how you're supposed to build a "serverless" app. Not a
headless Chromium.

### What If I Need a Database?

Cool. Same answer, different service.

You reach for `database/sql`, `pgx`, `sqlx`, or whatever driver speaks
your database's wire protocol. You write a connection string in an
environment variable. You open a connection pool in `main.go` and pass it
to your handlers. That's it. No ORM. No migration framework. No "prisma
generate" step that produces 4,000 lines of TypeScript types you didn't
ask for. No `schema.prisma` file that breaks because you renamed a column
and the generated client doesn't match the database anymore and now your
CI is red and someone from the Prisma Discord is telling you to run
`prisma db push --force-reset` in production.

I know what you're thinking. "But an ORM handles migrations, type safety,
relation loading, connection management—" Yes. I know what they sell. I've
seen the conference talks. Let me address each one.

**Migrations.** You can write SQL in a `.sql` file. You can run it with
`psql`. You can embed those migration files in the binary with `//go:embed`
and write a `main()` function that applies them on startup. It's 40 lines
of Go. You will never wonder why the TypeScript types don't match the
database schema because the database schema *is* the source of truth, not
a generated artifact.

**Type safety.** `sqlx` maps query rows to structs. `pgx` does it natively.
You write a `SELECT` query, you get back a struct. If the column doesn't
exist, the query fails at the database level, not after three layers of
generated code silently passed `undefined` through a pipeline. Go's type
system catches the mismatch at compile time. The database's query planner
catches it at planning time. You don't need an ORM to tell you a column
is missing when the database will tell you itself, in plain English, with
a row number.

**Relation loading.** You know what handles relations? `JOIN`. It's been
in SQL since 1974. You write `SELECT u.*, p.title FROM users u JOIN posts
p ON p.user_id = u.id` and you get the user with their posts. No N+1. No
`.include({ posts: true })` that generates five separate queries because
the ORM decided to be helpful. No "eager loading" vs "lazy loading" vs
"preload vs includes vs references" taxonomy that requires a decision
tree. It's a JOIN. It works. It's fast. You already know how to write it.

**Connection pooling.** `pgx` has a built-in pool. `database/sql` has one
too. You configure max connections, idle timeout, health check interval.
That's it. No proxy. No sidecar container. No connection pooling service
with its own pricing tier. The pool is a struct. You create it, you pass
it around, you close it on shutdown. Twelve lines.

"But Prisma has a studio!" Prisma Studio is a GUI for your database that
requires running a dev server. You know what else is a GUI for your
database? `pgAdmin`, `DBeaver`, `TablePlus`, `DataGrip`, `psql` with
`\x auto`. None of them require a `prisma generate` step. None of them
fail to start because your schema file has a trailing comma.

Here's the thing nobody says out loud: ORMs exist because JavaScript
doesn't have `database/sql`. The standard library doesn't ship with a SQL
interface. So the community built one on top of a type system that wasn't
designed for it, and they called it an ORM, and they sold it as a feature
instead of a workaround.

Go has `database/sql`. It's in the standard library. It's been there since
Go 1.0. You write `rows.Scan(&user.ID, &user.Name)` and you get type-safe,
compile-checked, null-aware deserialization without a single line of
generated code. The database is just another dependency. Import the
driver, write the query, move on with your life.

"But I need serverless," you say. "Firestore. MongoDB Atlas. DynamoDB.
Something that scales to zero so I don't get a surprise bill when my
Hacker News post hits the front page."

Fine. You can use those too. Go has drivers for all of them. The
`firebase` Go SDK exists. The MongoDB Go driver exists. The AWS SDK for
Go exists. You write the same code — open a connection, run a query, map
the result to a struct. The difference is you're paying per read instead
of paying for a connection slot that's doing nothing 99% of the time.
That's a business decision, not a technical one, and I respect it.

But let's be honest about what you're actually choosing. You're not
choosing Firestore because it's "serverless." You're choosing it because
you don't want to write migrations. MongoDB because you don't want to
define a schema. DynamoDB because you don't want to think about access
patterns until the bill arrives. These are document stores that call
themselves databases because "eventually consistent JSON bucket" doesn't
fit on a sticker.

And that's fine. If your data model is "I put things in boxes and take
them out again," a document store works great. But if you ever need to
join two collections, compute a sum, or run a transaction where three
things must succeed or none of them do, you'll find yourself writing
application-level consistency checks that PostgreSQL gave you for free
with a `BEGIN` and a `COMMIT`. The database you chose to avoid SQL will
spend the next six months teaching you why SQL exists.

You know what's actually serverless? PostgreSQL on Neon, or PlanetScale,
or Supabase, or Fly.io with Litestream. Real databases that speak SQL,
scale to zero when idle, and wake up in under 300ms when a request comes
in. You connect with a connection string. You write queries. You get
actual transactions, actual joins, actual foreign keys. The "serverless
database" problem was solved years ago. The industry just didn't update
the blog posts.

So pick your poison. `pgx` to Neon. MongoDB driver to Atlas. Firestore
SDK to Firestore. They all work. But if you pick a document store because
you're afraid of SQL, you're going to spend a year reimplementing SQL
badly in your application layer, and then you're going to migrate to
PostgreSQL anyway. I've seen this movie. It has a sequel called "why is
our migration taking 48 hours" and a trilogy capper called "we should have
just used Postgres from the start."

So yes. You need a database. You need a connection string in an
environment variable, a Go driver, and a willingness to write whatever
query language your database speaks. The database doesn't care what
language your web framework is written in. It speaks TCP. Go speaks TCP.
They'll figure it out.

### Tailwind Without Node

Do you remember that Tailwind is supposed to be a standalone thing? I
mean actually remember. Way back when Adam Wathan first showed it, it was
a CSS framework you dropped into your project. A config file. Some utility
classes. That was it.

Then the JavaScript ecosystem got its hands on it.

Now Tailwind comes wrapped in a stack of Node packages so deep you need a
machete to get through it. `tailwindcss`, `postcss`, `autoprefixer`,
`postcss-import`, `postcss-nesting`, `@tailwindcss/typography`,
`@tailwindcss/forms`, `@tailwindcss/aspect-ratio`,
`tailwindcss-animate`. Somewhere in there is a package called
`tailwindcss-animated` that has 17 dependencies of its own. You're running
a CSS preprocessor pipeline with more transitive dependencies than the
Linux kernel. For what? So you can add `rounded-lg` to a button without
typing four lines of CSS?

Tailwind v4 finally ships a standalone binary. A single file you can
download. Put it in your `$PATH`. Run it. It watches your HTML files and
spits out CSS. No `npm install`. No `postcss.config.js`. No `node-gyp`
failing because your build server is running Ubuntu 18.04 and the glibc
version is wrong. No "unable to resolve dependency tree" errors at 11PM on
a Sunday.

You know why Tailwind ships a standalone binary in the first place?
Because it doesn't need Node. It never did. Tailwind scans your files for
class names, matches them against its utility catalog, and generates a
CSS file. That's a text processing task. It's basically a glorified `grep`
with a color palette. The JavaScript ecosystem bolted on a PostCSS plugin
architecture because that's what you do — you take a simple thing, wrap it
in 14 abstraction layers, call it a "build pipeline," and charge for the
consulting.

So here's what we do: we ship the standalone binary in `bin/tailwindcss`.
If it's there, the Makefile uses it. If it's not (say, Vercel's builder
couldn't download it — their serverless infrastructure is a miracle of
engineering until you need to `curl` a file), it falls back to `npx
@tailwindcss/cli`. The Node path is the emergency exit, not the front
door.

But really. Just use the binary. It's a single file. It does the same
thing. It doesn't install 400MB of `postcss-selector-parser` and
`cssnano` and whatever the hell `icss-utils` is. You already have a
computer. It already works. You don't need a package manager to generate
CSS.

### Routing

Go 1.22's standard library now supports method-based routing and path
parameters. You don't need `chi`, `gorilla/mux`, `echo`, or whatever
framework is popular this week. You just write `mux.HandleFunc("GET
/{file}", handler)` and it works. No dependency. No "we're deprecating v3,
please migrate to v4" when you're in production with a flight to catch.

### HTMX View Transitions

I wanted page transitions without installing `framer-motion`, which would
have added 87KB and a spiritual dependency on Framer's pricing page.

So I used `view-transition-name` in CSS and `document.startViewTransition()`
in a `<script>` tag. That's it. The browser handles the animation on the
GPU. I didn't need a JavaScript framework to tell the GPU to do its job.

Wait. You said "we don't use JavaScript here." I know what I said. Look,
I'm not running a React SPA. I'm not shipping a virtual DOM. I'm not
parsing JSX, running a bundler, polyfilling half the ES2026 spec, or
loading a runtime that takes longer to parse than my entire Go binary
takes to start. I wrote a handful of lines of JavaScript — one to call
`document.startViewTransition()`, a couple more because I wanted dark mode
to also cross-fade. That's it. That's the whole JS surface area. You can
read it in the time it takes your Next.js app to execute its first
`useEffect()`.

The difference between this and a "JavaScript framework" is the difference
between owning a screwdriver and buying a hardware store. Yes, there's a
screwdriver in the drawer. No, I did not need to remortgage my house to
acquire it.

HTMX handles the link interception with `hx-boost="true"` and the swap
with `hx-swap="outerHTML transition:true"`. Then CSS `view-transition-name`
on a few containers tells the browser "hey, animate this." The renderer
runs on the GPU, not on the main thread. It's hardware-accelerated,
raster-based, and costs exactly zero framework overhead. Chrome, Firefox,
Safari — they all support it. They've supported it for years. The web
platform is not missing features. It's missing a generation of developers
who were told they need 70MB of npm dependencies to fade a div in.

So yes. There are technically bytes of JavaScript in this project. Eleven
lines of it. They live in a `<script>` tag at the bottom of `layout.templ`.
You can read them in the time it takes your IDE to index `node_modules`.

## What Makes This Better than The Abomination That is Next.js

Everything. The answer is everything. But let me be specific.

### It Compiles to a Binary

Next.js deploys as a Node.js runtime. Your "build output" is a folder full
of JavaScript files, JSON manifests, and pre-rendered HTML shells. You
ship V8, libuv, and a thousand `require()` calls to a serverless container
that has 512MB of RAM. Your 5KB of application logic sits inside a 50MB
runtime like a single pea in an industrial soup kettle.

GOTTH compiles to a static binary. One file. The operating system loads it
into memory, the CPU starts executing `main()`, and you're handling
requests. There is no interpreter. There is no JIT warm-up. There is no
garbage collector deciding now is a good time to pause the world and
collect your short-lived allocation of a `req.Body`. Go's GC is
sub-millisecond. Node's GC is a full stop.

### It Starts in Milliseconds

A Next.js serverless function on Vercel has to boot the Node runtime,
initialize the module system, resolve your `next.config.js`, load the
routing table, hydrate the React server components, and *then* it can
start thinking about your request. If you're using Edge Runtime, congrats,
you've added a V8 isolate bootstrap on top of that.

A Go binary starts in under 10ms. The kernel loads the ELF, maps the
segments, jumps to `_start`, and you're in `main()`. That's it. No event
loop initialization. No module resolution. No "building the dependency
graph." The operating system was designed to run binaries. It was not
designed to run JavaScript.

### No `node_modules`

I want you to think about this: Next.js has a `node_modules` folder. That
folder contains more files than the Linux kernel source tree. It contains
more files than some *operating systems*. A typical Next.js project pulls
in around 200,000 files. Two hundred thousand. For a web framework.

GOTTH has zero runtime dependencies. The `go.mod` file lists the templ
compiler and that's it. The binary contains everything it needs. If you
`docker scout` the image, the only CVE is your own code.

### No Client-Server Mismatch

React Server Components forced an entire generation of developers to learn
about serialization boundaries, client directives, server directives, and
the precise moment when a component crosses the wire from "runs on the
server" to "runs in the browser." If you get it wrong, you get hydration
errors. The browser console lights up like a Christmas tree because the
server rendered `<div>3</div>` and the client hydrated `<div>3</div>` but
the timestamp on the footer differed by one millisecond.

GOTTH renders HTML on the server. HTMX enhances it on the client. There's
no hydration because there's nothing to hydrate. HTML arrives at the
browser as HTML. The browser parses it. The end. You cannot have a
hydration mismatch when there is no hydration. It's a solved problem that
JavaScript re-introduced and then sold you a subscription to fix.

### No Build Pipeline

Next.js has a build pipeline. Not a small one. A pipeline that runs
PostCSS, Babel or SWC, Webpack or Turbopack, compresses images, generates
static pages, pre-renders partials, and outputs a JSON manifest that
describes the shape of every route so the runtime knows what to load next.
If you change one line in a CSS file, it invalidates half the cache and
rebuilds the universe.

GOTTH runs `templ generate`, `tailwindcss -i`, and `go build`. Three
commands. The Makefile runs them in order. It takes about a second. You
can run it on a Raspberry Pi. You can run it in a CI pipeline that has
1GB of RAM. The output is a single binary and a CSS file. That's it. No
manifest. No chunk hashes. No "Rebuilding... (x89)" in your terminal while
you question your life choices.

### It Costs Less to Run

Next.js on Vercel bills by compute time. Serverless functions that take
200ms to cold-start and 50ms to respond means you pay for the 200ms of
boot time on every cold request. Scale that to a thousand requests and
you've paid for several minutes of Node runtime doing absolutely nothing
except `require()`-ing its own weight.

GOTTH on Vercel cold-starts in the time it takes Node to decide which
`process.env` variable to load first. The same binary on a $5 VPS handles
thousands of requests without breaking a sweat. You could run it on
hardware you found in a dumpster behind an electronics store. Go binaries
don't care. They just run.

### The Framework Doesn't Own You

Next.js dictates your file structure, your routing, your data fetching
patterns, your build configuration, and your moral philosophy about
server components. You put a file in `app/` and suddenly it's a route. You
name it `page.tsx` and it's a page. You name it `loading.tsx` and it's a
loading state. You want to fetch data? Use `fetch()` because Next.js
patched the global `fetch` to do caching. You want to break out? Good
luck. The framework is the application.

GOTTH uses Go's `http.ServeMux`. You register handlers. They're functions.
They take a `http.ResponseWriter` and a `*http.Request`. They return
nothing. They write HTML to the wire. That's it. There is no framework
between you and the HTTP spec. You can use any Go library, any database
driver, any file structure. The computer works the way the computer works.
Nobody decided for you.

---

### In A Perfect World, Next is An Abomination, But...

Look. I'm not going to stand here and pretend this thing replaces Next.js
for every use case. It doesn't. Let me tell you what you lose when you
walk away from the abomination.

**Server-side React rendering.** This is the big one. If your application
is a dashboard with 50 interactive widgets that all need to share state
without a full page reload, HTMX will get you about 80% of the way there.
The last 20% you'll spend writing custom `hx-trigger` configurations and
wondering why your life turned out this way. React's component model,
for all its complexity, gives you a unified state tree. HTMX gives you
server-rendered HTML fragments and the hope that your backend can keep up.
For most apps, 80% is enough. For Figma? Absolutely not.

You can code this yourself. Probably have fun doing it too. But people
will still read docs for a framework they don't understand rather than
write code they understood from day one. Your call.

**Incremental Static Regeneration.** Next.js can pre-render a million
pages at build time, then revalidate individual pages on demand when the
data changes. It's genuinely useful for content-heavy sites. GOTTH can
pre-render pages at build time if you write a script that generates them
and embed them into the binary. But real-time revalidation per route?
You're writing that yourself. It's not hard — a background goroutine, a
channel, a `sync.Map` — but it's not built-in. Next.js hands you this on
a silver platter covered in the tears of Vercel's venture capital
investors.

You could build this in an afternoon. A `map[string]time.Time`, a
background goroutine with a ticker, a mutex. That's the whole thing. But
the industry decided reading migration guides is a better use of time
than writing a for loop. Your call.

**Image Optimization.** Next.js has an `<Image>` component that
automatically resizes, compresses, serves WebP/AVIF, and lazy-loads
images with a blur placeholder. It's genuinely good. I hate how good it
is. GOTTH serves images as static files. They're optimized at build time
or they're not optimized. There's no on-the-fly pipeline that detects the
viewport width and serves the exact resolution your user's Retina display
needs. You can hook up `imgproxy` or `thumbor` or a CDN with
transformations (Cloudflare Images, Imgix, etc.), but it's not zero-config
out of the box.

An image processing pipeline — decode, resize, encode, cache — is like
200 lines of Go. Maybe 300 if you want AVIF. But somehow we've normalized
that this is "hard" and requires a managed service with a monthly bill.
Your call.

**API Routes with Middleware.** Next.js lets you co-locate API routes
alongside your pages, with middleware that runs before every request. You
can add authentication, rate limiting, request logging, and header
modification in a single `middleware.ts` file. GOTTH uses Go's standard
`net/http` middleware pattern — wrap handlers with functions that take a
`http.Handler` and return a `http.Handler`. It's more verbose, but it's
also more explicit. You write `mux.Handle("GET /api/secret",
authMiddleware(handler))` and you know exactly what's happening. No magic.
No "the middleware file name must be `middleware.ts` and it must export a
`middleware` function and it runs at the edge but only if you configure
the matcher correctly."

Middleware is a function that wraps a function. You learned this in week
two of your first programming language. But sure, let's read the Next.js
middleware docs for the third time because you're not sure if the file
needs to be `middleware.ts` or `middleware.js` or if it lives in `src/`
or the root or if the `config.matcher` regex is correct. Your call.

**The Ecosystem.** This is the honest one. Next.js has 2,300 plugins,
15,000 blog posts, a dedicated conference, and a documentation site that
cost more to build than most startups raise in seed funding. If you have
a problem with Next.js, the answer is three Google searches away. If you
have a problem with GOTTH, the answer is somewhere in a GitHub issue from
2019 that was closed without resolution because the maintainer was busy
with their day job. You are trading convenience for sanity. That's a fair
trade for some people. For others, it's a non-starter.

You can't code an ecosystem. That one's actually fair. You can't grep
your way into 15,000 blog posts. But you can code your way out of needing
one. The standard library is the ecosystem. Your language's docs are the
blog posts. It's a mindset shift, not a technical limitation. Your call.

**File-Based Routing.** I'll admit it: putting a file in a folder and
having it become a route is satisfying. It's clean. It's discoverable.
GOTTH requires you to open `main.go` and type
`mux.HandleFunc("GET /blog/{slug}", handler)`. It's one extra step. But
it's one extra step *every time*. If you have 50 routes, you have 50 lines
of route registration. You can automate it with a loop and a map. But
that's code you write instead of code you don't.

A route registry in Go is a map literal. Twenty lines. You can even write
a code generator that reads your file tree and writes the handler
registrations for you. But dragging a file into a folder does feel easier,
I'll give them that. Your call.

**Streaming SSR and Suspense.** Next.js can stream HTML to the browser as
it renders, showing a loading fallback for slow components while the rest
of the page renders. It's genuinely impressive. GOTTH renders the entire
page on the server and sends it as one response. You can simulate streaming
with HTMX — render the shell, then lazy-load fragments with `hx-trigger="load"`
— but it's not the same thing. The page feels complete once the initial
HTML arrives, but heavy sections will pop in later. This is fine for most
apps. For a real-time financial dashboard where every millisecond matters,
you'll feel the difference.

Streaming is genuinely useful. You can approximate it with HTMX in an
afternoon — render the shell, lazy-load the fragments, call it a day. But
if your use case demands sub-100ms Time To First Byte for every HTML
fragment, Next.js genuinely wins here. That said, 90% of apps don't have
that use case. They have a database query that takes 200ms and they're
blaming the framework instead of the query. Your call.

90% of those, you can write them yourself, with some effort. But still, the
point is 90% of the web didn't need all those features.

---

So that's it. That's why this exists. Not because Go is fashionable.
Because the JavaScript ecosystem took a perfectly good document delivery
platform and turned it into a desktop application runtime that happens to
render HTML as a side effect. This project is a reset button. A reminder
that you can serve web pages with a language that compiles to machine code
and a template engine that doesn't require a PhD in type-level
metaprogramming.

It's Next.js for people who are tired. And I mean *tired*.

## Deploy on Vercel

Connect the repo. Vercel sees `main.go` and goes "oh, Go, okay" and builds
it. That's the whole process. The `vercel.json` is just:

```json
{ "src": "/(.*)", "dest": "/main.go" }
```

That's it. No `next.config.js`. No `eslintrc`. No PostCSS plugins. No CI/CD
pipeline that costs more than your rent.

### The Cold Start Thing

Serverless functions scale to zero. First request after silence has to boot
a container. Go boots in ~5ms. Vercel's infra takes longer than that. Your
users will not notice. I promise you they've waited longer for a single-page
app to hydrate.

### The `go mod tidy` Problem

`templ` generates files outside the root. Running `go mod tidy` will delete
the `templ` dependency because the compiler thinks it's unused. We import it
anonymously with `_ "github.com/a-h/templ"` to keep it alive. This is a hack
but it's a one-line hack and I refuse to feel bad about it.

## Learn More

- [GOTTH docs](/docs) — read them, they exist, they're fine
- [HTMX docs](https://htmx.org/docs/) — how to build interactive UIs without
  loading a React runtime the size of a Tolstoy novel
- [Templ Guide](https://templ.guide/) — type-safe HTML that doesn't make you
  want to cry
