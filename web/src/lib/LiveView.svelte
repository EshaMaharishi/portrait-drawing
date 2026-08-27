<script lang="ts">
  import { CameraImage, createResourceClient } from '@viamrobotics/svelte-sdk'
  import { CameraClient, GenericServiceClient } from '@viamrobotics/sdk'
  import { RESOURCES, doCommand, errorMessage, type DrawingImage } from '../viam'

  let {
    partID,
    spacingMM,
    drawing = false,
  }: { partID: string; spacingMM: number; drawing?: boolean } = $props()

  // While a draw runs, show the image the robot is actually drawing: the
  // module captures the preview when the draw starts and serves it from
  // poses-to-arm's drawing_image command, so any page (even one opened
  // mid-draw) shows the same thing. Poll until it is available.
  const arm = createResourceClient(GenericServiceClient, () => partID, () => RESOURCES.posesToArm)
  let frozenSrc = $state<string | undefined>()
  let frozenError = $state('')
  async function fetchDrawingImage() {
    if (!arm.current) return
    try {
      const { png_base64, mime_type } = await doCommand<DrawingImage>(arm.current, { command: 'drawing_image' })
      if (!png_base64) return
      const bytes = Uint8Array.from(atob(png_base64), (c) => c.charCodeAt(0))
      if (frozenSrc) URL.revokeObjectURL(frozenSrc)
      frozenSrc = URL.createObjectURL(new Blob([bytes], { type: mime_type || 'image/png' }))
      frozenError = ''
    } catch (err) {
      frozenError = errorMessage(err)
    }
  }
  $effect(() => {
    void arm.current
    if (!drawing) {
      if (frozenSrc) URL.revokeObjectURL(frozenSrc)
      frozenSrc = undefined
      frozenError = ''
      return
    }
    if (frozenSrc) return
    fetchDrawingImage()
    const id = setInterval(fetchDrawingImage, 1000)
    return () => clearInterval(id)
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
        { point_spacing_mm: spacingMM },
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
      {#if frozenSrc}
        <img src={frozenSrc} alt="What the robot is drawing" />
      {:else if frozenError}
        <div class="banner error" style="margin: 0.75rem">{frozenError}</div>
      {:else}
        <p class="hint" style="padding: 2rem; text-align: center">Loading the drawing…</p>
      {/if}
      <figcaption><span>🤖 Drawing this</span></figcaption>
    </figure>
  {:else}
  <div class="tiles">
    <figure class="tile small">
      <CameraImage {partID} name={RESOURCES.camera} refetchInterval={500} width="100%" alt="Camera" />
      <figcaption><span>Camera</span></figcaption>
    </figure>
    <figure class="tile small">
      <CameraImage {partID} name={RESOURCES.face} refetchInterval={1000} width="100%" alt="Face" />
      <figcaption><span>Face</span></figcaption>
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
