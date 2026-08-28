<script lang="ts">
  import { ViamProvider } from '@viamrobotics/svelte-sdk'
  import type { DialConf } from '@viamrobotics/sdk'
  import { getConnection, saveConnection } from './auth'
  import ConnectionBadge from './lib/ConnectionBadge.svelte'
  import PaperSetup from './lib/PaperSetup.svelte'
  import Adjust from './lib/Adjust.svelte'
  import LiveView from './lib/LiveView.svelte'
  import Draw from './lib/Draw.svelte'

  // $state.raw: the SDK structuredClone()s the dial config, and a deep
  // $state proxy cannot be cloned.
  let connection = $state.raw(getConnection())

  // Shared tuning values: Capture edits them, Draw sends them with draw.
  let spacingMM = $state(1)
  let shading = $state(true)
  let drawing = $state(false)
  let completed = $state(0)

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
    <div class="layout">
      <div class="controls">
        <PaperSetup partID={connection.machineId} locked={drawing} />
        <Adjust partID={connection.machineId} bind:spacingMM bind:shading locked={drawing} />
        <Draw partID={connection.machineId} {spacingMM} {shading} bind:drawing bind:completed />
      </div>
      <LiveView partID={connection.machineId} {spacingMM} {drawing} {completed} />
    </div>
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
