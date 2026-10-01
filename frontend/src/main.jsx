import { render } from "preact";
import { useEffect, useState } from "preact/hooks";
import "./style.css";

// ponytail: internal-only UI; replace this before exposing it outside the trusted LAN.
const CONFIG_TOKEN = "airplay-sendspin";

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

  return <div class="page-shell">
    <header class="topbar">
      <div class="brand">
        <span class="brand-mark" aria-hidden="true"><span /><span /><span /><span /></span>
        <span>Sendspin<span class="brand-light"> Bridge</span></span>
      </div>
      <label class="suffix-field" title="Added to every speaker and group name">
        <span>Suffix</span>
        <input type="text" form="config-form" placeholder="Suffix" value={config?.airplay_suffix ?? ""} disabled={!config || saving} onInput={(event) => { const value = event.currentTarget.value; update((copy) => { copy.airplay_suffix = value; }); }} />
      </label>
      <div class="header-actions">
        <p class="header-message" role="status" aria-live="polite">{config ? message : ""}</p>
        <span class="save-warning" id="save-warning">Stops playback</span>
        <button class="button button-quiet" type="button" disabled={saving} onClick={load} title="Reload discards unsaved changes">↻ <span>Reload</span></button>
        <button class="button button-primary" type="submit" form="config-form" disabled={!config || saving} aria-describedby="save-warning">{saving ? "Saving…" : "Save & restart"}</button>
      </div>
    </header>

    <main>
      <h1 class="sr-only">Speakers & groups</h1>
      {!config ? <div class="empty-state" role="status">
        <p>{message || "Loading configuration…"}</p>
        {message && <button class="button button-primary" type="button" onClick={load}>Try again</button>}
      </div> : <form id="config-form" onSubmit={save}>
        <section class="collection" aria-labelledby="speakers-title">
          <div class="section-heading">
            <div><h2 id="speakers-title">Speakers <span>{config.speakers.length}</span></h2><p>Discovered automatically · Adjust volume directly or expand settings to edit.</p></div>
          </div>
          <div class="device-list">
            {config.speakers.map((speaker, index) => <SpeakerCard key={speaker.id} speaker={speaker} onChange={(field, value) => update((copy) => { copy.speakers[index][field] = value; })} onPreviewVolume={(volume) => update((copy) => { copy.speakers[index].volume = volume; })} onCommitVolume={(volume) => setVolume(speaker.id, volume)} />)}
            {config.speakers.length === 0 && <p class="empty-state">No speakers discovered yet. They will appear here when they connect.</p>}
          </div>
        </section>

        <section class="collection" aria-labelledby="groups-title">
          <div class="section-heading">
            <div><h2 id="groups-title">Groups <span>{config.groups.length}</span></h2><p>One AirPlay destination for multiple speakers.</p></div>
            <button class="button button-secondary" type="button" onClick={() => { const group = blankGroup(); setNewGroupId(group.id); update((copy) => copy.groups.push(group)); }}>+ Add group</button>
          </div>
          <div class="device-list">
            {config.groups.map((group, index) => <GroupCard key={group.id} group={group} speakers={config.speakers} startOpen={group.id === newGroupId} onChange={(field, value) => update((copy) => { copy.groups[index][field] = value; })} onToggle={(speakerId) => update((copy) => {
              const members = copy.groups[index].speaker_ids;
              copy.groups[index].speaker_ids = members.includes(speakerId) ? members.filter((id) => id !== speakerId) : [...members, speakerId];
            })} onDelete={() => update((copy) => { copy.groups.splice(index, 1); })} />)}
            {config.groups.length === 0 && <p class="empty-state">No groups yet. Add one to play across rooms.</p>}
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
  return <article class="device-card speaker-card">
    <div class="speaker-row">
      <div class="speaker-identity">
        <span class={`connection ${connected ? "is-online" : ""}`}><span class="connection-dot" />{connected ? "Online" : "Offline"}</span>
        <span class="device-name"><strong>{speaker.airplay_name || speaker.id}</strong><small>{speaker.hidden ? "AirPlay hidden" : "AirPlay visible"} · {speaker.id}</small></span>
      </div>
      <div class="speaker-controls">
        <div class="volume-area">
          <label for={volumeId}>Volume <output for={volumeId}>{volume}%</output></label>
          <input id={volumeId} type="range" min="0" max="100" value={volume} disabled={!connected} onInput={(event) => onPreviewVolume(Number(event.currentTarget.value))} onChange={(event) => onCommitVolume(Number(event.currentTarget.value))} />
        </div>
        <Toggle checked={!speaker.hidden} onChange={(visible) => onChange("hidden", !visible)}>AirPlay</Toggle>
      </div>
    </div>
    <details class="speaker-settings">
      <summary>Settings <span class="chevron" aria-hidden="true" /></summary>
      <div class="device-body"><div class="field-grid">
        <Text label="AirPlay name" value={speaker.airplay_name} onInput={(value) => onChange("airplay_name", value)} />
        <NumberField label="Group offset (ms)" value={speaker.delay_ms ?? 0} min="-500" max="500" onInput={(value) => onChange("delay_ms", value)} />
      </div><p class="field-hint">Group offset: −500 to 500 ms.</p></div>
    </details>
  </article>;
}

function Toggle({ checked, onChange, children }) {
  return <label class="visibility-toggle">
    <input class="peer sr-only" type="checkbox" checked={checked} onChange={(event) => onChange(event.currentTarget.checked)} />
    <span aria-hidden="true" class="toggle-track" />
    <span>{children}</span>
  </label>;
}

function GroupCard({ group, speakers, startOpen, onChange, onToggle, onDelete }) {
  const members = group.speaker_ids.map((id) => speakers.find((speaker) => speaker.id === id)?.airplay_name || id).join(", ");
  return <details class="device-card" open={startOpen}>
    <summary class="device-summary">
      <span class="device-name"><strong>{group.airplay_name || group.id}</strong><small>{members || "No speakers selected"}</small></span>
      <span class="member-count">{group.speaker_ids.length} {group.speaker_ids.length === 1 ? "speaker" : "speakers"}</span>
      <span class="chevron" aria-hidden="true" />
    </summary>
    <div class="device-body">
      <Text label="AirPlay name" value={group.airplay_name} onInput={(value) => onChange("airplay_name", value)} />
      <fieldset class="member-field"><legend>Included speakers</legend>
        <div class="member-list">{speakers.map((speaker) => {
          const included = group.speaker_ids.includes(speaker.id);
          return <button class={`member-chip ${included ? "is-selected" : ""}`} type="button" aria-pressed={included} key={speaker.id} onClick={() => onToggle(speaker.id)}><span aria-hidden="true">{included ? "✓" : "+"}</span>{speaker.airplay_name || speaker.id}</button>;
        })}</div>
        {speakers.length === 0 && <p class="field-hint">No speakers discovered yet.</p>}
      </fieldset>
      <div class="group-actions"><button class="remove-button" type="button" onClick={onDelete}>Remove group</button></div>
    </div>
  </details>;
}

function Text({ label, value, onInput }) {
  return <label class="field">{label}<input value={value ?? ""} onInput={(event) => onInput(event.currentTarget.value)} /></label>;
}

function NumberField({ label, value, onInput, min, max }) {
  return <label class="field">{label}<input type="number" min={min} max={max} value={value ?? ""} onInput={(event) => onInput(event.currentTarget.value)} /></label>;
}

render(<App />, document.getElementById("app"));
