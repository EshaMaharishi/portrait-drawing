<script lang="ts">
  import { CameraImage, createResourceClient } from '@viamrobotics/svelte-sdk'
  import { CameraClient } from '@viamrobotics/sdk'
  import { RESOURCES, errorMessage } from '../viam'

  let {
    partID,
    threshold = $bindable(),
    spacingMM = $bindable(),
    locked = false,
  }: { partID: string; threshold: number; spacingMM: number; locked?: boolean } = $props()

  const imageToPoses = createResourceClient(CameraClient, () => partID, () => RESOURCES.imageToPoses)

  // The dot preview is fetched by hand so the slider values can be passed as
  // `extra` overrides — the same values Draw uses. It is recomputed
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

<section class="step" class:locked class:active={!locked}>
  <div class="step-head">
    <span class="step-num">2</span>
    <h2>Check the picture and adjust</h2>
  </div>

  <div class="tiles">
    <figure class="tile hero">
      <CameraImage {partID} name={RESOURCES.sketched} refetchInterval={2000} width="100%" alt="Sketch" />
      <figcaption><span>✏️ Sketch</span></figcaption>
    </figure>
    <figure class="tile hero">
      {#if previewError}
        <div class="banner error" style="margin: 0.75rem">{previewError}</div>
      {:else}
        <img src={previewSrc} alt="Dot preview (as drawn)" />
      {/if}
      {#if previewUpdating}<span class="overlay">updating…</span>{/if}
      <figcaption><span>🤖 What the robot will draw</span></figcaption>
    </figure>
    <figure class="tile small">
      <CameraImage {partID} name={RESOURCES.camera} refetchInterval={500} width="100%" alt="Camera" />
      <figcaption><span>Camera</span></figcaption>
    </figure>
    <figure class="tile small">
      <CameraImage {partID} name={RESOURCES.personOnly} refetchInterval={1000} width="100%" alt="Background removed" />
      <figcaption><span>Background removed</span></figcaption>
    </figure>
  </div>

  <div class="sliders">
    <label class="slider">
      <span>Darkness</span>
      <span class="value">{threshold}</span>
      <input type="range" min="0" max="255" step="1" bind:value={threshold} disabled={locked} />
      <span class="hint">Higher keeps more of the sketch (more dots, longer drawing).</span>
    </label>
    <label class="slider">
      <span>Dot spacing</span>
      <span class="value">{spacingMM.toFixed(1)} mm</span>
      <input type="range" min="0.5" max="5" step="0.1" bind:value={spacingMM} disabled={locked} />
      <span class="hint">Smaller is finer detail; larger draws faster.</span>
    </label>
  </div>
  {#if locked}<p class="hint" style="margin-top: 0.5rem">Settings are locked while the robot is drawing.</p>{/if}
</section>
