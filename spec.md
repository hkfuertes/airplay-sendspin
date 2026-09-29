# Spec: puente AirPlay 1 → Sendspin (Go)

Documento vivo. Recoge qué hace el bridge, por qué, y lo aprendido de la
implementación de referencia (aiosendspin). Estado: grupos multiroom
implementados en `feat/groups`, pendientes de prueba audible.

## 1. Objetivo y principios

- Un destino AirPlay 1 por cada altavoz Sendspin descubierto, más un destino
  por cada grupo configurado. Sin frontend: descubrimiento mDNS y un XML.
- Binario Go estático en Docker (host network). Nada de re-streaming RTSP/HTTP:
  libraop decodifica y entrega PCM directo al bridge.
- Fork mínimo y reproducible: upstream fijado en `dependencies.lock` + series
  de parches ordenadas por funcionalidad (`patches/libraop`, `patches/sendspin`).
- Fuera de alcance por ahora: AirPlay 2, parejas estéreo, UI.

## 2. Arquitectura

### 2.1 Flujo de audio (altavoz individual)

```
iPhone ─RAOP─▶ libraop (parche PCM directo, S16LE estéreo 44.1 kHz)
            ─▶ pcm.Pipeline: [libsoxr → formato del altavoz] → FIFO acotada (1 s)
            ─▶ sendspin.Server (tick 20 ms, timestamp por chunk) ─WS─▶ altavoz
```

- La FIFO descarta lo más viejo si se llena y rellena con silencio si se vacía
  (el reloj Sendspin nunca se para). PLAY/FLUSH/STOP de RAOP la vacían.
- Volumen RAOP (0..1) → volumen del reproductor (0..100, redondeado).

### 2.2 Conexión Sendspin: una dirección por altavoz

- `outbound`: el bridge descubre `_sendspin._tcp` y marca; un `Server` propio
  por destino (`StartOutbound`), sin listener ni mDNS de servidor.
- `inbound`: el bridge anuncia `_sendspin-server._tcp` en `:8927`; un único
  `Server` compartido enruta cada cliente por `client_id` (tras `client/hello`)
  a la `Pipeline` de su destino (`SourceForClient`).
- Un altavoz sólo acepta una sesión (HTTP 409 si ya hay otra): nunca abrir dos.
- mDNS no trae `client_id`: la identidad fiable es la del `client/hello`.

### 2.3 Formato por altavoz (`selectFormat`)

Sólo estéreo; frecuencia divisible por 50 (chunks de 20 ms enteros); 16/24 bit.
Preferencia: PCM > FLAC > Opus (Opus sólo 48 kHz/16); dentro del códec,
44.1 kHz (sin remuestreo) > 48 kHz > otras. Miembros de grupo: preferencia
fuerte por el formato del grupo (§4). La `Pipeline` se configura en el hook
`OnPlayerHello`, antes de que el rol de reproductor negocie el códec.

### 2.4 Configuración (`state/config.xml`)

```xml
<airplay-sendspin version="1">
  <speakers>
    <speaker id="cocina" client_id="…" airplay_name="Cocina"
             direction="outbound" port="7000">
      <endpoint instance="…" host="…" port="8928" path="/sendspin"/>
    </speaker>
  </speakers>
  <groups>
    <group id="casa" airplay_name="Toda la casa" port="7020">
      <speaker id="cocina"/>
    </group>
  </groups>
</airplay-sendspin>
```

- `id`: clave estable y legible (slug); `client_id`: identidad Sendspin.
- Puertos: bloques de 10 desde 7000, repartidos entre altavoces y grupos.
- Grupos escritos a mano: id único, miembros existentes y sin repetir (si no,
  error al cargar). Cambios en el XML requieren reiniciar.
- Anuncio AirPlay: `airplay_name + " (Sendspin)"`; MAC virtual derivada de
  `id` (altavoces) o `group:id` (grupos).

### 2.5 Parches

| Serie | Parche | Qué hace |
|---|---|---|
| libraop | 0001 | librería estática embebible |
| libraop | 0002 | salida PCM directa ordenada (sin HTTP/encoder), callback fuera del lock |
| sendspin | 0001 | ciclo de vida outbound (`StartOutbound`, `KeepSourceOpen`) |
| sendspin | 0002 | negociación de códec segura por formato, bit depth, `SetVolume` |
| sendspin | 0003 | enrutado inbound por cliente, hooks de hello, sync gate, timeline continuo |
| sendspin | 0004 | reloj compartido por proceso, rejilla de 20 ms, `TimedAudioSource.ReadAt` |

