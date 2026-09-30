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
    result.add_argument("-server-name", default="AirPlay Sendspin", help="inbound Sendspin server name")
    return result


async def run(args: argparse.Namespace) -> None:
    manager = Manager(
        Config(
            port_base=args.port_base,
            port_range=args.port_range,
            config_path=args.config,
            server_port=args.server_port,
            server_name=args.server_name,
        )
    )
    stop = asyncio.Event()
    loop = asyncio.get_running_loop()
    for signum in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(signum, stop.set)
    try:
        await manager.start()
        await stop.wait()
    finally:
        await manager.close()


def main() -> None:
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
    args = parser().parse_args()
    asyncio.run(run(args))


if __name__ == "__main__":
    main()
