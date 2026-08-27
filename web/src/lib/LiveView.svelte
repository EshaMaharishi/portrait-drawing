<script lang="ts">
  import { CameraImage, createResourceClient } from '@viamrobotics/svelte-sdk'
  import { CameraClient } from '@viamrobotics/sdk'
  import { RESOURCES, errorMessage } from '../viam'

  let {
    partID,
    threshold,
    spacingMM,
    drawing = false,
  }: { partID: string; threshold: number; spacingMM: number; drawing?: boolean } = $props()

  // While a draw runs, show a frozen copy of the preview as it was when Draw
  // was clicked, instead of the live feeds.
  let frozenSrc = $state<string | undefined>()
  $effect(() => {
    if (drawing) {
      if (!frozenSrc) frozenSrc = previewSrc
    } else {
      frozenSrc = undefined
    }
  })

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
    // Re-run when the client appears or a slider moves; pause while drawing.
    void imageToPoses.current
    void threshold
    void spacingMM
    if (drawing) return
    const debounce = setTimeout(refreshPreview, PREVIEW_DEBOUNCE_MS)
    const interval = setInterval(refreshPreview, PREVIEW_INTERVAL_MS)
    return () => {
      clearTimeout(debounce)
      clearInterval(interval)
    }
  })
</script>

<section class="views">
  {#if drawing}
    <figure class="tile hero drawing">
      <img src={frozenSrc ?? previewSrc} alt="What the robot is drawing" />
      <figcaption><span>🤖 Drawing this</span></figcaption>
    </figure>
  {:else}
  <div class="tiles">
    <figure class="tile small">
      <CameraImage {partID} name={RESOURCES.camera} refetchInterval={500} width="100%" alt="Camera" />
      <figcaption><span>Camera</span></figcaption>
    </figure>
    <figure class="tile small">
      <CameraImage {partID} name={RESOURCES.personOnly} refetchInterval={1000} width="100%" alt="Background removed" />
      <figcaption><span>Background removed</span></figcaption>
    </figure>
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
  </div>
  {/if}
</section>
