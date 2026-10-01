import { render } from "preact";
import { useEffect, useState } from "preact/hooks";
import "./style.css";

// ponytail: internal-only UI; replace this before exposing it outside the trusted LAN.
const CONFIG_TOKEN = "sendspin-bridge";
const buttonClass = "inline-flex min-h-[38px] items-center justify-center gap-2 whitespace-nowrap rounded-lg border border-transparent px-3.5 py-[7px] text-[13px] font-semibold";
const primaryButton = `${buttonClass} bg-accent text-[#102923] hover:enabled:bg-[#c3f8dc]`;
const quietButton = `${buttonClass} border-line bg-[#192a32] text-ink hover:bg-[#263941]`;
const secondaryButton = `${buttonClass} border-[#5b9680] bg-[#1b3c32] text-[#ddf9e8] hover:bg-[#29513f]`;
const cardClass = "overflow-hidden rounded-[9px] border border-line bg-surface open:border-[#597369]";
const inputClass = "min-w-0 rounded-[7px] border border-[#516971] bg-[#101e25] px-2.5 py-2 text-[13px] text-ink focus:border-accent";
const deviceNameClass = "grid min-w-0 flex-1 gap-[3px]";
const chevronClass = "size-2 shrink-0 rotate-45 border-b-2 border-r-2 border-[#a3bbb6] group-open:-translate-y-1 group-open:rotate-[225deg]";
const emptyClass = "rounded-[9px] border border-dashed border-[#52636c] p-[17px] text-[13px] text-muted";

function blankGroup() {
  const id = `group-${crypto.randomUUID()}`;
  return { id, exposed_name: "New group", port: 0, speaker_ids: [] };
}

function integers(config) {
  const copy = structuredClone(config);
  copy.speakers.forEach((speaker) => {
    speaker.delay_ms = speaker.delay_ms === "" ? 0 : Number(speaker.delay_ms);
  });
  return copy;
}

