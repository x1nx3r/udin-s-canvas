<p align="center">
  <img src="app/assets/public/logo.svg" width="96" height="96" alt="Udin's Canvas" style="border: 2px solid var(--border); padding: 8px;" />
</p>

<h1 align="center">Udin's Canvas</h1>

<p align="center">
  <strong>Live: <a href="https://canvas.x1nx3r.dev" target="_blank">canvas.x1nx3r.dev</a></strong>
</p>

So I built an Excalidraw wrapper because I wanted to draw diagrams without
signing up for a SaaS product that emails me every Tuesday about "premium
canvas textures." You know the ones. "Unlock the felt-tip pen for $9/mo."
I just want to draw a box with an arrow in it. I don't need a subscription
for that.

This is **Udin's Canvas** — a Go + Templ + HTMX + Tailwind app that wraps
Excalidraw, saves drawings to Firestore, and lets you share them with a
link. No latency. No login wall (well, a Google login wall, but you already
have a Google account). No "upgrade to pro to export as PNG."

It compiles to a single binary. The Excalidraw bundle is 8MB of JavaScript
embedded in that binary. I think about this sometimes when I'm trying to
fall asleep — how 8 megabytes of someone else's canvas renderer lives
inside my statically linked Go binary, warm in RAM, waiting for a user to
draw a stick figure.

Anyway.

## Getting Started

```bash
# Prerequisites: Go 1.22+, Node (for the excalidraw bundle step)
go run .
```

