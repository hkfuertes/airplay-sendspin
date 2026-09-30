import { render } from "preact";
import { useEffect, useState } from "preact/hooks";
import "./style.css";

// ponytail: internal-only UI; replace this before exposing it outside the trusted LAN.
const CONFIG_TOKEN = "airplay-sendspin";
const inputClass = "mt-1";
const buttonClass = "rounded-lg px-3 py-2 text-sm font-semibold transition";
const cardClass = "rounded-2xl border border-slate-800 bg-slate-900/70 p-5 shadow-lg shadow-black/10";

function blankGroup() {
  const id = `group-${crypto.randomUUID()}`;
  return { id, airplay_name: "New group", port: 0, speaker_ids: [] };
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

  if (!config) return <main class="mx-auto max-w-6xl p-8 text-slate-300">Loading configuration… {message}</main>;

  return <main class="mx-auto max-w-6xl p-4 pb-16 sm:p-8">
    <header class="mb-8 rounded-2xl border border-slate-800 bg-gradient-to-br from-slate-900 to-slate-950 p-6 shadow-xl shadow-black/20 sm:flex sm:items-end sm:justify-between">
      <div>
        <p class="text-sm font-semibold tracking-[0.18em] text-cyan-300">AIRPLAY SENDSPIN</p>
        <h1 class="mt-1 text-3xl font-bold tracking-tight">Configuration</h1>
        <p class="mt-2 max-w-2xl text-sm text-slate-400">{config.speakers.length} speakers discovered. Saving restarts the bridge and stops active audio.</p>
      </div>
      <button class={`${buttonClass} mt-4 bg-slate-800 hover:bg-slate-700 sm:mt-0`} type="button" onClick={load}>Reload</button>
    </header>

    <form onSubmit={save} class="space-y-8">
      <section class={cardClass}>
        <Text label="AirPlay suffix" value={config.airplay_suffix} onInput={(value) => update((copy) => { copy.airplay_suffix = value; })} />
      </section>

      <section>
        <div class="mb-4">
          <h2 class="text-xl font-bold">Speakers</h2>
          <p class="mt-1 text-sm text-slate-400">They are discovered automatically. Adjust only what changes daily use.</p>
        </div>
        <div class="grid gap-4 lg:grid-cols-2">
          {config.speakers.map((speaker, index) => <SpeakerCard key={speaker.id} speaker={speaker} onChange={(field, value) => update((copy) => { copy.speakers[index][field] = value; })} onPreviewVolume={(volume) => update((copy) => { copy.speakers[index].volume = volume; })} onCommitVolume={(volume) => setVolume(speaker.id, volume)} />)}
        </div>
      </section>

      <section>
        <div class="mb-4 flex items-center justify-between gap-4">
          <div>
            <h2 class="text-xl font-bold">Groups</h2>
            <p class="mt-1 text-sm text-slate-400">Each group appears as one shared AirPlay destination.</p>
          </div>
          <button class={`${buttonClass} shrink-0 bg-cyan-500 text-slate-950 hover:bg-cyan-400`} type="button" onClick={() => update((copy) => copy.groups.push(blankGroup()))}>Add group</button>
        </div>
        <div class="space-y-4">
          {config.groups.map((group, index) => <GroupCard key={group.id} group={group} speakers={config.speakers} onChange={(field, value) => update((copy) => { copy.groups[index][field] = value; })} onToggle={(speakerId) => update((copy) => {
            const members = copy.groups[index].speaker_ids;
            copy.groups[index].speaker_ids = members.includes(speakerId) ? members.filter((id) => id !== speakerId) : [...members, speakerId];
          })} onDelete={() => update((copy) => { copy.groups.splice(index, 1); })} />)}
          {config.groups.length === 0 && <p class="rounded-xl border border-dashed border-slate-700 p-5 text-sm text-slate-400">No groups yet.</p>}
        </div>
      </section>

      {message && <p class="rounded-lg border border-cyan-900 bg-cyan-950/60 px-3 py-2 text-sm text-cyan-100">{message}</p>}
      <button class={`${buttonClass} bg-emerald-400 text-slate-950 hover:bg-emerald-300 disabled:opacity-50`} disabled={saving} type="submit">{saving ? "Saving…" : "Save and restart"}</button>
    </form>
  </main>;
}

