"""Command-line entrypoint."""

from __future__ import annotations

import argparse
import asyncio
import logging
import signal

from .app import Config, Manager


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description="AirPlay 1 targets for Sendspin speakers")
    result.add_argument("-port-base", type=int, default=7000, help="first AirPlay port")
    result.add_argument("-port-range", type=int, default=10, help="ports reserved per AirPlay target")
    result.add_argument("-config", default="config.xml", help="persistent speaker registry")
    result.add_argument("-server-port", type=int, default=8927, help="inbound Sendspin server port")
    result.add_argument("-server-name", default="Sendspin Bridge", help="inbound Sendspin server name")
    result.add_argument("-web-host", default="0.0.0.0", help="configuration UI listen address")
    result.add_argument("-web-port", type=int, default=8080, help="configuration UI port")
    result.add_argument("-no-spotify", action="store_true", help="disable Spotify Connect targets")
    return result


def manager_config(args: argparse.Namespace) -> Config:
    return Config(
        port_base=args.port_base,
        port_range=args.port_range,
        config_path=args.config,
        server_port=args.server_port,
        server_name=args.server_name,
        web_host=args.web_host,
        web_port=args.web_port,
        spotify=not args.no_spotify,
    )


async def run(args: argparse.Namespace) -> None:
    stop = asyncio.Event()
    loop = asyncio.get_running_loop()
    for signum in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(signum, stop.set)

    while not stop.is_set():
        manager = Manager(manager_config(args))
        waiters: list[asyncio.Task[bool]] = []
        restart = False
        try:
            await manager.start()
            waiters = [
                asyncio.create_task(stop.wait()),
                asyncio.create_task(manager.restart_requested.wait()),
            ]
            await asyncio.wait(waiters, return_when=asyncio.FIRST_COMPLETED)
            restart = manager.restart_requested.is_set() and not stop.is_set()
        finally:
            for waiter in waiters:
                waiter.cancel()
            await asyncio.gather(*waiters, return_exceptions=True)
            await manager.close()
        if restart:
            logging.getLogger(__name__).info("Bridge restarted with updated configuration")


def main() -> None:
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
    asyncio.run(run(parser().parse_args()))


if __name__ == "__main__":
    main()