Open [http://localhost:3000](http://localhost:3000). You'll see a landing
page. Click "Start Drawing." Sign in with Google. Draw a box. It saves
automatically because I didn't want you to lose your masterpiece.

## Stack

| Layer | Choice | Reason |
|---|---|---|
| Language | Go | Compiles to a binary. No runtime. No `node_modules`. |
| Templates | Templ | Type-safe HTML that doesn't make you want to cry. |
| CSS | Tailwind v4 (standalone) | Utility classes without 400MB of PostCSS plugins. |
| Interactivity | HTMX | Server-rendered HTML fragments. No virtual DOM. |
| Auth | Firebase Auth Web SDK | Google sign-in. ID tokens. Server-verified session cookies. |
| Database | Firestore | Real-time sync. Serverless. I don't have to write migrations. |
| Canvas | Excalidraw (bundled) | 8MB of someone else's hard work. I just render it. |

## How It Works

### Auth Flow

1. User clicks "Sign in with Google."
2. Firebase Auth Web SDK opens a popup, user authenticates.
3. Client sends the ID token to `POST /auth/login`.
4. Server verifies the token with the Firebase Admin SDK and creates an
   http-only session cookie (`14 * 24 * time.Hour` expiry).
5. Middleware verifies the cookie on every request and injects the user's
   `uid` into the request context.

That's it. No JWT parsing on the client. No `localStorage` tokens. No
"refresh token rotation" blog post you'll read at 2AM and immediately
forget.

### Canvas Save/Load

Excalidraw fires an `onChange` callback on every user action. The client
debounces this with a 2-second timer. When the timer fires, it sends the
entire scene (elements + appState) to `POST /api/draw/{id}/save`. The
server writes it to Firestore with `firestore.MergeAll` so we don't
accidentally wipe your drawing's metadata.

Loading is the reverse: `GET /api/draw/{id}/data` reads from Firestore,
sanitizes the `appState.collaborators` field (because Excalidraw crashes
if that's not an array — ask me how I know), and returns the scene.

### Excalidraw Bundle

Excalidraw is bundled at build time via esbuild (`--bundle --format=iife`).
The entry point exports `Excalidraw`, `React`, `ReactDOM`, and
`exportToBlob` as globals. The output is `app/assets/public/excalidraw.bundle.js`
(8MB minified). It lives in the binary via `//go:embed`. The CSS is a
separate file (also embedded) that gets linked in the page head.

### Sharing

Clicking "Share" hits `POST /api/draw/{id}/share`, which generates a random
slug, creates a document in the `shares` collection mapping slug → drawingId,
and stores the slug on the drawing document. The share dialog shows a
read-only URL. Anyone with that URL can view the drawing — no auth required.

I know. Public URLs. It's basically security through obscurity plus one
layer of "nobody knows the slug." If you need proper access control, this
isn't the app for you. But for sending a wireframe to a client without
making them create an account? It works.

### Thumbnails

After every save, the client calls `exportToBlob` (exported from the
Excalidraw bundle) to generate a PNG thumbnail. If the blob is under
100KB, it gets base64-encoded and stored in the `thumbnail` field on the
Firestore document. The dashboard shows these thumbnails in a grid. If
there's no thumbnail, you get a placeholder with the drawing title.

The 100KB limit is arbitrary. I picked it because it felt right. If your
drawing produces a thumbnail larger than 100KB, congratulations, you've
made something detailed enough that a 100KB thumbnail doesn't do it
justice.

### Excalidraw Theming

Excalidraw's default CSS has rounded corners, soft shadows, and a color
scheme that doesn't match anything. I wrote an override stylesheet
(`excalidraw-brutalist.css`) that maps Excalidraw's internal CSS variables
to our design tokens, sets `border-radius: 0` everywhere, replaces soft
shadows with hard offset shadows (`4px 4px 0px`), and applies 2px borders
to everything that stands still long enough.

The theme toggle syncs with Excalidraw's internal state. When you flip
from Latte to Mocha, the canvas follows without needing a page reload.
This took longer than I'd like to admit.

## Project Structure

```
main.go                     # Routes, middleware, static serving
app/
  auth/
    firebase.go             # Firebase Admin SDK init
    middleware.go           # Session cookie verification
    handlers.go             # Login/logout/user endpoints
  canvas/
    page.go + .templ        # Canvas editor page
    shared.go + .templ      # Read-only shared view
    data.go                 # Save/load scene data
    share.go                # Share slug generate + serve
    rename.go               # PUT /api/draw/{id}/rename
    thumbnail.go            # POST /api/draw/{id}/thumbnail
  dashboard/
    page.go + .templ        # Drawing list + new drawing
  profile/
    page.go + .templ        # User profile with drawing grid
  components/
    navigation.templ        # Nav bar with logo, links, theme toggle
    drawing_card.templ      # Card with thumbnail support
    logo.templ              # SVG logo component
    footer.templ            # Footer
  assets/
    public/                 # Static files (logo, icons, excalidraw bundle/css)
  globals.css               # Design tokens, base styles, Tailwind
```

## Why Not Just Use Excalidraw's Built-In Save?

Excalidraw has a "save to disk" button and a "load from disk" button. That
works great for personal use. It doesn't work great for "I want to draw a
diagram on my work computer and open it on my laptop without emailing
myself a JSON file."

The Firebase backend is overkill for a drawing app. I know. But I wanted to
learn how the Firebase Admin SDK works with Go, and this was the excuse.
The app would be better if it used a simple file store or even Postgres.
But it doesn't. It uses Firestore. I made that choice and I'm living with
it.

## The Excalidraw CSS Situation

Excalidraw ships a minified CSS file that's about 145KB on one line. It
gets embedded in the Go binary and served as a static file. There's also
a brutalist override CSS that fixes the aesthetic clash between our sharp
gothic theme and Excalidraw's rounded-corner default.

If Excalidraw updates their CSS classes between versions, the overrides
will break. I've accepted this. When it happens, I'll spend an hour
updating selectors and muttering about CSS specificity. That's a future
problem. Future me is very understanding.

## Deploy

The app is a Go binary. You can deploy it anywhere that runs executables:

**Vercel:** Not natively (it's a long-running server, not serverless), but
can be adapted with a Vercel Go adapter. Or just use a $5 VPS.

**Render / Fly.io:** Works out of the box. Set the start command to the
binary path, set `PORT` env var, done.

**A VPS:** `scp` the binary, run it with systemd, forget about it for 18
months until a Go security patch forces you to rebuild.

You need:
- A Firebase project with Auth (Google sign-in) and Firestore enabled
- A service account key (JSON) in the project root
- The web config in the Firestore console

See the Firebase setup doc if that sounded like a list of chores. It is.
But you only do it once.

## Environment Variables

| Variable | Required | Description |
|---|---|---|
| `PORT` | No (default: 3000) | HTTP listen port |
| `GOOGLE_APPLICATION_CREDENTIALS` | Yes | Path to Firebase service account JSON |

The service account JSON file is auto-detected by scanning for
`-firebase-adminsdk-*.json` in the working directory. If you have exactly
one such file, it gets picked up. If you have zero or more than one, the
app yells at you.
