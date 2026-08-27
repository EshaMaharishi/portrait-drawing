<script lang="ts">
  import { CameraImage, createResourceClient } from '@viamrobotics/svelte-sdk'
  import { CameraClient, SwitchClient } from '@viamrobotics/sdk'
  import { RESOURCES, errorMessage } from '../viam'

  let {
    partID,
    threshold = $bindable(),
    spacingMM = $bindable(),
    locked = false,
  }: { partID: string; threshold: number; spacingMM: number; locked?: boolean } = $props()

  // These feeds don't depend on the sliders; <CameraImage> polls them.
  const feeds = [
    { name: RESOURCES.camera, label: 'Camera', interval: 500 },
    { name: RESOURCES.personOnly, label: 'Background removed', interval: 1000 },
    { name: RESOURCES.sketched, label: 'Sketch', interval: 2000 },
  ]

  const imageToPoses = createResourceClient(CameraClient, () => partID, () => RESOURCES.imageToPoses)
  const positions = createResourceClient(SwitchClient, () => partID, () => RESOURCES.positions)

  // The picture-taking pose is the last preset on the position switch.
  let movingArm = $state(false)
  let moveError = $state('')
  async function goToPicturePosition() {
    const client = positions.current
    if (!client) return
    movingArm = true
    moveError = ''
    try {
      const [count] = await client.getNumberOfPositions()
      if (count < 1) throw new Error(`${RESOURCES.positions} has no positions configured`)
      await client.setPosition(count - 1)
    } catch (err) {
      moveError = errorMessage(err)
    } finally {
      movingArm = false
    }
  }

  // The dot preview is fetched by hand so the slider values can be passed as
  // `extra` overrides — the same values draw/visualize use. It is recomputed
  // server-side per request, so: debounce slider moves, abort a request that
  // a newer slider value has made stale, and otherwise refresh slowly.
  const PREVIEW_INTERVAL_MS = 5000
  const PREVIEW_DEBOUNCE_MS = 150
  let previewSrc = $state<string | undefined>()
  let previewError = $state('')
  let previewUpdating = $state(false)
  let inflight: AbortController | undefined

  async function refreshPreview() {
    const client = imageToPoses.current
    if (!client) return
    inflight?.abort()
    const controller = new AbortController()
    inflight = controller
    previewUpdating = true
    try {
      const { images } = await client.getImages(
        ['points'],
        { threshold, point_spacing_mm: spacingMM },
        { signal: controller.signal },
      )
      if (controller.signal.aborted) return
      const image = images[0]
      if (image) {
        const url = URL.createObjectURL(new Blob([image.image as Uint8Array<ArrayBuffer>], { type: 'image/png' }))
        if (previewSrc) URL.revokeObjectURL(previewSrc)
        previewSrc = url
        previewError = ''
      }
    } catch (err) {
      if (!controller.signal.aborted) previewError = errorMessage(err)
    } finally {
      if (inflight === controller) {
        inflight = undefined
        previewUpdating = false
      }
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
</script>

<section class="panel">
  <h2>Capture &amp; preview</h2>
  <div class="row" style="margin: 0 0 0.75rem">
    <button class="secondary" onclick={goToPicturePosition} disabled={movingArm || locked || !positions.current}>
      {movingArm ? 'Moving arm…' : 'Go to picture-taking position'}
    </button>
    {#if moveError}<span class="error">{moveError}</span>{/if}
  </div>
  <div class="grid">
    {#each feeds as feed (feed.name)}
      <figure>
        <CameraImage {partID} name={feed.name} refetchInterval={feed.interval} width="100%" alt={feed.label} />
        <figcaption><span>{feed.label}</span><span class="muted">{feed.name}</span></figcaption>
      </figure>
    {/each}
    <figure class:updating={previewUpdating}>
      {#if previewError}
        <p class="error" style="padding: 0.6rem; margin: 0; background: #fff">{previewError}</p>
      {:else}
        <img src={previewSrc} alt="Dot preview (as drawn)" />
      {/if}
      <figcaption>
        <span>Dot preview</span>
        <span class="muted">{previewUpdating ? 'updating…' : RESOURCES.imageToPoses}</span>
      </figcaption>
    </figure>
  </div>

  <div class="row">
    <label class="slider">
      Threshold (darker than this becomes a dot) <span>{threshold}</span>
      <input type="range" min="0" max="255" step="1" bind:value={threshold} disabled={locked} />
    </label>
    <label class="slider">
      Point spacing (mm) <span>{spacingMM.toFixed(1)}</span>
      <input type="range" min="0.5" max="5" step="0.1" bind:value={spacingMM} disabled={locked} />
    </label>
    {#if locked}<span class="muted">Sliders are locked while drawing.</span>{/if}
  </div>
  <p class="muted">
    The sliders drive the dot preview and <em>Draw</em>. The drawing area, rotation and hover come
    from the <code>{RESOURCES.imageToPoses}</code> config.
  </p>
</section>