function App() {
  const [config, setConfig] = useState(null);
  const [message, setMessage] = useState("");
  const [saving, setSaving] = useState(false);
  const [newGroupId, setNewGroupId] = useState(null);

  const api = async (path, options = {}) => {
    const response = await fetch(path, {
      ...options,
      headers: { "X-Config-Token": CONFIG_TOKEN, ...(options.headers || {}) },
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
    return data;
  };

  const load = async () => {
    try {
      setConfig(await api("/api/config"));
      setMessage("");
    } catch (error) {
      setMessage(error.message);
    }
  };

  useEffect(() => { load(); }, []);

  const update = (fn) => setConfig((current) => {
    const copy = structuredClone(current);
    fn(copy);
    return copy;
  });

  const setVolume = async (speakerId, volume) => {
    try {
      const result = await api(`/api/speakers/${encodeURIComponent(speakerId)}/volume`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ volume }),
      });
      update((copy) => {
        const speaker = copy.speakers.find((item) => item.id === speakerId);
        if (speaker) speaker.volume = result.volume;
      });
    } catch (error) {
      await load();
      setMessage(error.message);
    }
  };

  const save = async (event) => {
    event.preventDefault();
    setSaving(true);
    try {
      const result = await api("/api/config", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(integers(config)),
      });
      setConfig(result.config);
      setMessage("Saved. Restarting bridge…");
      setTimeout(waitForRestart, 700);
    } catch (error) {
      setMessage(error.message);
      setSaving(false);
    }
  };

  const waitForRestart = async () => {
    for (let attempt = 0; attempt < 30; attempt += 1) {
      try {
        await api("/api/health");
        await load();
        setMessage("Configuration applied.");
        setSaving(false);
        return;
      } catch {
        await new Promise((resolve) => setTimeout(resolve, 500));
      }
    }
    setMessage("Saved, but the bridge is taking longer to return. Reload this page.");
    setSaving(false);
  };

  return <div class="mx-auto max-w-[1040px] px-6 pb-10 max-sm:px-3.5">
    <header class="sticky top-0 z-10 flex min-h-16 items-center justify-between gap-4 border-b border-line bg-canvas py-2 max-[800px]:flex-wrap max-[800px]:gap-1.5">
      <div class="flex items-center gap-2.5 whitespace-nowrap text-base font-bold tracking-tight">
        <span class="flex size-[30px] items-center justify-center gap-[3px] rounded-lg border border-[#5c9685] bg-[#173832]" aria-hidden="true"><span class="h-2.5 w-[3px] rounded-full bg-accent" /><span class="h-[18px] w-[3px] rounded-full bg-accent" /><span class="h-3.5 w-[3px] rounded-full bg-accent" /><span class="h-[7px] w-[3px] rounded-full bg-accent" /></span>
        <span>Sendspin<span class="font-normal text-muted"> Bridge</span></span>
      </div>
      <input class={`${inputClass} ml-auto box-border h-[38px] w-[120px] max-[800px]:w-[min(30vw,120px)]`} type="text" form="config-form" aria-label="Exposed name suffix" placeholder="Suffix" value={config?.exposed_suffix ?? ""} disabled={!config || saving} onInput={(event) => { const value = event.currentTarget.value; update((copy) => { copy.exposed_suffix = value; }); }} />
      <div class="flex flex-wrap items-center justify-end gap-2.5 max-[800px]:w-full max-[800px]:gap-2">
        <p class="m-0 max-w-[210px] text-xs text-accent empty:hidden max-[800px]:order-1 max-[800px]:w-full max-[800px]:max-w-none max-[800px]:text-right" role="status" aria-live="polite">{config ? message : ""}</p>
        <button class={quietButton} type="button" disabled={saving} onClick={load} title="Reload discards unsaved changes">↻ <span>Reload</span></button>
        <button class={primaryButton} type="submit" form="config-form" disabled={!config || saving}>{saving ? "Saving…" : "Save & restart"}</button>
      </div>
    </header>

    <main>
      <h1 class="sr-only">Speakers & groups</h1>
      {!config ? <div class={`${emptyClass} mt-7`} role="status">
        <p class="mb-3">{message || "Loading configuration…"}</p>
        {message && <button class={primaryButton} type="button" onClick={load}>Try again</button>}
      </div> : <form id="config-form" onSubmit={save}>
        <section class="mt-7" aria-labelledby="speakers-title">
          <div class="mb-3 flex items-center justify-between gap-3.5 max-sm:items-start">
            <div><h2 id="speakers-title" class="mb-0.5 text-[19px] tracking-tight">Speakers <span class="ml-1 text-sm font-normal text-muted">{config.speakers.length}</span></h2><p class="text-xs text-muted">Discovered automatically · Adjust volume directly or expand settings to edit.</p></div>
          </div>
          <div class="grid gap-2">
            {config.speakers.map((speaker, index) => <SpeakerCard key={speaker.id} speaker={speaker} onChange={(field, value) => update((copy) => { copy.speakers[index][field] = value; })} onPreviewVolume={(volume) => update((copy) => { copy.speakers[index].volume = volume; })} onCommitVolume={(volume) => setVolume(speaker.id, volume)} />)}
            {config.speakers.length === 0 && <p class={emptyClass}>No speakers discovered yet. They will appear here when they connect.</p>}
          </div>
        </section>

        <section class="mt-7" aria-labelledby="groups-title">
          <div class="mb-3 flex items-center justify-between gap-3.5 max-sm:items-start">
            <div><h2 id="groups-title" class="mb-0.5 text-[19px] tracking-tight">Groups <span class="ml-1 text-sm font-normal text-muted">{config.groups.length}</span></h2><p class="text-xs text-muted">One destination for multiple speakers.</p></div>
            <button class={`${secondaryButton} max-sm:mt-0.5 max-sm:px-[9px]`} type="button" onClick={() => { const group = blankGroup(); setNewGroupId(group.id); update((copy) => copy.groups.push(group)); }}>+ Add group</button>
          </div>
          <div class="grid gap-2">
            {config.groups.map((group, index) => <GroupCard key={group.id} group={group} speakers={config.speakers} startOpen={group.id === newGroupId} onChange={(field, value) => update((copy) => { copy.groups[index][field] = value; })} onToggle={(speakerId) => update((copy) => {
              const members = copy.groups[index].speaker_ids;
              copy.groups[index].speaker_ids = members.includes(speakerId) ? members.filter((id) => id !== speakerId) : [...members, speakerId];
            })} onDelete={() => update((copy) => { copy.groups.splice(index, 1); })} />)}
            {config.groups.length === 0 && <p class={emptyClass}>No groups yet. Add one to play across rooms.</p>}
          </div>
        </section>
      </form>}
    </main>
  </div>;
}

