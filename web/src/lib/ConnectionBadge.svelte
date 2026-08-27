<script lang="ts">
  import { useRobotConnection } from '@viamrobotics/svelte-sdk'
  import { MachineConnectionEvent, type DialConf } from '@viamrobotics/sdk'

  let { partID, dialConf }: { partID: string; dialConf: DialConf } = $props()
  const connection = useRobotConnection(() => partID)

  const connected = $derived(connection.connectionStatus === MachineConnectionEvent.CONNECTED)
  const label = $derived.by(() => {
    switch (connection.connectionStatus) {
      case MachineConnectionEvent.CONNECTED: return 'Robot connected'
      case MachineConnectionEvent.CONNECTING: return 'Connecting…'
      case MachineConnectionEvent.DISCONNECTING: return 'Disconnecting…'
      default: return 'Robot disconnected'
    }
  })
  const cls = $derived(connected ? 'ok' : connection.connectionStatus === MachineConnectionEvent.CONNECTING ? '' : 'bad')
  $effect(() => {
    if (connection.error) console.error('viam connection error', connection.error)
  })
</script>

<div class="conn">
  <span class="badge {cls}" title="{dialConf.host}{connection.error ? ` — ${connection.error.message}` : ''}">
    {connected ? '●' : '○'} {label}
  </span>
  {#if !connected}
    <button class="secondary small" onclick={() => connection.connect(dialConf)}>Reconnect</button>
  {/if}
</div>
