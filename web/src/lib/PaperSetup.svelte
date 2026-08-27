<script lang="ts">
  import { createResourceClient } from '@viamrobotics/svelte-sdk'
  import { GenericServiceClient } from '@viamrobotics/sdk'
  import { RESOURCES, doCommand, errorMessage } from '../viam'

  let { partID, locked = false }: { partID: string; locked?: boolean } = $props()

  const arm = createResourceClient(GenericServiceClient, () => partID, () => RESOURCES.posesToArm)

  // Hover the pen over each paper corner (runs in the background; the Draw
  // panel shows progress and Stop cancels it).
  let error = $state('')
  async function showPaper() {
    if (!arm.current) return
    error = ''
    try {
      await doCommand(arm.current, { command: 'show_paper' })
    } catch (err) {
      error = errorMessage(err)
    }
  }
</script>

<section class="step" class:locked>
  <div class="step-head">
    <span class="step-num">1</span>
    <h2>Set up paper</h2>
  </div>
  <div class="row">
    <button onclick={showPaper} disabled={locked || !arm.current}>
      <span class="icon">📄</span> Show me where the paper goes
    </button>
  </div>
  {#if error}<div class="banner error">{error}</div>{/if}
</section>
