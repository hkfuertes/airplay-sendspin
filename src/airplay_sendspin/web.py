"""Small authenticated configuration UI for the bridge."""

from __future__ import annotations

import asyncio
import json
import logging
import os
import secrets
import tempfile
from collections.abc import Callable
from pathlib import Path

from aiohttp import web

from .registry import Endpoint, Group, Registry, Speaker

LOG = logging.getLogger(__name__)
TOKEN_HEADER = "X-Config-Token"


class ConfigWeb:
    """Serve the compiled UI and atomically save a replacement registry."""

    def __init__(
        self,
        *,
        config_path: str,
        port_base: int,
        port_range: int,
        host: str,
        port: int,
        advertised_host: str,
        registry: Callable[[], Registry],
        replace_registry: Callable[[Registry], None],
        restart: Callable[[], None],
    ) -> None:
        self._config_path = config_path
        self._port_base = port_base
        self._port_range = port_range
        self._host = host
        self._port = port
        self._advertised_host = advertised_host
        self._registry = registry
        self._replace_registry = replace_registry
        self._restart = restart
        self._token = _load_token(Path(config_path).parent / ".config-web-token")
        self._runner: web.AppRunner | None = None
        self._save_lock = asyncio.Lock()
        self._restart_scheduled = False

    @property
    def url(self) -> str:
        return f"http://{self._advertised_host}:{self._port}/?token={self._token}"

    async def start(self) -> None:
        static_root = Path(__file__).with_name("web")
        if not (static_root / "index.html").is_file():
            raise RuntimeError("web UI assets are missing; rebuild the image")
        app = web.Application(middlewares=[self._authenticate])
        app.router.add_get("/", self._index)
        app.router.add_get("/api/health", self._health)
        app.router.add_get("/api/config", self._get_config)
        app.router.add_put("/api/config", self._put_config)
        app.router.add_static("/", static_root, show_index=False)
        self._runner = web.AppRunner(app)
        await self._runner.setup()
        try:
            await web.TCPSite(self._runner, self._host, self._port).start()
        except BaseException:
            await self._runner.cleanup()
            self._runner = None
            raise

    async def close(self) -> None:
        if self._runner is not None:
            await self._runner.cleanup()
            self._runner = None

    @web.middleware
    async def _authenticate(self, request: web.Request, handler):
        if request.path.startswith("/api/"):
            supplied = request.headers.get(TOKEN_HEADER, "")
            if not secrets.compare_digest(supplied, self._token):
                return web.json_response({"error": "configuration token required"}, status=401)
        return await handler(request)

    async def _index(self, _request: web.Request) -> web.FileResponse:
        return web.FileResponse(Path(__file__).with_name("web") / "index.html")

    async def _health(self, _request: web.Request) -> web.Response:
        return web.json_response({"ok": True})

    async def _get_config(self, _request: web.Request) -> web.Response:
        return web.json_response(registry_to_payload(self._registry()))

    async def _put_config(self, request: web.Request) -> web.Response:
        try:
            payload = await request.json()
            registry = registry_from_payload(payload, self._config_path, self._port_base, self._port_range)
            async with self._save_lock:
                registry.save()
                self._replace_registry(registry)
        except (ValueError, json.JSONDecodeError) as error:
            return web.json_response({"error": str(error)}, status=400)
        if not self._restart_scheduled:
            self._restart_scheduled = True
            asyncio.get_running_loop().call_later(0.1, self._restart)
        return web.json_response({"config": registry_to_payload(registry), "restarting": True})


def registry_to_payload(registry: Registry) -> dict:
    return {
        "airplay_suffix": registry.airplay_suffix,
        "speakers": [
            {
                "id": speaker.id,
                "airplay_name": speaker.airplay_name,
                "direction": speaker.direction,
                "port": speaker.port,
                "client_id": speaker.client_id,
                "hidden": speaker.hidden,
                "delay_ms": speaker.delay_ms,
                "endpoint": {
                    "instance": speaker.endpoint.instance,
                    "host": speaker.endpoint.host,
                    "port": speaker.endpoint.port,
                    "path": speaker.endpoint.path,
                },
            }
            for speaker in registry.speakers()
        ],
        "groups": [
            {
                "id": group.id,
                "airplay_name": group.airplay_name,
                "port": group.port,
                "speaker_ids": group.speaker_ids,
            }
            for group in registry.groups()
        ],
    }


