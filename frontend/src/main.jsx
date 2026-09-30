import { render } from "preact";
import { useEffect, useState } from "preact/hooks";
import "./style.css";

const inputClass = "mt-1";
const buttonClass = "rounded-md px-3 py-2 text-sm font-semibold transition";

function initialToken() {
  const url = new URL(window.location.href);
  const token = url.searchParams.get("token") || localStorage.getItem("airplay-sendspin-token") || "";
  if (url.searchParams.has("token")) {
    localStorage.setItem("airplay-sendspin-token", token);
    url.searchParams.delete("token");
    history.replaceState({}, "", url);
  }
  return token;
}

function blankSpeaker() {
  const id = `speaker-${Date.now().toString(36)}`;
  return {
    id,
    airplay_name: id,
    direction: "outbound",
    port: 0,
    client_id: "",
    hidden: false,
    delay_ms: 0,
    endpoint: { instance: "", host: "", port: 0, path: "/sendspin" },
  };
}

function blankGroup() {
  const id = `group-${Date.now().toString(36)}`;
  return { id, airplay_name: id, port: 0, speaker_ids: [] };
}

function integers(config) {
  const copy = structuredClone(config);
  const number = (value) => (value === "" ? 0 : Number(value));
  copy.speakers.forEach((speaker) => {
    speaker.port = number(speaker.port);
    speaker.delay_ms = number(speaker.delay_ms);
    speaker.endpoint.port = number(speaker.endpoint.port);
  });
  copy.groups.forEach((group) => {
    group.port = number(group.port);
  });
  return copy;
}