## 3. Sincronía (modelo de tiempo)

- Timestamp de chunk = `timeline` continuo: avanza exactamente 20 ms por chunk
  (nunca "ahora + buffer" por tick: EchoLocal coloca cada chunk por su
  timestamp y el jitter producía huecos/solapes). Si el timeline queda atrás,
  se rebasa a `ahora + 500 ms`, alineado a la rejilla de 20 ms.
- Sync gate: no se envía audio a un cliente hasta su primer `client/time`
  + 100 ms (o 500 ms tras el hello si nunca sincroniza). Evita el anclaje
  absurdo del primer chunk en EchoLocal.
- 0004: todos los `Server` del proceso comparten época de reloj y rejilla, así
  que un mismo timestamp significa el mismo instante en todos. Es la base para
  sincronizar altavoces que van por servidores distintos.
- Lo que NO se compensa hoy: la latencia de salida propia de cada aparato
  (ver `output_delay`, §5.3).
- nqptp (PTP de shairport-sync) no ayuda aquí: sincroniza el emisor AirPlay 2
  con el receptor, no el bridge con los altavoces (eso ya lo hace el
  `client/time` de Sendspin, y los Echo no hablan PTP). Nuestro AirPlay 1 usa
  el timing propio de RAOP vía libraop. Sólo tendría sentido con AirPlay 2
  (multiselección nativa en el iPhone), que está fuera de alcance, y exige
  otro daemon con UDP 319/320 exclusivos.
- Capas de la sincronía: (1) reloj común: intercambio tipo NTP + filtro
  (Sendspin: `client/time` + Kalman 2D de desfase y deriva; también Snapcast,
  RAOP) o PTP (AirPlay 2, AES67/Dante, AVB). PTP sólo gana claramente con
  timestamps hardware y switches PTP en cable; por Wi-Fi con timestamps
  software ambos quedan en el mismo orden (manda el jitter de Wi-Fi).
  (2) reproducción: chunks con hora + corregir la deriva del DAC (meter/quitar
  frames como EchoLocal, o remuestreo adaptativo) + compensar la latencia del
  aparato (la spec se la asigna al cliente; `output_delay_ms` sólo cubre lo
  que va después del puerto).
- Nuestro error entre altavoces está en (2), no en (1): banda muerta de ±2 ms
  de EchoLocal (hasta ~4 ms entre dos aparatos) y latencia distinta por
  modelo. Referencia: 1 ms ≈ 34 cm de recorrido del sonido; multiroom tolera
  varios ms, una pareja estéreo pide ~1 ms.

## 4. Grupos multiroom (implementado, `feat/groups`)

- Cada `<group>` = un destino AirPlay más (su propio receptor libraop).
- `pcm.Group`: el audio del grupo va a una `Pipeline` en `GroupFormat`
  (48 kHz/16 bit, remuestreado una vez) y se sirve **por instante**: caché de
  16 chunks (320 ms) indexada por `playbackTime / 20 ms`. El primer miembro que
  pide un chunk lo lee de la FIFO; el resto recibe la misma copia, aunque vaya
  por otro `Server`. Petición muy adelantada (≥16) → salta; muy atrasada → nada.
- `pcm.Mix`: fuente Sendspin de cada altavoz = su propio destino + sus grupos,
  sumados con recorte a int16. Sólo mezcla si el altavoz está en `GroupFormat`;
  si no lo soporta, suena suelto pero no en el grupo (aviso en log).
- Políticas: los miembros siguen anunciados individualmente. AirPlay 1 = un
  emisor → un destino, así que desde un móvil gana el último elegido; con dos
  emisores a la vez el altavoz mezcla.
- Volumen de grupo = algoritmo de aiosendspin (§5.5): la media de los miembros
  conectados pasa al valor de AirPlay y cada uno se mueve lo mismo, así que se
  conservan las diferencias; lo recortado en 0/100 se reparte entre el resto.
  Se calcula sobre el volumen que reporta cada reproductor (`Clients()`).
  EchoLocal cuantiza a 30 pasos redondeando hacia abajo al recibir y al
  reportar, así que el volumen real puede quedar hasta ~3 puntos bajo el pedido
  (sin deriva: cada evento recalcula el delta). En 0 o 100 todos quedan
  iguales y las diferencias se pierden.