def registry_from_payload(payload: object, path: str, port_base: int, port_range: int) -> Registry:
    if not isinstance(payload, dict):
        raise ValueError("configuration must be an object")
    speakers_data = _list(payload, "speakers")
    groups_data = _list(payload, "groups")
    speakers = [_speaker(item, index) for index, item in enumerate(speakers_data, 1)]
    groups = [_group(item, index) for index, item in enumerate(groups_data, 1)]
    registry = Registry(
        path,
        port_base,
        port_range,
        airplay_suffix=_text(payload, "airplay_suffix", strip=False),
        speakers=speakers,
        groups=groups,
    )
    registry._normalize()
    return registry


def _speaker(value: object, index: int) -> Speaker:
    data = _object(value, f"speaker {index}")
    endpoint = _object(data.get("endpoint", {}), f"speaker {index} endpoint")
    delay = data.get("delay_ms")
    if delay is not None:
        delay = _integer(delay, f"speaker {index} delay_ms", -500, 500)
    return Speaker(
        id=_required_text(data, "id", f"speaker {index}"),
        airplay_name=_text(data, "airplay_name"),
        direction=_text(data, "direction", "outbound"),
        port=_integer(data.get("port", 0), f"speaker {index} port", 0, 65535),
        client_id=_text(data, "client_id"),
        hidden=_boolean(data.get("hidden", False), f"speaker {index} hidden"),
        delay_ms=delay,
        endpoint=Endpoint(
            instance=_text(endpoint, "instance"),
            host=_text(endpoint, "host"),
            port=_integer(endpoint.get("port", 0), f"speaker {index} endpoint port", 0, 65535),
            path=_text(endpoint, "path", "/sendspin"),
        ),
    )


def _group(value: object, index: int) -> Group:
    data = _object(value, f"group {index}")
    members = _list(data, "speaker_ids")
    if not all(isinstance(member, str) for member in members):
        raise ValueError(f"group {index} speaker_ids must contain strings")
    return Group(
        id=_required_text(data, "id", f"group {index}"),
        airplay_name=_text(data, "airplay_name"),
        port=_integer(data.get("port", 0), f"group {index} port", 0, 65535),
        speaker_ids=members,
    )


def _object(value: object, label: str) -> dict:
    if not isinstance(value, dict):
        raise ValueError(f"{label} must be an object")
    return value


def _list(data: dict, key: str) -> list:
    value = data.get(key, [])
    if not isinstance(value, list):
        raise ValueError(f"{key} must be an array")
    return value


def _text(data: dict, key: str, default: str = "", *, strip: bool = True) -> str:
    value = data.get(key, default)
    if not isinstance(value, str):
        raise ValueError(f"{key} must be a string")
    return value.strip() if strip else value


def _required_text(data: dict, key: str, label: str) -> str:
    value = _text(data, key)
    if not value:
        raise ValueError(f"{label} needs {key}")
    return value


def _integer(value: object, label: str, minimum: int, maximum: int) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or not minimum <= value <= maximum:
        raise ValueError(f"{label} must be an integer in {minimum}..{maximum}")
    return value


def _boolean(value: object, label: str) -> bool:
    if not isinstance(value, bool):
        raise ValueError(f"{label} must be true or false")
    return value


def _load_token(path: Path) -> str:
    try:
        token = path.read_text().strip()
    except FileNotFoundError:
        token = ""
    if token:
        return token
    token = secrets.token_urlsafe(24)
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(dir=path.parent, prefix=".config-web-token-", delete=False) as tmp:
        tmp.write((token + "\n").encode())
        tmp.flush()
        os.fsync(tmp.fileno())
        name = tmp.name
    os.chmod(name, 0o600)
    os.replace(name, path)
    return token
