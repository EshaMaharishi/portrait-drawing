<script lang="ts">
  import { createResourceClient } from '@viamrobotics/svelte-sdk'
  import { GenericServiceClient, SwitchClient } from '@viamrobotics/sdk'
  import { RESOURCES, doCommand, errorMessage } from '../viam'

  let { partID, locked = false }: { partID: string; locked?: boolean } = $props()

  const positions = createResourceClient(SwitchClient, () => partID, () => RESOURCES.positions)
  const arm = createResourceClient(GenericServiceClient, () => partID, () => RESOURCES.posesToArm)

  // Hover the pen over each paper corner (runs in the background; the Draw
  // panel shows progress and Stop cancels it).
  async function showPaper() {
    if (!arm.current) return
    error = ''
    try {
      await doCommand(arm.current, { command: 'show_paper' })
    } catch (err) {
      error = errorMessage(err)
    }
  }

  // The picture-taking pose is the last preset on the position switch.
  let moving = $state(false)
  let error = $state('')
  async function goToPicturePosition() {
    const client = positions.current
    if (!client) return
    moving = true
    error = ''
    try {
      const [count] = await client.getNumberOfPositions()
      if (count < 1) throw new Error(`${RESOURCES.positions} has no positions configured`)
      await client.setPosition(count - 1)
    } catch (err) {
      error = errorMessage(err)
    } finally {
      moving = false
    }
  }
</script>

<section class="step" class:locked>
  <div class="step-head">
    <span class="step-num">1</span>
    <h2>Go to picture taking position</h2>
  </div>
  <div class="row">
    <button onclick={goToPicturePosition} disabled={moving || locked || !positions.current}>
      <span class="icon">📷</span>
      {moving ? 'Moving arm…' : 'Go to picture-taking position'}
    </button>
    <button class="secondary" onclick={showPaper} disabled={locked || !arm.current}>
      <span class="icon">📄</span> Show me where the paper goes
    </button>
  </div>
  {#if error}<div class="banner error">{error}</div>{/if}
</section>
