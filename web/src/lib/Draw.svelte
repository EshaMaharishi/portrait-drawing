<script lang="ts">
  import { createResourceClient } from '@viamrobotics/svelte-sdk'
  import { GenericServiceClient } from '@viamrobotics/sdk'
  import { RESOURCES, doCommand, errorMessage, type DrawStatus } from '../viam'

  let {
    partID,
    threshold,
    spacingMM,
    drawing = $bindable(false),
  }: { partID: string; threshold: number; spacingMM: number; drawing?: boolean } = $props()

  const arm = createResourceClient(GenericServiceClient, () => partID, () => RESOURCES.posesToArm)

  let status = $state<DrawStatus>({ state: 'idle', completed: 0, total: 0, error: '' })
  let error = $state('')

  const active = $derived(status.state === 'fetching' || status.state === 'drawing')
  $effect(() => {
    drawing = active
  })
  const percent = $derived(status.total > 0 ? Math.round((100 * status.completed) / status.total) : 0)

  async function refreshStatus() {
    if (!arm.current) return
    try {
      status = await doCommand<DrawStatus>(arm.current, { command: 'status' })
    } catch (err) {
      error = errorMessage(err)
    }
  }

  // Poll status once a second while a draw is running (and once on load /
  // after any command, so a draw started elsewhere shows up too).
  $effect(() => {
    if (!arm.current) return
    refreshStatus()
    if (!active) return
    const id = setInterval(refreshStatus, 1000)
    return () => clearInterval(id)
  })

  async function draw() {
    if (!arm.current) return
    error = ''
    try {
      await doCommand(arm.current, { command: 'draw', threshold, point_spacing_mm: spacingMM })
      status = { state: 'fetching', completed: 0, total: 0, error: '' }
    } catch (err) {
      error = errorMessage(err)
    }
    await refreshStatus()
  }

  async function stop() {
    if (!arm.current) return
    error = ''
    try {
      await doCommand(arm.current, { command: 'stop' })
    } catch (err) {
      error = errorMessage(err)
    }
    await refreshStatus()
  }

</script>

<section class="panel">
  <h2>Draw</h2>
  <div class="row">
    <button class="big" onclick={draw} disabled={active || !arm.current}>
      {active ? 'Drawing…' : 'Draw'}
    </button>
    <button class="big danger" onclick={stop} disabled={!arm.current}>Stop</button>
  </div>

  <div style="margin-top: 1rem">
    <progress max="100" value={percent}></progress>
    <div class="status">
      <span class="state">{status.state}</span>
      <span class="muted">
        {#if status.state === 'fetching'}computing poses…{:else if status.total > 0}{status.completed} / {status.total} poses ({percent}%){:else}—{/if}
      </span>
    </div>
    {#if status.state === 'error' && status.error}<p class="error">Draw failed: {status.error}</p>{/if}
  </div>
  {#if error}<p class="error">{error}</p>{/if}
</section>
