# portrait-drawing

A Viam module (`chess-piece-detection:portrait-drawing`) for drawing a portrait with an arm, plus a
Viam-hosted web app to run it.

## Models

| Model | API | Purpose |
|---|---|---|
| `background-removal` | camera | Source colour frame with everything beyond `max_depth_mm` painted white |
| `face-crop` | camera | Crops the source image to the largest face (pigo, pure Go) padded by `padding` (0.6); passes the image through when no face is found; `detect` reports the boxes |
| `image-to-poses` | camera | Turns an image into a dotted pen path that fits on the paper; `GetImage` previews it on the paper outline, `get_poses` returns the poses (with `include_preview`, each dot's preview position too), `get_paper` the paper corners |
| `poses-to-3d-scene` | world_state_store | `visualize` / `clear` the poses in the Viam visualizer |
| `poses-to-arm` | generic service | `draw` (async), `show_paper` (hover over the paper corners), `stop`, `status`, `drawing_image` (progress picture: done dots black, pending gray) |

`draw` and `show_paper` return immediately with `{"status":"started"}` and run in the
background; `status` returns `{"state", "completed", "total", "error"}` where `state` is one
of `idle`, `fetching`, `drawing`, `showing_paper`, `stopped`, `complete`, `error`.

### Paper geometry (`image-to-poses`)

The paper lies with its long side along the arm's +x axis, centered on y = 0. The image is
scaled to fit inside the paper minus a margin, preserving aspect ratio, and centered.

| attribute | default | meaning |
|---|---|---|
| `paper_x_mm` | required | x of the paper's near edge |
| `paper_width_mm` | 279.4 | extent along x (11in) |
| `paper_height_mm` | 215.9 | extent along y (8.5in) |
| `margin_mm` | 25.4 | border kept clear on all sides (1in) |
| `image_up` | `"+x"` | which way the top of the image points: `"+x"` away from the arm, `"-x"` toward it |
| `mirror` | `true` | flip the image left-to-right so a portrait reads like a reflection |
| `fit_to_content` | `true` | crop to the dark pixels (plus 3% padding) before scaling, so the subject fills the drawing area |
| `surface_z_mm` | required | z of the paper surface |
| `point_spacing_mm` | required | grid cell size ≈ pen tip width |
| `threshold` | 128 | grayscale cutoff for a dot (0-255) |
| `hover_above_mm` | required | pen lift between dots (0 disables) |
| `max_hover_travel_mm` | 0 | XY jump beyond which the arm crosses flat at hover height |
| `dense_block_size` | 0 | collapse fully dark n×n blocks to one dot |

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