function SpeakerCard({ speaker, onChange, onPreviewVolume, onCommitVolume }) {
  const connected = Boolean(speaker.connected);
  const volume = Number(speaker.volume ?? 100);
  return <article class={cardClass}>
    <div class="mb-5 flex items-start justify-between gap-4">
      <div class="min-w-0">
        <h3 class="truncate text-lg font-bold">{speaker.airplay_name || speaker.id}</h3>
        <p class="mt-1 text-sm text-slate-400">Individual speaker</p>
      </div>
      <span class={`shrink-0 rounded-full px-2.5 py-1 text-xs font-semibold ${connected ? "bg-emerald-400/15 text-emerald-300" : "bg-slate-800 text-slate-400"}`}>{connected ? "Connected" : "Offline"}</span>
    </div>

    <div class="grid gap-4 sm:grid-cols-2">
      <Text label="AirPlay name" value={speaker.airplay_name} onInput={(value) => onChange("airplay_name", value)} />
      <NumberField label="Group offset (ms)" value={speaker.delay_ms ?? 0} min="-500" max="500" onInput={(value) => onChange("delay_ms", value)} />
    </div>

    <div class="mt-5 border-t border-slate-800 pt-4">
      <div class="flex items-baseline justify-between gap-4">
        <p class="text-sm font-semibold">Volume</p>
        <output class="rounded-md bg-slate-800 px-2.5 py-1 text-sm font-bold text-cyan-300">{volume}%</output>
      </div>
      <input class="volume-slider mt-4" type="range" min="0" max="100" value={volume} disabled={!connected} onInput={(event) => onPreviewVolume(Number(event.currentTarget.value))} onChange={(event) => onCommitVolume(Number(event.currentTarget.value))} />
      <div class="mt-1 flex justify-between text-xs text-slate-500"><span>0</span><span>100</span></div>
    </div>

    <Toggle checked={!speaker.hidden} onChange={(visible) => onChange("hidden", !visible)}>Show as an individual AirPlay speaker</Toggle>
  </article>;
}

function Toggle({ checked, onChange, children }) {
  return <label class="mt-5 flex cursor-pointer items-center gap-3 text-sm font-medium text-slate-200">
    <input class="peer sr-only" type="checkbox" checked={checked} onChange={(event) => onChange(event.currentTarget.checked)} />
    <span aria-hidden="true" class="relative h-6 w-11 shrink-0 rounded-full bg-slate-700 transition after:absolute after:left-0.5 after:top-0.5 after:h-5 after:w-5 after:rounded-full after:bg-white after:shadow after:transition-transform peer-checked:bg-cyan-500 peer-checked:after:translate-x-5 peer-focus-visible:ring-2 peer-focus-visible:ring-cyan-400 peer-focus-visible:ring-offset-2 peer-focus-visible:ring-offset-slate-900" />
    <span>{children}</span>
  </label>;
}

function GroupCard({ group, speakers, onChange, onToggle, onDelete }) {
  return <article class={cardClass}>
    <div class="mb-4 flex items-center justify-between gap-4">
      <div><h3 class="font-bold">{group.airplay_name || group.id}</h3><p class="mt-1 text-sm text-slate-400">Shared AirPlay destination</p></div>
      <button class="text-sm font-semibold text-rose-300 hover:text-rose-200" type="button" onClick={onDelete}>Remove</button>
    </div>
    <Text label="AirPlay name" value={group.airplay_name} onInput={(value) => onChange("airplay_name", value)} />
    <fieldset class="mt-5">
      <legend class="text-xs font-medium text-slate-300">Included speakers</legend>
      <div class="mt-2 flex flex-wrap gap-2">{speakers.map((speaker) => {
        const included = group.speaker_ids.includes(speaker.id);
        return <button class={`flex items-center gap-2 rounded-lg border px-3 py-2 text-sm font-medium transition ${included ? "border-cyan-400/70 bg-cyan-400/10 text-cyan-200" : "border-slate-700 text-slate-400 hover:border-slate-600 hover:text-slate-200"}`} type="button" aria-pressed={included} key={speaker.id} onClick={() => onToggle(speaker.id)}><span class={`grid h-4 w-4 place-items-center rounded-full text-xs ${included ? "bg-cyan-400 text-slate-950" : "bg-slate-800 text-slate-500"}`}>{included ? "✓" : "+"}</span>{speaker.airplay_name || speaker.id}</button>;
      })}</div>
    </fieldset>
  </article>;
}

function Text({ label, value, onInput }) {
  return <label>{label}<input class={inputClass} value={value ?? ""} onInput={(event) => onInput(event.currentTarget.value)} /></label>;
}

function NumberField({ label, value, onInput, min, max }) {
  return <label>{label}<input class={inputClass} type="number" min={min} max={max} value={value ?? ""} onInput={(event) => onInput(event.currentTarget.value)} /></label>;
}

render(<App />, document.getElementById("app"));
