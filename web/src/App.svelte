<script lang="ts">
  import { ViamProvider } from '@viamrobotics/svelte-sdk'
  import type { DialConf } from '@viamrobotics/sdk'
  import { getConnection, saveConnection } from './auth'
  import ConnectionBadge from './lib/ConnectionBadge.svelte'
  import ArmPosition from './lib/ArmPosition.svelte'
  import Capture from './lib/Capture.svelte'
  import Draw from './lib/Draw.svelte'

  // $state.raw: the SDK structuredClone()s the dial config, and a deep
  // $state proxy cannot be cloned.
  let connection = $state.raw(getConnection())

  // Shared tuning values: Capture edits them, Draw sends them with draw.
  let threshold = $state(235)
  let spacingMM = $state(1)
  let drawing = $state(false)

  let host = $state('')
  let keyId = $state('')
  let key = $state('')

  const dialConfigs = $derived<Record<string, DialConf>>(
    connection ? { [connection.machineId]: connection.dialConf } : {},
  )

  function connectManually() {
    saveConnection(host.trim(), keyId.trim(), key.trim())
    connection = getConnection()
  }
</script>

{#if connection}
  <ViamProvider {dialConfigs}>
    <header>
      <h1>Portrait Drawing</h1>
      <ConnectionBadge partID={connection.machineId} dialConf={connection.dialConf} />
    </header>
    <ArmPosition partID={connection.machineId} locked={drawing} />
    <Capture partID={connection.machineId} bind:threshold bind:spacingMM locked={drawing} />
    <Draw partID={connection.machineId} {threshold} {spacingMM} bind:drawing />
  </ViamProvider>
{:else}
  <header><h1>Portrait Drawing</h1></header>
  <section class="step">
    <div class="step-head"><h2>Connect to a machine</h2></div>
    <p class="hint">
      No machine credentials were found. When hosted on viamapplications.com (or via
      <code>viam module local-app-testing</code>) these are injected automatically; for
      plain local development enter them here.
    </p>
    <div class="row">
      <input type="text" placeholder="host (e.g. my-machine-main.abc123.viam.cloud)" bind:value={host} />
      <input type="text" placeholder="API key ID" bind:value={keyId} />
      <input type="password" placeholder="API key" bind:value={key} />
      <button onclick={connectManually} disabled={!host || !keyId || !key}>Connect</button>
    </div>
  </section>
{/if}
