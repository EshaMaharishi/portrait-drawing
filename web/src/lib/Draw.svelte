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
  // Whether the user has acknowledged a finished/stopped/failed draw, so the
  // panel returns to its ready state.
  let acknowledged = $state(false)

  const active = $derived(status.state === 'fetching' || status.state === 'drawing')
  $effect(() => {
    drawing = active
  })
  const percent = $derived(status.total > 0 ? Math.round((100 * status.completed) / status.total) : 0)
  const finished = $derived(!acknowledged && (status.state === 'complete' || status.state === 'stopped' || status.state === 'error'))

  // Time estimate from the observed pose rate since drawing started.
  let rateStart = $state<{ t: number; completed: number } | undefined>()
  let eta = $state('')
  $effect(() => {
    if (status.state !== 'drawing') {
      rateStart = undefined
      eta = ''
      return
    }
    const now = Date.now()
    if (!rateStart) {
      rateStart = { t: now, completed: status.completed }
      return
    }
    const done = status.completed - rateStart.completed
    const secs = (now - rateStart.t) / 1000
    if (done < 20 || secs < 10) return
    const remaining = (status.total - status.completed) / (done / secs)
    const mins = Math.round(remaining / 60)
    eta = mins < 1 ? 'less than a minute left' : `about ${mins} min left`
  })

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
    acknowledged = false
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

<section class="step" class:active={active}>
  <div class="step-head">
    <span class="step-num">3</span>
    <h2>Draw</h2>
  </div>

  <div class="row nowrap">
    {#if finished}
      <button class="huge" onclick={() => (acknowledged = true)}><span class="icon">↩</span> Draw another</button>
    {:else}
      <button class="huge" onclick={draw} disabled={active || !arm.current}>
        <span class="icon">✏️</span>
        {active ? 'Drawing…' : 'Draw my portrait'}
      </button>
    {/if}
    <button class={active ? 'huge danger' : 'danger'} onclick={stop} disabled={!arm.current}>
      <span class="icon">⏹</span> Stop
    </button>
  </div>

  {#if active || finished}
    <div class="progress-wrap">
      <progress max="100" value={percent}></progress>
      <div class="readout">
        {#if status.state === 'fetching'}
          <span class="big">Getting ready…</span>
          <span class="detail">turning the sketch into dots</span>
        {:else if status.state === 'drawing'}
          <span class="big">{percent}%</span>
          <span class="detail">{status.completed.toLocaleString()} / {status.total.toLocaleString()} moves{eta ? ` · ${eta}` : ''}</span>
        {:else if status.state === 'complete'}
          <span class="big ok">Done! 🎉</span>
          <span class="detail">{status.total.toLocaleString()} moves</span>
        {:else if status.state === 'stopped'}
          <span class="big bad">Stopped</span>
          <span class="detail">after {status.completed.toLocaleString()} of {status.total.toLocaleString()} moves</span>
        {:else if status.state === 'error'}
          <span class="big bad">Something went wrong</span>
        {/if}
      </div>
      {#if status.state === 'error' && status.error}<div class="banner error">{status.error}</div>{/if}
    </div>
  {/if}
  {#if error}<div class="banner error">{error}</div>{/if}
</section>
