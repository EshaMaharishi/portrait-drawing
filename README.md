# portrait-drawing

A Viam module (`chess-piece-detection:portrait-drawing`) for drawing a portrait with an arm, plus a
Viam-hosted web app to run it.

## Models

| Model | API | Purpose |
|---|---|---|
| `background-removal` | camera | Source colour frame with everything beyond `max_depth_mm` painted white |
| `image-to-poses` | camera | Turns an image into a dotted pen path; `GetImage` is a WYSIWYG preview, `get_poses` returns the poses |
| `table-surface` | generic service | Calibrates the drawing plane from recorded touch points (`record_point`, `undo`, `clear`, `set_flat`, `status`, `get_plane`) |
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

```sh
make module.tar.gz        # go build + npm ci + vite build -> bin/, dist/, module.tar.gz
viam module upload --upload=module.tar.gz --platform=any --version=<semver>
```

The same tarball carries the module binary and the app (`meta.json` `applications`
entry). Once uploaded the app is served at

    https://portrait-drawing_chess-piece-detection.viamapplications.com

Viam Applications requires the module to be `visibility: public`. The Go binary in the
tarball is therefore publicly downloadable.

For the module binary alone (e.g. `viam module reload`) `make build` is enough.
