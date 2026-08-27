<script lang="ts">
  let {
    threshold = $bindable(),
    spacingMM = $bindable(),
    locked = false,
  }: { threshold: number; spacingMM: number; locked?: boolean } = $props()
</script>

<section class="step" class:locked class:active={!locked}>
  <div class="step-head">
    <span class="step-num">2</span>
    <h2>Check the picture, then adjust</h2>
  </div>
  <div class="sliders">
    <label class="slider">
      <span>Darkness</span>
      <span class="value">{threshold}</span>
      <input type="range" min="0" max="255" step="1" bind:value={threshold} disabled={locked} />
      <span class="hint">Higher keeps more of the sketch (more dots, longer drawing).</span>
    </label>
    <label class="slider">
      <span>Dot spacing</span>
      <span class="value">{spacingMM.toFixed(1)} mm</span>
      <input type="range" min="0.5" max="5" step="0.1" bind:value={spacingMM} disabled={locked} />
      <span class="hint">Smaller is finer detail; larger draws faster.</span>
    </label>
  </div>
  {#if locked}<p class="hint" style="margin-top: 0.5rem">Settings are locked while the robot is drawing.</p>{/if}
</section>
