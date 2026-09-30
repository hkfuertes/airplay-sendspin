# Spec: puente AirPlay 1 → Sendspin (Python)

## Objetivo

Un destino AirPlay 1 por altavoz Sendspin descubierto y otro por cada grupo
configurado. Sin interfaz web: descubrimiento mDNS y `config.xml` persistente.
No soporta AirPlay 2 ni parejas estéreo.

## Runtime

```text
iPhone ─RAOP─> libraop (PCM S16LE estéreo, 44.1 kHz)
              └─> extensión CFFI / buffer nativo acotado
                    └─> scheduler Python, rejilla de 20 ms
                          └─> aiosendspin PushStream ─WS─> altavoces
```

- `libraop` sigue siendo el único receptor/decoder RAOP. El parche entrega PCM
  ordenado directamente; Python no implementa RAOP.
- La extensión CFFI copia PCM en un anillo nativo de dos segundos y expone
  eventos PLAY/FLUSH/STOP/volumen. Así no hay callbacks Python en el hilo de
  audio de libraop.
- `aiosendspin==9.1.1` es la implementación oficial de Sendspin. Un único
  `SendspinServer` anuncia `_sendspin-server._tcp`, descubre `_sendspin._tcp`
  y mantiene las conexiones de entrada y salida.
- El scheduler genera chunks de 20 ms. Cuando hay varios altavoces activos les
  da el mismo `play_start_us`; si no llega PCM a tiempo, rellena con silencio.

## Configuración

`config.xml` se crea y actualiza atómicamente. Los puertos se asignan en bloques
de diez desde 7000.

```xml
<airplay-sendspin version="1" airplay_suffix=" (Sendspin)">
  <speakers>
    <speaker id="cocina" client_id="…" airplay_name="Cocina"
             direction="outbound" port="7000" hidden="false" delay_ms="0">
      <endpoint instance="…" host="192.168.1.50" port="8928" path="/sendspin"/>
    </speaker>
  </speakers>
  <groups>
    <group id="casa" airplay_name="Toda la casa" port="7020">
      <speaker id="cocina"/>
    </group>
  </groups>
</airplay-sendspin>
```

- `outbound`: el bridge descubre o marca al reproductor Sendspin.
- `inbound`: el reproductor descubre y marca al bridge. `client_id` es la
  identidad que evita que un altavoz use ambos sentidos.
- `hidden="true"` no anuncia el target individual; sigue reproduciendo grupos.
- `delay_ms` está incluido en `[-500, 500]` y afecta sólo al audio de grupos:
  positivo lo retiene, negativo lo adelanta. El primer hello materializa `0`.

## Grupos y volumen

Cada `<group>` tiene su propio receptor AirPlay. Su PCM se guarda por índice de
chunk; cada miembro mezcla la misma copia con su entrada individual usando
saturación S16. El offset de cada miembro se aplica al índice de muestras antes
de mezclar. El volumen del grupo mueve la media de los miembros sin borrar su
diferencia, limitado a 0–100.

No se usan grupos internos de `aiosendspin`: un `PushStream` nativo no mezcla
entradas concurrentes y sustituiría el target individual. La mezcla se hace
antes del stream para conservar audio individual + grupos y `delay_ms` firmado.

## Build y add-on

Docker clona la revisión fijada de libraop, aplica `patches/libraop/`, compila
la extensión CFFI y empaqueta Python 3.13 con `aiosendspin`. La etapa `runtime`
usa `/data/config.xml`; `addon` usa `/config/config.xml` y host networking.

El workflow del add-on publica imágenes amd64 y aarch64 al cambiar
`airplay-sendspin/config.yaml` en `main`.

## Comprobaciones mínimas

- CFFI abre y cierra un receptor RAOP real.
- XML conserva IDs, direcciones y `delay_ms` firmado.
- Un grupo retiene con delay positivo, adelanta con negativo y satura la mezcla.
- El build Docker ejecuta los tests Python y carga la extensión nativa.