function SpeakerCard({ speaker, onChange, onPreviewVolume, onCommitVolume }) {
  const connected = Boolean(speaker.connected);
  const volume = Number(speaker.volume ?? 100);
  const volumeId = `volume-${encodeURIComponent(speaker.id)}`;
  return <article class={`${cardClass} speaker-card`}>
    <div class="flex min-h-16 flex-wrap items-center justify-between gap-x-[18px] gap-y-3 px-[17px] py-3 max-sm:gap-y-2">
      <div class="flex min-w-[180px] flex-1 items-center gap-[15px] max-sm:w-full">
        <span class={`inline-flex min-w-16 items-center gap-1.5 text-[11px] ${connected ? "text-[#b6efd3]" : "text-muted"}`}><span class={`size-[7px] shrink-0 rounded-full ${connected ? "bg-[#82e5ae]" : "bg-[#778b8d]"}`} />{connected ? "Online" : "Offline"}</span>
        <span class={deviceNameClass}><strong class="overflow-hidden text-ellipsis whitespace-nowrap text-sm font-semibold">{speaker.exposed_name || speaker.id}</strong><small class="overflow-hidden text-ellipsis whitespace-nowrap text-xs text-muted">{speaker.exposed ? "Exposed" : "Not exposed"} · {speaker.id}</small></span>
      </div>
      <div class="flex items-center gap-6 max-sm:w-full max-sm:justify-between">
        <div class="w-[170px] max-sm:w-[min(55%,200px)]">
          <label class="mb-0.5 flex justify-between text-[11px] text-muted" for={volumeId}>Volume <output class="font-semibold text-ink" for={volumeId}>{volume}%</output></label>
          <input class="m-0 w-full cursor-pointer accent-accent disabled:cursor-not-allowed disabled:opacity-45" id={volumeId} type="range" min="0" max="100" value={volume} disabled={!connected} onInput={(event) => onPreviewVolume(Number(event.currentTarget.value))} onChange={(event) => onCommitVolume(Number(event.currentTarget.value))} />
        </div>
        <Toggle checked={speaker.exposed} onChange={(exposed) => onChange("exposed", exposed)}>Exposed</Toggle>
      </div>
    </div>
    <details class="group border-t border-line">
      <summary class="flex w-fit cursor-pointer list-none items-center gap-1 px-[17px] py-[7px] text-xs text-[#bdd9cf] hover:bg-[#21333a] [&::-webkit-details-marker]:hidden">Settings <span class={`${chevronClass} ml-1 size-1.5`} aria-hidden="true" /></summary>
      <div class="grid gap-4 border-t border-line p-[17px]"><div class="grid grid-cols-2 gap-4 max-sm:grid-cols-1">
        <Text label="Exposed name" value={speaker.exposed_name} onInput={(value) => onChange("exposed_name", value)} />
        <NumberField label="Group offset (ms)" value={speaker.delay_ms ?? 0} min="-500" max="500" onInput={(value) => onChange("delay_ms", value)} />
      </div><p class="m-0 text-xs leading-normal text-muted">Group offset: −500 to 500 ms.</p></div>
    </details>
  </article>;
}