function App() {
  const [token, setToken] = useState(initialToken);
  const [tokenInput, setTokenInput] = useState("");
  const [config, setConfig] = useState(null);
  const [message, setMessage] = useState("");
  const [saving, setSaving] = useState(false);

  const api = async (path, options = {}) => {
    const response = await fetch(path, {
      ...options,
      headers: { "X-Config-Token": token, ...(options.headers || {}) },
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
    return data;
  };

  const load = async () => {
    try {
      setConfig((await api("/api/config")));
      setMessage("");
    } catch (error) {
      setMessage(error.message);
    }
  };

  useEffect(() => {
    if (token) load();
  }, [token]);

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

  if (!token) {
    return <TokenGate value={tokenInput} onInput={setTokenInput} onSubmit={() => {
      localStorage.setItem("airplay-sendspin-token", tokenInput.trim());
      setToken(tokenInput.trim());
    }} />;
  }
  if (!config) return <main class="mx-auto max-w-5xl p-8 text-slate-300">Cargando configuración… {message}</main>;

  return <main class="mx-auto max-w-5xl p-4 pb-16 sm:p-8">
    <header class="mb-8 flex flex-wrap items-end justify-between gap-4">
      <div>
        <p class="text-sm font-semibold tracking-wide text-cyan-300">AIRPLAY SENDSPIN</p>
        <h1 class="text-3xl font-bold">Configuración</h1>
        <p class="mt-1 text-sm text-slate-400">Guardar reinicia el bridge; el audio activo se detendrá.</p>
      </div>
      <button class={`${buttonClass} bg-slate-800 hover:bg-slate-700`} type="button" onClick={load}>Recargar</button>
    </header>

    <form onSubmit={save} class="space-y-8">
      <section class="rounded-xl border border-slate-800 bg-slate-900/60 p-5">
        <Text label="Sufijo AirPlay" value={config.airplay_suffix} onInput={(value) => update((copy) => { copy.airplay_suffix = value; })} />
      </section>

      <section>
        <div class="mb-3 flex items-center justify-between">
          <h2 class="text-xl font-bold">Altavoces</h2>
          <button class={`${buttonClass} bg-cyan-500 text-slate-950 hover:bg-cyan-400`} type="button" onClick={() => update((copy) => copy.speakers.push(blankSpeaker()))}>Añadir altavoz</button>
        </div>
        <div class="space-y-4">
          {config.speakers.map((speaker, index) => <SpeakerCard key={speaker.id} speaker={speaker} onChange={(field, value) => update((copy) => { copy.speakers[index][field] = value; })} onEndpoint={(field, value) => update((copy) => { copy.speakers[index].endpoint[field] = value; })} onDelete={() => update((copy) => {
            const [removed] = copy.speakers.splice(index, 1);
            copy.groups.forEach((group) => { group.speaker_ids = group.speaker_ids.filter((id) => id !== removed.id); });
          })} />)}
        </div>
      </section>

      <section>
        <div class="mb-3 flex items-center justify-between">
          <h2 class="text-xl font-bold">Grupos</h2>
          <button class={`${buttonClass} bg-cyan-500 text-slate-950 hover:bg-cyan-400`} type="button" onClick={() => update((copy) => copy.groups.push(blankGroup()))}>Añadir grupo</button>
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

function TokenGate({ value, onInput, onSubmit }) {
  return <main class="mx-auto mt-24 max-w-md rounded-xl border border-slate-800 bg-slate-900 p-6">
    <p class="text-sm font-semibold text-cyan-300">AIRPLAY SENDSPIN</p>
    <h1 class="mt-1 text-2xl font-bold">Token de configuración</h1>
    <p class="mt-2 text-sm text-slate-400">Abre la URL mostrada al arrancar el bridge, o pega aquí su token.</p>
    <form class="mt-5 space-y-3" onSubmit={(event) => { event.preventDefault(); onSubmit(); }}>
      <input value={value} onInput={(event) => onInput(event.currentTarget.value)} placeholder="Token" />
      <button class={`${buttonClass} w-full bg-cyan-500 text-slate-950 hover:bg-cyan-400`}>Abrir</button>
    </form>
  </main>;
}

function SpeakerCard({ speaker, onChange, onEndpoint, onDelete }) {
  return <article class="rounded-xl border border-slate-800 bg-slate-900/60 p-5">
    <div class="mb-4 flex items-center justify-between gap-4">
      <h3 class="font-bold">{speaker.airplay_name || speaker.id}</h3>
      <button class="text-sm text-rose-300 hover:text-rose-200" type="button" onClick={onDelete}>Eliminar</button>
    </div>
    <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
      <Text label="ID" value={speaker.id} onInput={(value) => onChange("id", value)} />
      <Text label="Nombre AirPlay" value={speaker.airplay_name} onInput={(value) => onChange("airplay_name", value)} />
      <Select label="Dirección" value={speaker.direction} onChange={(value) => onChange("direction", value)} options={[["outbound", "Descubierto"], ["inbound", "Entrante"]]} />
      <NumberField label="Puerto AirPlay (0 = automático)" value={speaker.port} onInput={(value) => onChange("port", value)} />
      <NumberField label="Offset de grupo (ms)" value={speaker.delay_ms ?? 0} min="-500" max="500" onInput={(value) => onChange("delay_ms", value)} />
      <Text label="ID Sendspin" value={speaker.client_id} onInput={(value) => onChange("client_id", value)} />
      <Text label="Host Sendspin" value={speaker.endpoint.host} onInput={(value) => onEndpoint("host", value)} />
      <NumberField label="Puerto Sendspin" value={speaker.endpoint.port} onInput={(value) => onEndpoint("port", value)} />
      <Text label="Ruta Sendspin" value={speaker.endpoint.path} onInput={(value) => onEndpoint("path", value)} />
      <Text label="Instancia mDNS" value={speaker.endpoint.instance} onInput={(value) => onEndpoint("instance", value)} />
    </div>
    <label class="mt-4 flex items-center gap-2 text-sm"><input class="h-4 w-4" type="checkbox" checked={speaker.hidden} onChange={(event) => onChange("hidden", event.currentTarget.checked)} />Ocultar target individual de AirPlay</label>
  </article>;
}

function GroupCard({ group, speakers, onChange, onToggle, onDelete }) {
  return <article class="rounded-xl border border-slate-800 bg-slate-900/60 p-5">
    <div class="mb-4 flex items-center justify-between gap-4"><h3 class="font-bold">{group.airplay_name || group.id}</h3><button class="text-sm text-rose-300 hover:text-rose-200" type="button" onClick={onDelete}>Eliminar</button></div>
    <div class="grid gap-3 sm:grid-cols-3"><Text label="ID" value={group.id} onInput={(value) => onChange("id", value)} /><Text label="Nombre AirPlay" value={group.airplay_name} onInput={(value) => onChange("airplay_name", value)} /><NumberField label="Puerto AirPlay (0 = automático)" value={group.port} onInput={(value) => onChange("port", value)} /></div>
    <fieldset class="mt-4"><legend class="text-xs font-medium text-slate-300">Miembros</legend><div class="mt-2 flex flex-wrap gap-2">{speakers.map((speaker) => <label class="flex items-center gap-2 rounded-md border border-slate-700 px-3 py-2 text-sm" key={speaker.id}><input class="h-4 w-4" type="checkbox" checked={group.speaker_ids.includes(speaker.id)} onChange={() => onToggle(speaker.id)} />{speaker.airplay_name || speaker.id}</label>)}</div></fieldset>
  </article>;
}

function Text({ label, value, onInput }) {
  return <label>{label}<input class={inputClass} value={value ?? ""} onInput={(event) => onInput(event.currentTarget.value)} /></label>;
}

function NumberField({ label, value, onInput, min, max }) {
  return <label>{label}<input class={inputClass} type="number" min={min} max={max} value={value ?? ""} onInput={(event) => onInput(event.currentTarget.value)} /></label>;
}

function Select({ label, value, onChange, options }) {
  return <label>{label}<select class={inputClass} value={value} onChange={(event) => onChange(event.currentTarget.value)}>{options.map(([key, name]) => <option value={key}>{name}</option>)}</select></label>;
}

render(<App />, document.getElementById("app"));