- Formato del grupo fijo (48/16) = limitación consciente (§6).

## 5. Referencia: aiosendspin (commit 83209af)

### 5.1 Modelo de stream
- Un `PushStream` por grupo. El productor hace `prepare_audio(pcm, fmt,
  channel)` + `commit_audio()`. Por canal hay un timeline
  (`_channel_timing[ch]` = inicio del próximo chunk) que avanza por número de
  muestras con residuo entero (`divmod(n·1e6 + residuo, rate)`): sin deriva ni
  con 44.1 kHz.
- Arranque en `ahora + max(send_ahead)` de los reproductores, con
  `send_ahead = min_buffer + output_delay` (fuente en vivo) o
  `max(min_buffer, required_lead_time) + output_delay` (fuente con buffer).
- Si la producción se atasca y el timeline queda por detrás, desplaza **todos**
  los canales lo mismo (siguen alineados entre sí). Un canal nuevo se une al
  timeline compartido, no empieza de cero.
- `commit_audio(play_start_us=…)` explícito: pensado para varios servidores con
  reloj compartido y timestamps idénticos (misma idea que nuestro 0004).
- `sleep_to_limit_buffer`: el productor se frena cuando va demasiado adelantado
  (contrapresión en origen, no descartar en la FIFO).

### 5.2 Conversión bajo demanda
- Reproductores agrupados por clave PCM (canal, rate, bits, canales): se
  remuestrea **una vez por formato distinto**, no por reproductor. Remuestreadores
  cacheados por `_ResamplerKey` y creados al aparecer un reproductor que los
  necesita; si el formato coincide, passthrough.
- Después se codifica una vez por `TransformKey` (códec, formato, duración de
  frame, opciones) y se reparte a todos los que la comparten; caché de chunks
  por clave para quien llega tarde.
- **Timestamps del audio convertido = tiempo de contenido.** Cada
  remuestreador lleva `pending_timestamp_us` (instante de la próxima muestra de
  salida) y lo avanza por muestras emitidas. La latencia del filtro (soxr
  precision=30) retrasa *cuándo* sale el audio, no la etiqueta: sale más tarde
  pero bien fechado, y todos los formatos quedan alineados al contenido.
  La deriva se detecta en el cursor de entrada (hueco > 20 ms → vaciar y
  reconstruir el filtro).
- Medido con nuestra libsoxr HQ 44.1→48 kHz: la primera llamada no emite nada,
  sale a ráfagas, y retiene ~700 frames (14.6 ms) tras 1 s.
- Consecuencia para Go: nuestra FIFO asigna timestamps por posición de lectura,
  no por contenido. Con un solo formato por grupo da igual; con varios
  formatos, cada variante debe fecharse por contenido (o generarse en lockstep
  desde el mismo chunk maestro con un chunk de adelanto).

### 5.3 `output_delay` (latencia por altavoz)
- El reproductor declara `output_delay_ms` en su estado; el servidor puede
  fijarlo con el comando `set_output_delay`. El servidor lo usa para el envío
  anticipado y para decidir si un chunk llega tarde (`timestamp − output_delay`);
  la compensación la hace el propio reproductor.
- Es el equivalente protocolario del `sync_delay_ms` de sendspin-server.
- EchoLocal 0.0.7 no lo implementa (su estado sólo lleva volumen y mute) y
  sendspin-go tampoco: un desfase fijo entre aparatos hay que corregirlo en el
  bridge.

### 5.4 Canales y estéreo
- La spec oficial (Sendspin/spec) no tiene parejas estéreo: `channels` es sólo
  el número (1 mono, 2 estéreo), no cuál; un grupo es miembros + volumen +
  mute + estado. Pero tampoco lo impide: "each client receives its own
  independently encoded stream", con reloj común, así que el servidor puede
  mandar L a un altavoz y R a otro con los mismos timestamps. El altavoz no
  sabe que es "el izquierdo".