function Toggle({ checked, onChange, children }) {
  return <label class="flex cursor-pointer items-center gap-2 text-xs text-[#d9e8e4]">
    <input class="peer sr-only" type="checkbox" checked={checked} onChange={(event) => onChange(event.currentTarget.checked)} />
    <span aria-hidden="true" class="relative h-[21px] w-9 shrink-0 rounded-full bg-[#52616b] before:absolute before:top-[3px] before:left-[3px] before:size-[15px] before:rounded-full before:bg-white before:content-[''] before:transition-transform peer-checked:bg-[#4baf84] peer-checked:before:translate-x-[15px] peer-focus-visible:outline-2 peer-focus-visible:outline-offset-[3px] peer-focus-visible:outline-accent motion-reduce:before:transition-none" />
    <span>{children}</span>
  </label>;
}

function GroupCard({ group, speakers, startOpen, onChange, onToggle, onDelete }) {
  const members = group.speaker_ids.map((id) => speakers.find((speaker) => speaker.id === id)?.exposed_name || id).join(", ");
  return <details class={`${cardClass} group device-card`} open={startOpen}>
    <summary class="flex min-h-[66px] cursor-pointer list-none items-center gap-3 px-[17px] py-3 hover:bg-[#21333a] [&::-webkit-details-marker]:hidden">
      <span class={deviceNameClass}><strong class="overflow-hidden text-ellipsis whitespace-nowrap text-sm font-semibold">{group.exposed_name || group.id}</strong><small class="overflow-hidden text-ellipsis whitespace-nowrap text-xs text-muted">{members || "No speakers selected"}</small></span>
      <span class="whitespace-nowrap text-xs text-muted">{group.speaker_ids.length} {group.speaker_ids.length === 1 ? "speaker" : "speakers"}</span>
      <span class={`${chevronClass} mb-1 ml-2 mr-[3px]`} aria-hidden="true" />
    </summary>
    <div class="grid gap-4 border-t border-line p-[17px]">
      <Text label="Exposed name" value={group.exposed_name} onInput={(value) => onChange("exposed_name", value)} />
      <fieldset class="m-0 min-w-0 border-0 p-0"><legend class="p-0 text-xs font-semibold text-[#d1e1de]">Included speakers</legend>
        <div class="mt-2.5 flex flex-wrap gap-[7px]">{speakers.map((speaker) => {
          const included = group.speaker_ids.includes(speaker.id);
          return <button class={`member-chip inline-flex items-center gap-[7px] rounded-[7px] border px-2.5 py-1.5 text-xs hover:border-[#8abdac] ${included ? "border-[#73b990] bg-[#234437] text-[#d8f7e2]" : "border-[#516971] bg-[#192b33] text-[#d1e1de]"}`} type="button" aria-pressed={included} key={speaker.id} onClick={() => onToggle(speaker.id)}><span aria-hidden="true" class="text-sm">{included ? "✓" : "+"}</span>{speaker.exposed_name || speaker.id}</button>;
        })}</div>
        {speakers.length === 0 && <p class="m-0 text-xs leading-normal text-muted">No speakers discovered yet.</p>}
      </fieldset>
      <div class="border-t border-line pt-3"><button class="p-0 text-xs text-[#ffbdb4] hover:underline" type="button" onClick={onDelete}>Remove group</button></div>
    </div>
  </details>;
}

function Text({ label, value, onInput }) {
  return <label class="grid gap-1.5 text-xs font-semibold text-[#d1e1de]">{label}<input class={`${inputClass} w-full`} value={value ?? ""} onInput={(event) => onInput(event.currentTarget.value)} /></label>;
}

function NumberField({ label, value, onInput, min, max }) {
  return <label class="grid gap-1.5 text-xs font-semibold text-[#d1e1de]">{label}<input class={`${inputClass} w-full`} type="number" min={min} max={max} value={value ?? ""} onInput={(event) => onInput(event.currentTarget.value)} /></label>;
}

render(<App />, document.getElementById("app"));
