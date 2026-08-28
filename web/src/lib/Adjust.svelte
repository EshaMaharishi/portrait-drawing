<script lang="ts">
  import { createResourceClient } from '@viamrobotics/svelte-sdk'
  import { SwitchClient } from '@viamrobotics/sdk'
  import { RESOURCES, errorMessage } from '../viam'

  let {
    partID,
    spacingMM = $bindable(),
    shading = $bindable(),
    locked = false,
  }: { partID: string; spacingMM: number; shading: boolean; locked?: boolean } = $props()

  const positions = createResourceClient(SwitchClient, () => partID, () => RESOURCES.positions)

  // The picture-taking pose is the last preset on the position switch.
  let moving = $state(false)
  let moveError = $state('')
  async function goToPicturePosition() {
    const client = positions.current
    if (!client) return
    moving = true
    moveError = ''
    try {
      const [count] = await client.getNumberOfPositions()
      if (count < 1) throw new Error(`${RESOURCES.positions} has no positions configured`)
      await client.setPosition(count - 1)
    } catch (err) {
      moveError = errorMessage(err)
    } finally {
      moving = false
    }
  }
</script>

<section class="step" class:locked class:active={!locked}>
  <div class="step-head">
    <span class="step-num">2</span>
    <h2>Check the picture, then adjust</h2>
  </div>
  <div class="row" style="margin-bottom: 0.75rem">
    <button onclick={goToPicturePosition} disabled={moving || locked || !positions.current}>
      <span class="icon">📷</span>
      {moving ? 'Moving arm…' : 'Click to move arm to picture-taking position'}
    </button>
  </div>
  {#if moveError}<div class="banner error" style="margin: 0 0 0.75rem">{moveError}</div>{/if}
  <div class="sliders">
    <label class="slider">
      <span>Dot spacing</span>
      <span class="value">{spacingMM.toFixed(1)} mm</span>
      <input type="range" min="0.5" max="5" step="0.1" bind:value={spacingMM} disabled={locked} />
      <span class="hint">Smaller is finer detail; larger draws faster.</span>
    </label>
    <label class="check">
      <input type="checkbox" bind:checked={shading} disabled={locked} />
      <span>Shading</span>
      <span class="hint">
        Rests the pen on darker dots so more ink bleeds in. Turn it off to give every dot a
        single touch, which draws much faster.
      </span>
    </label>
  </div>
  {#if locked}<p class="hint" style="margin-top: 0.5rem">Settings are locked while the robot is drawing.</p>{/if}
</section>