- En aiosendspin, "channel" = canal de enrutado del stream (UUID,
  `MAIN_CHANNEL` por defecto), no L/R. `channel_resolver(player_id)` asigna
  canal a cada reproductor; cada canal tiene su PCM pero comparte timeline.
- El reparto L/R lo hace la aplicación (sendspin-server: `split_stereo_pcm` →
  canales LEFT/RIGHT; grupo estéreo = exactamente 2 miembros en orden
  izquierda, derecha; volumen = mínimo de los miembros).

### 5.5 Grupos
- Todo cliente está siempre en exactamente un grupo (por defecto, uno propio).
  `add_client` lo saca del anterior (el resto de ese grupo sigue sonando);
  `remove_client` le manda `stream/end` y lo pasa a un grupo propio. El nombre
  por defecto del grupo es el de su primer miembro; los clientes reciben
  `group/update` (id, nombre, estado).
- `start_stream(channel_resolver)` crea el único `PushStream` del grupo; si ya
  había uno (cambio de pista), lo sustituye sin cerrar el stream de protocolo.
  También reclama miembros desconectados (soporte multi-servidor).
- Volumen de grupo = **media** de los miembros. Al fijarlo se aplica a todos
  el mismo delta (objetivo − media), conservando las diferencias; lo que se
  pierde al recortar en 0/100 se reparte entre los que no han llegado al
  límite. Mute de grupo = todos en mute.

### 5.6 Late join (catch-up)
- El miembro se une en el acto (no diferido por reloj: eso desincroniza).
- Si otros ya usan su misma `TransformKey`, recibe de la caché los chunks ya
  codificados con timestamp ≥ `ahora + max(lead mínimo, su send_ahead)`: suena
  casi inmediatamente, en el timeline compartido. Un chunk que empezó antes del
  objetivo se salta, no se manda a medias.
- Si es el único con ese formato, se recodifica desde la caché de PCM crudo
  (tarea de catch-up) y luego pasa a directo.
- Nuestro bridge no hace catch-up: un miembro que llega tarde empieza en su
  timeline (`ahora + 500 ms`, más el sync gate). Aceptable para AirPlay.

## 6. Pendiente y decisiones abiertas

- [ ] Prueba audible del grupo "Toda la casa" (Dot outbound + Show inbound).
- [ ] Si hay desfase fijo entre aparatos: `delay_ms` por altavoz en el XML
      (EchoLocal no soporta `set_output_delay`); el miembro lee el chunk de
      grupo de `t − delay`, que cabe en la caché si es < 320 ms.
- [ ] Variantes por formato dentro del grupo (fechadas por contenido, §5.2);
      eliminaría `GroupFormat` fijo y la preferencia de formato. Esperar a que
      haya un altavoz que lo necesite.
- [ ] Parejas estéreo: `<speaker id="…" channel="left|right"/>` dentro del
      `<group>`; en `pcm.Mix` el miembro copia su canal del chunk de grupo a
      ambos lados de cada frame (los Echo sólo aceptan estéreo y suman a mono,
      así que (L,L) suena como L). Decidir volumen: conservar la diferencia
      (hace de balance) o forzar el mínimo como sendspin-server. Riesgo real: la
      sincronía fina; unos ms entre aparatos descentran la imagen (EchoLocal
      corrige a ±2 ms y no soporta `output_delay`) → `delay_ms` por altavoz.
- [ ] Opcional: alinear el ticker a la rejilla de 20 ms (hoy el envío puede
      saltar un tick; inocuo para EchoLocal).

## 7. Diagnóstico

- Logs del bridge: `docker compose logs bridge`.
- Echo Dot (EchoLocal 0.0.7), root por adb `192.168.77.229:5555`:
  `adb logcat -s echolocal`. `sendspin ahead`: `lead_ms` (~490–500 sano),
  `dropped` (0), `corrected` (frames corregidos, acumulado).
  `sendspin correction` es un informe por segundo del desfase instantáneo
  (`off_ms`, oscila ±10 ms por el buffer del hardware); EchoLocal sólo actúa
  sobre la media suavizada (`drift_ms`): nudge > 2 ms, snap > 10 ms,
  re-anclaje > 2 s.
- mDNS: `avahi-browse` no ve los anuncios; usar una consulta Go
  (`hashicorp/mdns`) con host network.
