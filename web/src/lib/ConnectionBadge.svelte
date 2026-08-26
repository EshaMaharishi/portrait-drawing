<script lang="ts">
  import { useRobotConnection } from '@viamrobotics/svelte-sdk'
  import { MachineConnectionEvent, type DialConf } from '@viamrobotics/sdk'

  let { partID, dialConf }: { partID: string; dialConf: DialConf } = $props()
  const host = $derived(dialConf.host)
  const connection = useRobotConnection(() => partID)

  const label = $derived.by(() => {
    switch (connection.connectionStatus) {
      case MachineConnectionEvent.CONNECTED: return 'Connected'
      case MachineConnectionEvent.CONNECTING: return 'Connecting…'
      case MachineConnectionEvent.DISCONNECTING: return 'Disconnecting…'
      default: return 'Disconnected'
    }
  })
  const cls = $derived(
    connection.connectionStatus === MachineConnectionEvent.CONNECTED ? 'ok'
      : connection.connectionStatus === MachineConnectionEvent.CONNECTING ? ''
      : 'bad',
  )
  $effect(() => {
    if (connection.error) console.error('viam connection error', connection.error)
  })
</script>

<div style="text-align: right">
  <span class="badge {cls}" title={host}>{label} · {host}</span>
  {#if connection.error}
    <p class="error" style="margin: 0.4rem 0 0">{connection.error.message}</p>
  {/if}
  <button class="secondary" style="margin-top: 0.4rem; padding: 0.3rem 0.7rem; font-size: 0.8rem"
    onclick={() => connection.connect(dialConf)}>Reconnect</button>
</div>
