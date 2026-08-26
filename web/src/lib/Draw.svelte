<script lang="ts">
  import { createResourceClient } from '@viamrobotics/svelte-sdk'
  import { GenericServiceClient, WorldStateStoreClient } from '@viamrobotics/sdk'
  import { RESOURCES, doCommand, errorMessage, type DrawStatus, type VisualizeResult } from '../viam'

  let { partID, threshold, spacingMM }: { partID: string; threshold: number; spacingMM: number } = $props()

  const arm = createResourceClient(GenericServiceClient, () => partID, () => RESOURCES.posesToArm)
  const scene = createResourceClient(WorldStateStoreClient, () => partID, () => RESOURCES.scene)

  let status = $state<DrawStatus>({ state: 'idle', completed: 0, total: 0, error: '' })
  let sceneResult = $state<VisualizeResult | undefined>()
  let sceneBusy = $state(false)
  let error = $state('')

  const active = $derived(status.state === 'fetching' || status.state === 'drawing')
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
    if (!confirm('Start drawing? The arm will move.')) return
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

  async function visualize(clear = false) {
    if (!scene.current) return
    sceneBusy = true
    error = ''
    try {
      if (clear) {
        await doCommand(scene.current, { command: 'clear' })
        sceneResult = undefined
      } else {
        sceneResult = await doCommand<VisualizeResult>(scene.current, {
          command: 'visualize',
          threshold,
          point_spacing_mm: spacingMM,
          max_poses: 2000,
        })
      }
    } catch (err) {
      error = errorMessage(err)
    } finally {
      sceneBusy = false
    }
  }
</script>

<section class="panel">
  <h2>Draw</h2>
  <div class="row">
    <button class="secondary" onclick={() => visualize()} disabled={sceneBusy || !scene.current}>
      {sceneBusy ? 'Working…' : 'Visualize in 3D'}
    </button>
    <button class="secondary" onclick={() => visualize(true)} disabled={sceneBusy || !scene.current}>Clear 3D</button>
    {#if sceneResult}
      <span class="muted">showing {sceneResult.shown} of {sceneResult.total} poses in the visualizer</span>
    {/if}
  </div>

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
        {#if status.total > 0}{status.completed} / {status.total} poses ({percent}%){:else}—{/if}
      </span>
    </div>
    {#if status.state === 'error' && status.error}<p class="error">Draw failed: {status.error}</p>{/if}
  </div>
  {#if error}<p class="error">{error}</p>{/if}
</section>
