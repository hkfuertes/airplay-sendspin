import { render } from "preact";
import { useEffect, useState } from "preact/hooks";
import "./style.css";

// ponytail: internal-only UI; replace this before exposing it outside the trusted LAN.
const CONFIG_TOKEN = "airplay-sendspin";
const inputClass = "mt-1";
const buttonClass = "rounded-md px-3 py-2 text-sm font-semibold transition";

function blankGroup() {
  const id = `group-${crypto.randomUUID()}`;
  return { id, airplay_name: "Nuevo grupo", port: 0, speaker_ids: [] };
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
      setMessage("Guardado. Reiniciando el bridge…");
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
        setMessage("Configuración aplicada.");
        setSaving(false);
        return;
      } catch {
        await new Promise((resolve) => setTimeout(resolve, 500));
      }
    }
    setMessage("Guardado, pero el bridge tarda en volver. Recarga esta página.");
    setSaving(false);
  };

  if (!config) return <main class="mx-auto max-w-5xl p-8 text-slate-300">Cargando configuración… {message}</main>;

  return <main class="mx-auto max-w-5xl p-4 pb-16 sm:p-8">
    <header class="mb-8 flex flex-wrap items-end justify-between gap-4">
      <div>
        <p class="text-sm font-semibold tracking-wide text-cyan-300">AIRPLAY SENDSPIN</p>
        <h1 class="text-3xl font-bold">Configuración</h1>
        <p class="mt-1 text-sm text-slate-400">{config.speakers.length} altavoces detectados. Guardar reinicia el bridge y detiene el audio activo.</p>
      </div>
      <button class={`${buttonClass} bg-slate-800 hover:bg-slate-700`} type="button" onClick={load}>Recargar</button>
    </header>

    <form onSubmit={save} class="space-y-8">
      <section class="rounded-xl border border-slate-800 bg-slate-900/60 p-5">
        <Text label="Sufijo AirPlay" value={config.airplay_suffix} onInput={(value) => update((copy) => { copy.airplay_suffix = value; })} />
      </section>

      <section>
        <div class="mb-3">
          <h2 class="text-xl font-bold">Altavoces</h2>
          <p class="mt-1 text-sm text-slate-400">Los altavoces se descubren automáticamente. Aquí sólo ajustas cómo se anuncian y se mezclan.</p>
        </div>
        <div class="space-y-4">
          {config.speakers.map((speaker, index) => <SpeakerCard key={speaker.id} speaker={speaker} onChange={(field, value) => update((copy) => { copy.speakers[index][field] = value; })} />)}
        </div>
      </section>

      <section>
        <div class="mb-3 flex items-center justify-between gap-4">
          <div>
            <h2 class="text-xl font-bold">Grupos</h2>
            <p class="mt-1 text-sm text-slate-400">Un grupo aparece como un único destino AirPlay.</p>
          </div>
          <button class={`${buttonClass} shrink-0 bg-cyan-500 text-slate-950 hover:bg-cyan-400`} type="button" onClick={() => update((copy) => copy.groups.push(blankGroup()))}>Añadir grupo</button>
        </div>
        <div class="space-y-4">
          {config.groups.map((group, index) => <GroupCard key={group.id} group={group} speakers={config.speakers} onChange={(field, value) => update((copy) => { copy.groups[index][field] = value; })} onToggle={(speakerId) => update((copy) => {
            const members = copy.groups[index].speaker_ids;
            copy.groups[index].speaker_ids = members.includes(speakerId) ? members.filter((id) => id !== speakerId) : [...members, speakerId];
          })} onDelete={() => update((copy) => { copy.groups.splice(index, 1); })} />)}
        </div>
      </section>

      {message && <p class="rounded-md border border-cyan-900 bg-cyan-950/60 px-3 py-2 text-sm text-cyan-100">{message}</p>}
      <button class={`${buttonClass} bg-emerald-400 text-slate-950 hover:bg-emerald-300 disabled:opacity-50`} disabled={saving} type="submit">{saving ? "Guardando…" : "Guardar y reiniciar"}</button>
    </form>
  </main>;
}

function SpeakerCard({ speaker, onChange }) {
  return <article class="rounded-xl border border-slate-800 bg-slate-900/60 p-5">
    <div class="mb-4">
      <h3 class="font-bold">{speaker.airplay_name || speaker.id}</h3>
      <p class="mt-1 text-sm text-slate-400">Altavoz descubierto</p>
    </div>
    <div class="grid gap-3 sm:grid-cols-2">
      <Text label="Nombre AirPlay" value={speaker.airplay_name} onInput={(value) => onChange("airplay_name", value)} />
      <NumberField label="Ajuste en grupos (ms)" value={speaker.delay_ms ?? 0} min="-500" max="500" onInput={(value) => onChange("delay_ms", value)} />
    </div>
    <label class="mt-4 flex items-center gap-2 text-sm"><input class="h-4 w-4" type="checkbox" checked={speaker.hidden} onChange={(event) => onChange("hidden", event.currentTarget.checked)} />Usar sólo en grupos</label>
  </article>;
}

function GroupCard({ group, speakers, onChange, onToggle, onDelete }) {
  return <article class="rounded-xl border border-slate-800 bg-slate-900/60 p-5">
    <div class="mb-4 flex items-center justify-between gap-4">
      <h3 class="font-bold">{group.airplay_name || group.id}</h3>
      <button class="text-sm text-rose-300 hover:text-rose-200" type="button" onClick={onDelete}>Eliminar</button>
    </div>
    <Text label="Nombre AirPlay" value={group.airplay_name} onInput={(value) => onChange("airplay_name", value)} />
    <fieldset class="mt-4">
      <legend class="text-xs font-medium text-slate-300">Altavoces</legend>
      <div class="mt-2 flex flex-wrap gap-2">{speakers.map((speaker) => <label class="flex items-center gap-2 rounded-md border border-slate-700 px-3 py-2 text-sm" key={speaker.id}><input class="h-4 w-4" type="checkbox" checked={group.speaker_ids.includes(speaker.id)} onChange={() => onToggle(speaker.id)} />{speaker.airplay_name || speaker.id}</label>)}</div>
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
