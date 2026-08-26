# portrait-drawing

A Viam module (`chess-piece-detection:portrait-drawing`) for drawing a portrait with an arm, plus a
Viam-hosted web app to run it.

## Models

| Model | API | Purpose |
|---|---|---|
| `background-removal` | camera | Source colour frame with everything beyond `max_depth_mm` painted white |
| `image-to-poses` | camera | Turns an image into a dotted pen path at the configured `surface_z_mm`; `GetImage` is a preview, `get_poses` returns the poses |
| `poses-to-3d-scene` | world_state_store | `visualize` / `clear` the poses in the Viam visualizer |
| `poses-to-arm` | generic service | `draw` (async), `stop`, `status` |

`draw` returns immediately with `{"status":"started"}` and runs in the background;
`status` returns `{"state", "completed", "total", "error"}` where `state` is one of
`idle`, `fetching`, `drawing`, `stopped`, `complete`, `error`.

## Web app (`web/`)

Vite + Svelte 5 + [`@viamrobotics/svelte-sdk`](https://github.com/viamrobotics/viam-svelte-sdk).
Shows the camera / background-removed / sketch / dot-preview feeds, exposes
`threshold` and `point_spacing_mm` sliders for `get_poses` and `visualize`, and
starts/stops a drawing with a live progress bar.

Resource names are hard-coded in `web/src/viam.ts` and must match the machine config
(`camera-1`, `person-only`, `sketched`, `image-to-poses`, `poses-to-3d-scene`, `poses-to-arm`).

### Develop against a real machine

```sh
cd web && npm install && npm run dev            # http://localhost:5173
# in another terminal, inject the machine's credentials as cookies:
viam module local-app-testing --app-url=http://localhost:5173 --machine-id=<MACHINE_ID>
# then open http://localhost:8012/start
```

Without `local-app-testing` the app shows a form to enter host + API key manually.

### Build, package, publish

Release with Viam's cloud builder so each platform gets a native binary (do **not** upload a
locally built tarball as `--platform any`; a Mac-built binary won't run on the Linux machine):

```sh
git push origin main
viam module build start --version <semver> --ref <commit>
viam module build list --id <build id>      # wait for linux/* to show Done
```

`make module.tar.gz` (npm ci + vite build -> web/dist/, then go build, which embeds web/dist)
is what the builder runs, per `meta.json`'s `build` section; `make setup` installs Node there.
The same tarball carries the module binary and the app (`meta.json` `applications` entry).
Once uploaded the app is served at

    https://portrait-drawing_chess-piece-detection.viamapplications.com

Viam Applications requires the module to be `visibility: public`. The Go binary in the
tarball is therefore publicly downloadable.

### Local server on the machine

The module also provides a `chess-piece-detection:portrait-drawing:webapp` generic component that
embeds `web/dist` and serves the same app on the machine's LAN at `http://<machine-ip>:8888`
(configurable with a `port` attribute). Add it to the machine config to use the app without
viamapplications.com.
