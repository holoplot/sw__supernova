#!/usr/bin/env python3

import asyncio
import json
import os
import sys

from dataclasses import dataclass

from dataclass_wizard import JSONWizard

import nats
from nats.js.api import ConsumerConfig

from prometheus_client import start_http_server, Counter, Gauge


MESSAGES_RECEIVED_PUSH = Counter(
    "nats_stream_consumer_messages_received_push_total",
    "The total number of messages received from a push subscription",
)
MESSAGES_RECEIVED_PULL = Counter(
    "nats_stream_consumer_messages_received_pull_total",
    "The total number of messages received from a pull subscription",
)
FETCH_ERRORS_DEADLINE = Counter(
    "nats_stream_consumer_fetch_errors_deadline_total",
    "The total number of deadline errors fetching messages from a pull subscription",
)
FETCH_ERRORS_OTHER = Counter(
    "nats_stream_consumer_fetch_errors_other_total",
    "The total number of other errors fetching messages from a pull subscription",
)
FETCH_ERRORS_CONSUMER_DELETED = Counter(
    "nats_stream_consumer_fetch_errors_consumer_deleted_total",
    "The total number of errors fetching messages from a pull subscription where the consumer was deleted",
)
NC_STATUS = Gauge(
    "nats_stream_consumer_nc_status",
    "The status of the NATS connection",
)
RESUBSCRIBES = Counter(
    "nats_stream_consumer_re_subscribes_total",
    "The total number of re-subscribes",
)
CREATE_CONSUMER_ERRORS = Counter(
    "nats_stream_consumer_create_consumer_errors_total",
    "The total number of errors creating a consumer",
)
CREATE_SUBSCRIPTION_ERRORS = Counter(
    "nats_stream_consumer_create_subscription_errors_total",
    "The total number of errors creating a subscription",
)


@dataclass
class SubjectConsumerConfig(JSONWizard):
    stream: str
    subject: str
    pull: bool | None = None
    unique_delivery_subject: bool | None = None
    consumer_config: ConsumerConfig | None = None


def load_config(config_path):
    consumers = []

    with open(config_path, "r") as f:
        for line in f:
            conf = SubjectConsumerConfig.from_json(line)
            consumers.append(conf)

    return consumers


async def create_consumer(nc, js, config):
    print("starting consumer with config {}".format(config))

    if config.unique_delivery_subject:
        config.consumer_config.deliver_subject = nc.new_inbox()

    if config.pull:
        while True:
            try:
                await pull_subscribe(js, config)
            except Exception as e:
                print("pull subscribe error: {}".format(e))
                RESUBSCRIBES.inc()

    sub = await js.subscribe(
        config.subject,
        stream=config.stream,
        config=config.consumer_config,
    )
    # TODO can we explicitly delete the consumer?

    try:
        async for msg in sub.messages:
            MESSAGES_RECEIVED_PUSH.inc()
    except Exception as e:
        print("Error while processing messages: {}".format(e))
        sub.unsubscribe()

    print("stopping consumer with config {}".format(config))


async def pull_subscribe(js, config):
    try:
        sub = await js.pull_subscribe(
            config.subject,
            "",
            stream=config.stream,
            config=config.consumer_config,
        )
    except Exception as e:
        CREATE_SUBSCRIPTION_ERRORS.inc()
        return

    while True:
        try:
            msgs = sub.fetch(1)
            msg = msgs[0]
        except Exception as e:
            FETCH_ERRORS_OTHER.inc()
            continue

        MESSAGES_RECEIVED_PULL.inc()

    sub.unsubscribe()


async def track_status(nc):
    while True:
        status = nc._status
        NC_STATUS.set(status)
        print("nc status {}".format(status))
        await asyncio.sleep(1)


async def main():
    start_http_server(9090)

    nats_url = os.environ.get("NATS_URL")
    if nats_url is None:
        sys.exit("NATS_URL environment variable is not specified")

    config_path = os.environ.get("CONFIG_FILE")
    if config_path  is None:
        sys.exit("CONFIG_FILE environment variable is not specified")

    configs = load_config(config_path)

    nc = await nats.connect(nats_url, max_reconnect_attempts=-1)
    js = nc.jetstream()

    asyncio.create_task(track_status(nc))

    async with asyncio.TaskGroup() as tg:
        for c in configs:
            tg.create_task(
                create_consumer(nc, js, c)
            )

    await nc.drain()
    return


if __name__ == "__main__":
    asyncio.run(main())
