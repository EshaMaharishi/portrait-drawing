<script lang="ts">
  import { CameraImage, createResourceClient } from '@viamrobotics/svelte-sdk'
  import { CameraClient } from '@viamrobotics/sdk'
  import { RESOURCES, doCommand, errorMessage, type GetPosesResult } from '../viam'

  let {
    partID,
    threshold = $bindable(),
    spacingMM = $bindable(),
  }: { partID: string; threshold: number; spacingMM: number } = $props()

  // The dot preview is recomputed server-side on every request, so it is
  // polled slowly; the others are cheap.
  const feeds = [
    { name: RESOURCES.camera, label: 'Camera', interval: 500 },
    { name: RESOURCES.personOnly, label: 'Background removed', interval: 1000 },
    { name: RESOURCES.sketched, label: 'Sketch', interval: 2000 },
  ]

  const imageToPoses = createResourceClient(CameraClient, () => partID, () => RESOURCES.imageToPoses)

  // The dot preview is fetched by hand (not with <CameraImage>) so the slider
  // values can be passed as `extra` overrides, matching get_poses/visualize.
  // It is recomputed server-side per request, so refresh slowly and debounce
  // slider changes.
  const PREVIEW_INTERVAL_MS = 5000
  const PREVIEW_DEBOUNCE_MS = 400
  let previewSrc = $state<string | undefined>()
  let previewError = $state('')
  let previewBusy = false

  async function refreshPreview() {
    const client = imageToPoses.current
    if (!client || previewBusy) return
    previewBusy = true
    try {
      const { images } = await client.getImages(['points'], { threshold, point_spacing_mm: spacingMM })
      const image = images[0]
      if (image) {
        const url = URL.createObjectURL(new Blob([image.image as Uint8Array<ArrayBuffer>], { type: 'image/png' }))
        if (previewSrc) URL.revokeObjectURL(previewSrc)
        previewSrc = url
        previewError = ''
      }
    } catch (err) {
      previewError = errorMessage(err)
    } finally {
      previewBusy = false
    }
  }

  $effect(() => {
    // Re-run when the client appears or a slider moves.
    void imageToPoses.current
    void threshold
    void spacingMM
    const debounce = setTimeout(refreshPreview, PREVIEW_DEBOUNCE_MS)
    const interval = setInterval(refreshPreview, PREVIEW_INTERVAL_MS)
    return () => {
      clearTimeout(debounce)
      clearInterval(interval)
    }
  })

  let busy = $state(false)
  let result = $state<GetPosesResult | undefined>()
  let error = $state('')

  async function computePoses() {
    if (!imageToPoses.current) return
    busy = true
    error = ''
    try {
      result = await doCommand<GetPosesResult>(imageToPoses.current, {
        command: 'get_poses',
        threshold,
        point_spacing_mm: spacingMM,
      })
    } catch (err) {
      error = errorMessage(err)
    } finally {
      busy = false
    }
  }

  // Poses ≈ 2 per dot (contact + hover) plus the approach/retreat pair.
  const approxDots = $derived(result ? Math.max(0, Math.round((result.count - 2) / 2)) : 0)
</script>

<section class="panel">
  <h2>Capture &amp; preview</h2>
  <div class="grid">
    {#each feeds as feed (feed.name)}
      <figure>
        <CameraImage {partID} name={feed.name} refetchInterval={feed.interval} width="100%" alt={feed.label} />
        <figcaption><span>{feed.label}</span><span class="muted">{feed.name}</span></figcaption>
      </figure>
    {/each}
    <figure>
      {#if previewError}
        <p class="error" style="padding: 0.6rem; margin: 0; background: #fff">{previewError}</p>
      {:else}
        <img src={previewSrc} alt="Dot preview (as drawn)" />
      {/if}
      <figcaption><span>Dot preview (as drawn, at slider values)</span><span class="muted">{RESOURCES.imageToPoses}</span></figcaption>
    </figure>
  </div>

  <div class="row">
    <label class="slider">
      Threshold (darker than this becomes a dot) <span>{threshold}</span>
      <input type="range" min="0" max="255" step="1" bind:value={threshold} />
    </label>
    <label class="slider">
      Point spacing (mm) <span>{spacingMM.toFixed(1)}</span>
      <input type="range" min="0.5" max="5" step="0.1" bind:value={spacingMM} />
    </label>
    <button onclick={computePoses} disabled={busy || !imageToPoses.current}>
      {busy ? 'Computing…' : 'Compute poses'}
    </button>
  </div>

  {#if result}
    <p class="muted">
      {result.count} poses (≈{approxDots} dots) over a {result.size_x_mm}×{result.size_y_mm} mm area
      at {result.point_spacing_mm} mm spacing.
    </p>
  {/if}
  {#if error}<p class="error">{error}</p>{/if}
  <p class="muted">
    The sliders apply everywhere: the dot preview, <em>Compute poses</em>, <em>Visualize in 3D</em>
    and <em>Draw</em>. The drawing area, rotation and hover come from the
    <code>{RESOURCES.imageToPoses}</code> config.
  </p>
</section>
